//go:build e2e

package rebalancer_test

import (
	"bufio"
	"context"
	"errors"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/flare-foundation/rebalancer/internal/rebalancer"
	"github.com/flare-foundation/rebalancer/pkg/txmng"
	"github.com/stretchr/testify/require"
)

const (
	rpcURL            = "https://coston2-api.flare.network/ext/C/rpc"
	rebalancerPrivKey = "0xa392eb3a8bfa1dff1c1eff81785b6e126b248d6eca3fc502620aeac10114d681"
	rebalancerAddrStr = "0x36352928E1C66a280cb94490B963d07F23706482"
	metricsURL        = "http://localhost:19090/metrics"
)

func flrToWei(flr int64) *big.Int {
	e18 := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	return new(big.Int).Mul(big.NewInt(flr), e18)
}

// scrapeMetricValue fetches the Prometheus metrics endpoint and returns the value
// of the named metric. Only metrics with no labels are supported.
func scrapeMetricValue(t *testing.T, name string) (float64, bool) {
	t.Helper()

	resp, err := http.Get(metricsURL) //nolint:noctx // e2e test helper
	if err != nil {
		return 0, false
	}
	defer func() {
		if err := resp.Body.Close(); err != nil {
			t.Logf("close metrics response body: %v", err)
		}
	}()

	prefix := name + " "
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, prefix) {
			parts := strings.Fields(line)
			if len(parts) >= 2 {
				val, err := strconv.ParseFloat(parts[1], 64)
				if err == nil {
					return val, true
				}
			}
		}
	}
	return 0, false
}

func TestE2ERebalancer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	ethClient, err := ethclient.Dial(rpcURL)
	require.NoError(t, err)

	rebalancerAddr := common.HexToAddress(rebalancerAddrStr)

	// Pre-check: rebalancer sender needs enough funds
	senderBal, err := ethClient.BalanceAt(ctx, rebalancerAddr, nil)
	require.NoError(t, err)
	if senderBal.Cmp(flrToWei(10)) < 0 {
		t.Skipf("Rebalancer %s balance too low (%s wei); top up at https://faucet.flare.network/coston2",
			rebalancerAddrStr, senderBal.String())
	}
	t.Logf("Rebalancer balance: %s wei", senderBal.String())

	// Generate ephemeral tracked address
	trackedKey, err := crypto.GenerateKey()
	require.NoError(t, err)
	trackedAddr := crypto.PubkeyToAddress(trackedKey.PublicKey)
	t.Logf("Tracked address: %s", trackedAddr.Hex())

	cfg := rebalancer.Config{
		NodeURL:       rpcURL,
		PrivateKeyHex: rebalancerPrivKey,
		CheckInterval: 15 * time.Second,
		TxTimeout:     10 * time.Second,
		MaxRetries:    3,
		MetricsAddr:   ":19090",
		Addresses: []rebalancer.TrackedAddressConfig{
			{
				Address:    trackedAddr,
				MinBalance: big.NewInt(1_000_000_000_000_000_000), // 1 C2FLR in wei
				TopUpValue: big.NewInt(2_000_000_000_000_000_000), // 2 C2FLR in wei
			},
		},
	}

	rb, err := rebalancer.New(cfg)
	require.NoError(t, err)

	rbCtx, rbCancel := context.WithCancel(ctx)
	defer rbCancel()
	rbDone := make(chan error, 1)
	go func() { rbDone <- rb.Run(rbCtx) }()

	// txmng for the tracked key — used to drain and cleanup
	drainCtx, drainCancel := context.WithCancel(ctx)
	defer drainCancel()
	drainManager, err := txmng.New(trackedKey, ethClient, txmng.Config{
		TxTimeout: 10 * time.Second, MaxRetries: 3,
	}, nil)
	require.NoError(t, err)
	drainDone := make(chan struct{})
	go func() {
		defer close(drainDone)
		_ = drainManager.Run(drainCtx)
	}()

	// Wait for metrics server to be ready
	t.Log("Waiting for metrics server to start...")
	require.Eventually(t, func() bool {
		resp, err := http.Get(metricsURL) //nolint:noctx // e2e test helper
		if err != nil {
			return false
		}
		_ = resp.Body.Close()
		return resp.StatusCode == http.StatusOK
	}, 10*time.Second, time.Second, "metrics server did not start within 10 seconds")
	t.Log("Metrics server up ✓")

	topUpWei := flrToWei(2)

	// Assert 1: tracked address receives initial top-up
	t.Log("Waiting for initial top-up to 2 C2FLR...")
	require.Eventually(t, func() bool {
		bal, err := ethClient.BalanceAt(ctx, trackedAddr, nil)
		if err != nil {
			t.Logf("balance check error: %v", err)
			return false
		}
		t.Logf("Tracked balance: %s wei", bal.String())
		return bal.Cmp(topUpWei) >= 0
	}, 2*time.Minute, 10*time.Second, "tracked address not funded within 2 minutes")
	t.Log("Initial top-up confirmed ✓")

	// Assert 2: Prometheus metrics reflect the initial top-up
	t.Log("Waiting for Prometheus metrics to reflect initial top-up...")
	require.Eventually(t, func() bool {
		val, ok := scrapeMetricValue(t, "rebalancer_fundings")
		return ok && val >= 1
	}, 30*time.Second, 3*time.Second, "rebalancer_fundings did not reach 1")

	val, ok := scrapeMetricValue(t, "rebalancer_checks")
	require.True(t, ok, "rebalancer_checks metric not found")
	require.GreaterOrEqual(t, val, 1.0, "expected at least 1 check cycle")

	val, ok = scrapeMetricValue(t, "rebalancer_sender_balance_native")
	require.True(t, ok, "rebalancer_sender_balance_native metric not found")
	require.Greater(t, val, 0.0, "expected positive sender balance")

	val, ok = scrapeMetricValue(t, "rebalancer_amount_sent_native")
	require.True(t, ok, "rebalancer_amount_sent_native metric not found")
	require.Greater(t, val, 0.0, "expected positive amount sent")

	val, ok = scrapeMetricValue(t, "rebalancer_successful_topups_total")
	require.True(t, ok, "rebalancer_successful_topups_total metric not found")
	require.GreaterOrEqual(t, val, 1.0, "expected successful top-ups counter")

	val, ok = scrapeMetricValue(t, "rebalancer_topup_amount_native_total")
	require.True(t, ok, "rebalancer_topup_amount_native_total metric not found")
	require.Greater(t, val, 0.0, "expected positive topup_amount_native_total counter")

	val, ok = scrapeMetricValue(t, "rebalancer_last_check_timestamp_seconds")
	require.True(t, ok, "rebalancer_last_check_timestamp_seconds metric not found")
	require.Greater(t, val, 0.0, "expected non-zero last check timestamp")

	val, ok = scrapeMetricValue(t, "rebalancer_last_funding_timestamp_seconds")
	require.True(t, ok, "rebalancer_last_funding_timestamp_seconds metric not found")
	require.Greater(t, val, 0.0, "expected non-zero last funding timestamp")

	t.Log("Prometheus metrics after initial top-up ✓")

	// Drain: send 1.5 C2FLR from tracked address back to rebalancer address
	drainAmount := new(big.Int)
	drainAmount.SetString("1500000000000000000", 10)
	t.Log("Draining 1.5 C2FLR from tracked address to rebalancer address...")
	require.NoError(t, drainManager.Send(ctx, rebalancerAddr, drainAmount))
	t.Log("Drain tx confirmed ✓")

	// Assert 3: rebalancer detects low balance and tops up again
	t.Log("Waiting for re-top-up to 2 C2FLR...")
	require.Eventually(t, func() bool {
		bal, err := ethClient.BalanceAt(ctx, trackedAddr, nil)
		if err != nil {
			t.Logf("balance check error: %v", err)
			return false
		}
		t.Logf("Tracked balance: %s wei", bal.String())
		return bal.Cmp(topUpWei) >= 0
	}, 2*time.Minute, 10*time.Second, "tracked address not re-funded within 2 minutes")
	t.Log("Re-top-up confirmed ✓")

	// Assert 4: Prometheus metrics reflect the second top-up
	t.Log("Waiting for Prometheus metrics to reflect re-top-up...")
	require.Eventually(t, func() bool {
		val, ok := scrapeMetricValue(t, "rebalancer_fundings")
		return ok && val >= 2
	}, 30*time.Second, 3*time.Second, "rebalancer_fundings did not reach 2 after re-top-up")
	t.Log("Prometheus metrics after re-top-up ✓")

	// Stop rebalancer
	rbCancel()
	select {
	case err := <-rbDone:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("unexpected rebalancer error: %v", err)
		}
	case <-time.After(15 * time.Second):
		t.Error("rebalancer did not stop within 15 seconds")
	}
	t.Log("Rebalancer stopped ✓")

	// Cleanup: return remaining tracked balance to rebalancer address
	trackedBal, err := ethClient.BalanceAt(ctx, trackedAddr, nil)
	if err != nil {
		t.Logf("Warning: could not get tracked balance for cleanup: %v", err)
	} else {
		gasBuffer := big.NewInt(10_000_000_000_000_000) // 0.01 C2FLR
		sendBack := new(big.Int).Sub(trackedBal, gasBuffer)
		if sendBack.Sign() > 0 {
			t.Logf("Cleanup: returning %s wei to rebalancer address", sendBack.String())
			if err := drainManager.Send(ctx, rebalancerAddr, sendBack); err != nil {
				t.Logf("Warning: cleanup send failed: %v", err)
			} else {
				t.Log("Cleanup done ✓")
			}
		}
	}

	drainCancel()
	<-drainDone
}

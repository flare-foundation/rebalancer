//go:build e2e

package rebalancer_test

import (
	"context"
	"errors"
	"math/big"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/flare-network/rebalancer/internal/rebalancer"
	"github.com/flare-network/rebalancer/pkg/txmng"
	"github.com/stretchr/testify/require"
)

const (
	rpcURL            = "https://coston2-api.flare.network/ext/C/rpc"
	rebalancerPrivKey = "0xa392eb3a8bfa1dff1c1eff81785b6e126b248d6eca3fc502620aeac10114d681"
	rebalancerAddrStr = "0x36352928E1C66a280cb94490B963d07F23706482"
)

func flrToWei(flr int64) *big.Int {
	e18 := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	return new(big.Int).Mul(big.NewInt(flr), e18)
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

	// Drain: send 1.5 C2FLR from tracked address back to rebalancer address
	drainAmount := new(big.Int)
	drainAmount.SetString("1500000000000000000", 10)
	t.Log("Draining 1.5 C2FLR from tracked address to rebalancer address...")
	require.NoError(t, drainManager.Send(ctx, rebalancerAddr, drainAmount))
	t.Log("Drain tx confirmed ✓")

	// Assert 2: rebalancer detects low balance and tops up again
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

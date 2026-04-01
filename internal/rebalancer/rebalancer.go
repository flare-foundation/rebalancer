package rebalancer

import (
	"context"
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/flare-network/rebalancer/pkg/rebalancer"
	"github.com/flare-network/rebalancer/pkg/txmng"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"golang.org/x/sync/errgroup"
)

const defaultMetricsAddr = ":8080"

// Rebalancer wires together pkg/txmng and pkg/rebalancer with real dependencies.
type Rebalancer struct {
	manager       *txmng.Manager
	rb            *rebalancer.Rebalancer
	client        *ethclient.Client
	metrics       *metrics
	checkInterval time.Duration
	metricsAddr   string
}

// New creates a new Rebalancer, wiring ethclient, txmng, and rebalancer packages.
func New(cfg Config) (*Rebalancer, error) {
	// Validate required config
	if cfg.NodeURL == "" {
		return nil, errors.New("NodeURL is required (from ETH_RPC_URL)")
	}

	if cfg.PrivateKeyHex == "" {
		return nil, errors.New("PrivateKeyHex is required (from REBALANCER_PRIVATE_KEY)")
	}

	// Connect to Ethereum node
	nodeURL := cfg.NodeURL
	if cfg.APIKey != "" {
		u, err := url.Parse(nodeURL)
		if err != nil {
			return nil, fmt.Errorf("failed to parse NodeURL: %w", err)
		}
		q := u.Query()
		q.Set("x-apikey", cfg.APIKey)
		u.RawQuery = q.Encode()
		nodeURL = u.String()
	}

	client, err := ethclient.Dial(nodeURL)
	if err != nil {
		return nil, fmt.Errorf("failed to dial ethclient: %w", err)
	}

	// Parse private key
	pk, err := PrivateKeyFromHex(cfg.PrivateKeyHex)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %w", err)
	}

	// Configure logger from config
	logger.Set(cfg.Logger)

	// Create logger adapter
	log := logger.GetLogger()

	// Create txmng.Manager
	txmngCfg := txmng.Config{
		TxTimeout:  cfg.TxTimeout,
		MaxRetries: cfg.MaxRetries,
	}
	manager, err := txmng.New(pk, client, txmngCfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create txmng.Manager: %w", err)
	}

	// Convert TrackedAddressConfig to rebalancer.TrackedAddress
	trackedAddrs := make([]*rebalancer.TrackedAddress, len(cfg.Addresses))
	for i, addr := range cfg.Addresses {
		// Apply defaults if not specified (nil means use default)
		minBalWei := addr.MinBalance
		if minBalWei == nil {
			minBalWei = rebalancer.DefaultMinBalanceWei()
		}

		topUpValWei := addr.TopUpValue
		if topUpValWei == nil {
			topUpValWei = rebalancer.DefaultTopUpValueWei()
		}

		trackedAddrs[i] = &rebalancer.TrackedAddress{
			Address:      addr.Address,
			MinBalance:   minBalWei,
			TopUpValue:   topUpValWei,
			DailyLimit:   addr.DailyLimit,
			WeeklyLimit:  addr.WeeklyLimit,
			LastCheckAt:  0,
			LastFundedAt: 0,
		}
	}

	// Determine warning balance
	warningBalWei := rebalancer.DefaultWarningBalanceWei()
	if cfg.WarningBalanceFLR > 0 {
		warningBalWei = rebalancer.FLRToWei(cfg.WarningBalanceFLR)
	}

	// Determine check interval
	checkInterval := cfg.CheckInterval
	if checkInterval == 0 {
		checkInterval = rebalancer.DefaultCheckInterval
	}

	// Create metrics (needed as LimitReporter for rebalancer)
	m := newMetrics()

	// Create rebalancer.Rebalancer
	rebalancerCfg := rebalancer.Config{
		CheckInterval:    checkInterval,
		WarningBalance:   warningBalWei,
		InitialAddresses: trackedAddrs,
		LimitReporter:    m,
		MetricPusher:     m,
	}
	rb, err := rebalancer.New(manager, client, rebalancerCfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create rebalancer: %w", err)
	}

	metricsAddr := cfg.MetricsAddr
	if metricsAddr == "" {
		metricsAddr = defaultMetricsAddr
	}

	return &Rebalancer{
		manager:       manager,
		rb:            rb,
		client:        client,
		metrics:       m,
		checkInterval: checkInterval,
		metricsAddr:   metricsAddr,
	}, nil
}

// Run starts the rebalancer, transaction manager, and balance monitoring.
// This function is blocking and should be run in a goroutine.
func (r *Rebalancer) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

	// Serve Prometheus metrics
	g.Go(func() error {
		return r.serveMetrics(ctx)
	})

	// Run transaction manager (handles queued transactions)
	g.Go(func() error {
		return r.manager.Run(ctx)
	})

	// Monitor sender balance and update Prometheus metric
	g.Go(func() error {
		r.monitorSenderBalance(ctx)
		return nil
	})

	// Run rebalancer (checks tracked addresses and funds them)
	g.Go(func() error {
		return r.rb.Run(ctx)
	})

	return g.Wait()
}

// serveMetrics starts an HTTP server exposing Prometheus metrics on /metrics.
func (r *Rebalancer) serveMetrics(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	srv := &http.Server{
		Addr:    r.metricsAddr,
		Handler: mux,
	}

	go func() {
		<-ctx.Done()
		if err := srv.Shutdown(context.Background()); err != nil {
			logger.Warnf("metrics server shutdown error: %v", err)
		}
	}()

	logger.Infof("Serving metrics on %s/metrics", r.metricsAddr)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("metrics server: %w", err)
	}
	return nil
}

// monitorSenderBalance periodically updates the sender's balance metric.
func (r *Rebalancer) monitorSenderBalance(ctx context.Context) {
	ticker := time.NewTicker(r.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			bal, err := r.client.BalanceAt(ctx, r.manager.Address(), nil)
			if err != nil {
				logger.Warnf("failed to check sender balance: %v", err)
				continue
			}

			// Convert big.Int to float64 for gauge
			// Use big.Float for precision when converting
			fBal := new(big.Float).SetInt(bal)
			fBalVal, _ := fBal.Float64()
			r.metrics.senderBalance.Set(fBalVal)
		}
	}
}

func PrivateKeyFromHex(privateKey string) (*ecdsa.PrivateKey, error) {
	privateKey = strings.TrimPrefix(privateKey, "0x")

	privKey, err := crypto.HexToECDSA(privateKey)
	if err != nil {
		return nil, errors.New("cannot parse private key")
	}
	return privKey, nil
}

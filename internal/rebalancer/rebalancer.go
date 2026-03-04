package rebalancer

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"time"

	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/flare-foundation/flare-system-client/utils/credentials"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/flare-network/rebalancer/pkg/rebalancer"
	"github.com/flare-network/rebalancer/pkg/txmng"
	"golang.org/x/sync/errgroup"
)

// Rebalancer wires together pkg/txmng and pkg/rebalancer with real dependencies.
type Rebalancer struct {
	manager       *txmng.Manager
	rb            *rebalancer.Rebalancer
	client        *ethclient.Client
	metrics       *metrics
	checkInterval time.Duration
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
	pk, err := credentials.PrivateKeyFromHex(cfg.PrivateKeyHex)
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

	// Create rebalancer.Rebalancer
	rebalancerCfg := rebalancer.Config{
		CheckInterval:    checkInterval,
		WarningBalance:   warningBalWei,
		InitialAddresses: trackedAddrs,
	}
	rb, err := rebalancer.New(manager, client, rebalancerCfg, log)
	if err != nil {
		return nil, fmt.Errorf("failed to create rebalancer: %w", err)
	}

	return &Rebalancer{
		manager:       manager,
		rb:            rb,
		client:        client,
		metrics:       newMetrics(),
		checkInterval: checkInterval,
	}, nil
}

// Run starts the rebalancer, transaction manager, and balance monitoring.
// This function is blocking and should be run in a goroutine.
func (r *Rebalancer) Run(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)

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

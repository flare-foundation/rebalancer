package rebalancer

import (
	"context"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
)

// BalanceChecker is the interface for checking address balances on chain.
type BalanceChecker interface {
	BalanceAt(ctx context.Context, account common.Address, blockNumber *big.Int) (*big.Int, error)
}

// Sender is the interface for sending transactions to fund addresses.
type Sender interface {
	Send(ctx context.Context, to common.Address, amount *big.Int) error
	Address() common.Address
}

// Logger is the interface for logging messages.
type Logger interface {
	Infof(format string, args ...any)
	Info(args ...any)
	Debugf(format string, args ...any)
	Warnf(format string, args ...any)
	Errorf(format string, args ...any)
}

// NoOpLogger is a default logger that does no logging.
type NoOpLogger struct{}

func (n *NoOpLogger) Infof(format string, args ...any)  {}
func (n *NoOpLogger) Info(args ...any)                  {}
func (n *NoOpLogger) Debugf(format string, args ...any) {}
func (n *NoOpLogger) Warnf(format string, args ...any)  {}
func (n *NoOpLogger) Errorf(format string, args ...any) {}

// Config holds configuration for creating a new Rebalancer.
type Config struct {
	CheckInterval    time.Duration     `toml:"check_interval"`
	WarningBalance   *big.Int          `toml:"warning_balance"`
	InitialAddresses []*TrackedAddress `toml:"initial_addresses"`
}

// TrackedAddress represents an address being monitored by the rebalancer.
type TrackedAddress struct {
	Address      common.Address `toml:"address"`
	MinBalance   *big.Int       `toml:"min_balance"`
	TopUpValue   *big.Int       `toml:"top_up_value"`
	LastCheckAt  int64          `toml:"-"`
	LastFundedAt int64          `toml:"-"`
}

// BalanceCheckResult contains the result of a balance check for an address.
type BalanceCheckResult struct {
	Address    common.Address
	Balance    *big.Int
	NeedsFunds bool
	Error      error
}

// RebalancerMetrics tracks statistics about rebalancing operations.
type RebalancerMetrics struct {
	TotalChecks     uint64
	TotalFundings   uint64
	TotalAmountSent *big.Int
	LastCheckTime   int64
	LastFundTime    int64
}

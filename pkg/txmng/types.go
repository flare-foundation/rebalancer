package txmng

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

// ChainClient is the interface for interacting with an Ethereum-compatible blockchain.
// It matches the signature of ethclient.Client from go-ethereum.
type ChainClient interface {
	NetworkID(context.Context) (*big.Int, error)
	SuggestGasPrice(context.Context) (*big.Int, error)
	SuggestGasTipCap(context.Context) (*big.Int, error)
	NonceAt(context.Context, common.Address, *big.Int) (uint64, error)
	EstimateGas(context.Context, ethereum.CallMsg) (uint64, error)
	SendTransaction(context.Context, *types.Transaction) error
	TransactionReceipt(context.Context, common.Hash) (*types.Receipt, error)
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

var _ Logger = (*NoOpLogger)(nil)

func (n *NoOpLogger) Infof(format string, args ...any)  {}
func (n *NoOpLogger) Info(args ...any)                  {}
func (n *NoOpLogger) Debugf(format string, args ...any) {}
func (n *NoOpLogger) Warnf(format string, args ...any)  {}
func (n *NoOpLogger) Errorf(format string, args ...any) {}

// Config holds configuration for creating a new Manager.
type Config struct {
	TxTimeout  time.Duration `toml:"tx_timeout"`
	MaxRetries int           `toml:"max_retries"`
}

// validate checks the configuration for validity.
func (c *Config) validate() error {
	if c.TxTimeout <= 0 {
		return errors.New("tx_timeout must be positive")
	}
	if c.MaxRetries < 0 {
		return errors.New("max_retries must be non-negative")
	}
	return nil
}

// txRequest is an internal request placed in the queue by Send/SendWithInput.
type txRequest struct {
	to     common.Address
	amount *big.Int   // may be zero but not nil
	input  []byte     // nil for plain value transfers
	result chan error // buffered with capacity 1; caller blocks on this
}

package rebalancer

import (
	"fmt"
	"math/big"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/ethereum/go-ethereum/common"
	"github.com/flare-foundation/go-flare-common/pkg/logger"
	"github.com/kelseyhightower/envconfig"
)

// TrackedAddressConfig is the TOML-friendly configuration for a tracked address.
type TrackedAddressConfig struct {
	Address    common.Address `toml:"address"`
	MinBalance *big.Int       `toml:"min_balance_wei"`  // in wei; nil means use default
	TopUpValue *big.Int       `toml:"top_up_value_wei"` // in wei; nil means use default
}

// UnmarshalTOML implements custom TOML unmarshaling for TrackedAddressConfig.
func (t *TrackedAddressConfig) UnmarshalTOML(data any) error {
	m, ok := data.(map[string]any)
	if !ok {
		return nil
	}

	// Parse address
	if addr, ok := m["address"].(string); ok {
		t.Address = common.HexToAddress(addr)
	}

	// Parse MinBalance (int64 or string) and convert to *big.Int
	if minBal, ok := m["min_balance_wei"].(int64); ok && minBal != 0 {
		t.MinBalance = big.NewInt(minBal)
	} else if minBalStr, ok := m["min_balance_wei"].(string); ok && minBalStr != "" {
		t.MinBalance = new(big.Int)
		t.MinBalance.SetString(minBalStr, 10)
	}

	// Parse TopUpValue (int64 or string) and convert to *big.Int
	if topUpVal, ok := m["top_up_value_wei"].(int64); ok && topUpVal != 0 {
		t.TopUpValue = big.NewInt(topUpVal)
	} else if topUpValStr, ok := m["top_up_value_wei"].(string); ok && topUpValStr != "" {
		t.TopUpValue = new(big.Int)
		t.TopUpValue.SetString(topUpValStr, 10)
	}

	return nil
}

// Config holds all configuration for the internal rebalancer package.
type Config struct {
	// ENV-only
	NodeURL       string `toml:"-" envconfig:"ETH_RPC_URL"`
	PrivateKeyHex string `toml:"-" envconfig:"REBALANCER_PRIVATE_KEY"`

	// TOML + ENV overrides
	CheckInterval     time.Duration          `toml:"check_interval" envconfig:"REBALANCER_CHECK_INTERVAL"`
	WarningBalanceFLR int64                  `toml:"warning_balance_flr" envconfig:"REBALANCER_WARNING_BALANCE_FLR"`
	Addresses         []TrackedAddressConfig `toml:"addresses"`

	// txmng config
	TxTimeout  time.Duration `toml:"tx_timeout" envconfig:"REBALANCER_TX_TIMEOUT"`
	MaxRetries int           `toml:"max_retries" envconfig:"REBALANCER_MAX_RETRIES"`

	// logger config
	Logger logger.Config `toml:"logger"`
}

// Load reads configuration from a TOML file and then overrides with environment variables.
func Load(fileName string) (Config, error) {
	cfg := Config{}

	// Parse TOML file (allowMissing=true)
	if err := ReadTo(fileName, &cfg, true); err != nil {
		return Config{}, fmt.Errorf("reading config: %w", err)
	}

	// Override with environment variables
	if err := envconfig.Process("", &cfg); err != nil {
		return Config{}, fmt.Errorf("reading env variables: %w", err)
	}

	return cfg, nil
}

// ReadTo reads toml file from filePath and marshals it into dest.
// If allowUnknownFields is set to false, any unknown field in toml file will trigger error.
func ReadTo[T any](filePath string, dest *T, allowUnknownFields bool) error {
	md, err := toml.DecodeFile(filePath, dest)

	if !allowUnknownFields && len(md.Undecoded()) > 0 {
		return fmt.Errorf("unknown field in toml %v", md.Undecoded()[0].String())
	}

	return err
}

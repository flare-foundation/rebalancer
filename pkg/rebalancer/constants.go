package rebalancer

import (
	"math/big"
	"time"
)

// Default configuration values for the rebalancer
const (
	// DefaultCheckInterval is the default period between balance checks
	DefaultCheckInterval = 5 * time.Minute

	// DefaultMinBalance is the default minimum balance for tracked addresses (in FLR)
	DefaultMinBalance = 20

	// DefaultTopUpValue is the default top-up balance for tracked addresses (in FLR)
	DefaultTopUpValue = 200

	// DefaultWarningBalance is the default balance threshold for warnings (in FLR)
	DefaultWarningBalance = 1000
)

// ToWei converts FLR to Wei (18 decimals)
func FLRToWei(flr int64) *big.Int {
	return new(big.Int).Mul(big.NewInt(flr), big.NewInt(1e18))
}

// DefaultMinBalanceWei returns the default minimum balance in Wei
func DefaultMinBalanceWei() *big.Int {
	return FLRToWei(DefaultMinBalance)
}

// DefaultTopUpValueWei returns the default top-up value in Wei
func DefaultTopUpValueWei() *big.Int {
	return FLRToWei(DefaultTopUpValue)
}

// DefaultWarningBalanceWei returns the default warning balance in Wei
func DefaultWarningBalanceWei() *big.Int {
	return FLRToWei(DefaultWarningBalance)
}

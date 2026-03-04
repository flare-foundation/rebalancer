package txmng

import "time"

const (
	DefaultTxTimeout      = 10 * time.Second
	DefaultMaxRetries     = 3
	DefaultPollInterval   = 200 * time.Millisecond
	DefaultQueueSize      = 100
	RetryGasBumpNumerator = 11 // 10% bump = multiply by 11/10, then add 1 wei to ensure >10%
	RetryGasBumpDivisor   = 10
)

// Gas multipliers using integer arithmetic to avoid floating-point rounding issues.
// Initial transaction:
//
//	TipCap = SuggestGasTipCap (from node)
//	FeeCap = SuggestGasPrice (from node)
//	GasLimit = EstimatedGas * 15/10 = 1.5x estimated
//
// Retry bumping:
//
//	Both TipCap and FeeCap = max(fresh values, previous * 11/10 + 1 wei)
const (
	TipCapNumerator   = 20
	TipCapDivisor     = 10
	FeeCapNumerator   = 35
	FeeCapDivisor     = 10
	GasLimitNumerator = 15
	GasLimitDivisor   = 10
)

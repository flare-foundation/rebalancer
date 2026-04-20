package txmng

import "time"

const (
	// DefaultTxTimeout bounds each RPC call and receipt-poll attempt.
	DefaultTxTimeout = 10 * time.Second

	// DefaultMaxRetries is the number of resend attempts before a transaction is considered dropped.
	DefaultMaxRetries = 3

	// DefaultPollInterval is the delay between TransactionReceipt polls.
	DefaultPollInterval = 200 * time.Millisecond

	// DefaultQueueSize is the buffer depth of the transaction request queue.
	DefaultQueueSize = 100

	// RetryGasBumpNumerator and RetryGasBumpDivisor encode a 10% bump (×11/10, +1 wei) applied
	// to gas tip and fee caps on each retry, satisfying the replacement-transaction rule.
	RetryGasBumpNumerator = 11
	RetryGasBumpDivisor   = 10

	// GasLimitNumerator and GasLimitDivisor scale the estimated gas limit by 1.5× (×15/10)
	// to absorb variability between estimate time and inclusion time.
	GasLimitNumerator = 15
	GasLimitDivisor   = 10
)

package rebalancer

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/params"
	"github.com/flare-foundation/rebalancer/pkg/rebalancer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// weiToNativeTokenFloat converts wei to a token amount (÷ 10^18) for Prometheus.
// Prometheus only has float64; stuffing raw wei into a float rounds badly, so divide first.
func weiToNativeTokenFloat(wei *big.Int) float64 {
	if wei == nil || wei.Sign() == 0 {
		return 0
	}
	q := new(big.Float).Quo(new(big.Float).SetInt(wei), big.NewFloat(params.Ether))
	f, _ := q.Float64()
	return f
}

var (
	_ rebalancer.LimitReporter = (*metrics)(nil)
	_ rebalancer.MetricPusher  = (*metrics)(nil)
)

// metrics holds Prometheus metrics for the internal rebalancer.
type metrics struct {
	senderBalance    prometheus.Gauge
	limitsReached    *prometheus.CounterVec
	checks           prometheus.Gauge
	fundings         prometheus.Gauge
	amountSentNative prometheus.Gauge
	lastCheckTime    prometheus.Gauge
	lastFundingTime  prometheus.Gauge

	// Top-up counters: increments computed in applyTopupCounterDeltas from Push() snapshots.
	successfulTopupsTotal  prometheus.Counter
	topupAmountNativeTotal prometheus.Counter

	pushInitialized bool
	prevFundings    uint64
	prevAmountWei   *big.Int
}

// newMetrics creates and registers Prometheus metrics for the rebalancer.
func newMetrics() *metrics {
	return &metrics{
		senderBalance: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "sender_balance_native",
			Help:      "Sender balance in native token units (not wei)",
		}),
		limitsReached: promauto.NewCounterVec(prometheus.CounterOpts{
			Namespace: "rebalancer",
			Name:      "topup_limit_reached_total",
			Help:      "Number of times a topup was skipped due to rate limiting",
		}, []string{"address", "limit_type"}),
		checks: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "checks",
			Help:      "Cumulative number of balance check cycles completed",
		}),
		fundings: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "fundings",
			Help:      "Cumulative number of successful top-up transactions sent",
		}),
		amountSentNative: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "amount_sent_native",
			Help:      "Total amount sent in native token units (not wei)",
		}),
		lastCheckTime: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "last_check_timestamp_seconds",
			Help:      "Unix timestamp of the most recent balance check cycle",
		}),
		lastFundingTime: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "last_funding_timestamp_seconds",
			Help:      "Unix timestamp of the most recent top-up transaction",
		}),
		successfulTopupsTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "rebalancer",
			Name:      "successful_topups_total",
			Help:      "Successful top-up transactions (increments once per completed send)",
		}),
		topupAmountNativeTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "rebalancer",
			Name:      "topup_amount_native_total",
			Help:      "Cumulative top-up volume in native token units (not wei)",
		}),
	}
}

// ReportLimitReached increments the Prometheus counter for a rate-limited topup.
func (m *metrics) ReportLimitReached(addr common.Address, limitType string) {
	m.limitsReached.WithLabelValues(addr.Hex(), limitType).Inc()
}

// Push updates gauges and top-up counter deltas from the latest metrics snapshot.
func (m *metrics) Push(rm rebalancer.Metrics) {
	m.checks.Set(float64(rm.TotalChecks))
	m.fundings.Set(float64(rm.TotalFundings))
	if rm.TotalAmountSent != nil {
		m.amountSentNative.Set(weiToNativeTokenFloat(rm.TotalAmountSent))
	}
	m.lastCheckTime.Set(float64(rm.LastCheckTime))
	m.lastFundingTime.Set(float64(rm.LastFundTime))

	m.applyTopupCounterDeltas(rm)
}

func (m *metrics) applyTopupCounterDeltas(rm rebalancer.Metrics) {
	var totalAmt *big.Int
	if rm.TotalAmountSent != nil {
		totalAmt = new(big.Int).Set(rm.TotalAmountSent)
	} else {
		totalAmt = big.NewInt(0)
	}

	// Process restarted or state reset: treat the next Push as a fresh baseline.
	if m.pushInitialized && (rm.TotalFundings < m.prevFundings ||
		(m.prevAmountWei != nil && totalAmt.Cmp(m.prevAmountWei) < 0)) {
		m.pushInitialized = false
		m.prevAmountWei = nil
	}

	if !m.pushInitialized {
		// First snapshot after start: copy current totals into counters so earlier top-ups still count.
		if rm.TotalFundings > 0 {
			m.successfulTopupsTotal.Add(float64(rm.TotalFundings))
		}
		if totalAmt.Sign() > 0 {
			m.topupAmountNativeTotal.Add(weiToNativeTokenFloat(totalAmt))
		}
		m.prevFundings = rm.TotalFundings
		m.prevAmountWei = new(big.Int).Set(totalAmt)
		m.pushInitialized = true
		return
	}

	df := rm.TotalFundings - m.prevFundings
	if df > 0 {
		m.successfulTopupsTotal.Add(float64(df))
	}

	delta := new(big.Int).Sub(totalAmt, m.prevAmountWei)
	if delta.Sign() > 0 {
		m.topupAmountNativeTotal.Add(weiToNativeTokenFloat(delta))
	}

	m.prevFundings = rm.TotalFundings
	m.prevAmountWei.Set(totalAmt)
}

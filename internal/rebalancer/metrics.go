package rebalancer

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/flare-network/rebalancer/pkg/rebalancer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metrics holds Prometheus metrics for the internal rebalancer.
type metrics struct {
	senderBalance         prometheus.Gauge
	limitsReached         *prometheus.CounterVec
	checks                prometheus.Gauge
	fundings              prometheus.Gauge
	amountSentWei         prometheus.Gauge
	lastCheckTime         prometheus.Gauge
	lastFundingTime       prometheus.Gauge
	successfulTopupsTotal prometheus.Counter
	topupAmountWeiTotal   prometheus.Counter

	pushInitialized bool
	prevFundings    uint64
	prevAmountWei   *big.Int
}

// newMetrics creates and registers Prometheus metrics for the rebalancer.
func newMetrics() *metrics {
	return &metrics{
		senderBalance: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "sender_balance_wei",
			Help:      "Current balance of the rebalancer sender address in wei",
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
		amountSentWei: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "amount_sent_wei",
			Help:      "Cumulative amount sent in wei across all top-up transactions",
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
		topupAmountWeiTotal: promauto.NewCounter(prometheus.CounterOpts{
			Namespace: "rebalancer",
			Name:      "topup_amount_wei_total",
			Help:      "Sum of wei sent in successful top-ups (increments by each top-up amount)",
		}),
	}
}

// ReportLimitReached increments the Prometheus counter for a rate-limited topup.
func (m *metrics) ReportLimitReached(addr common.Address, limitType string) {
	m.limitsReached.WithLabelValues(addr.Hex(), limitType).Inc()
}

// Push syncs Prometheus gauges from the latest RebalancerMetrics snapshot.
func (m *metrics) Push(rm rebalancer.RebalancerMetrics) {
	m.checks.Set(float64(rm.TotalChecks))
	m.fundings.Set(float64(rm.TotalFundings))
	if rm.TotalAmountSent != nil {
		f, _ := new(big.Float).SetInt(rm.TotalAmountSent).Float64()
		m.amountSentWei.Set(f)
	}
	m.lastCheckTime.Set(float64(rm.LastCheckTime))
	m.lastFundingTime.Set(float64(rm.LastFundTime))

	m.applyTopupCounterDeltas(rm)
}

func (m *metrics) applyTopupCounterDeltas(rm rebalancer.RebalancerMetrics) {
	var totalAmt *big.Int
	if rm.TotalAmountSent != nil {
		totalAmt = new(big.Int).Set(rm.TotalAmountSent)
	} else {
		totalAmt = big.NewInt(0)
	}
	if m.pushInitialized && (rm.TotalFundings < m.prevFundings ||
		(m.prevAmountWei != nil && totalAmt.Cmp(m.prevAmountWei) < 0)) {
		m.pushInitialized = false
		m.prevAmountWei = nil
	}

	if !m.pushInitialized {
		// Baseline-only would miss top-ups that completed before this first snapshot
		// (e.g. first check cycle funded). Seed counters from current totals once.
		if rm.TotalFundings > 0 {
			m.successfulTopupsTotal.Add(float64(rm.TotalFundings))
		}
		if totalAmt.Sign() > 0 {
			f, _ := new(big.Float).SetInt(totalAmt).Float64()
			m.topupAmountWeiTotal.Add(f)
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
		f, _ := new(big.Float).SetInt(delta).Float64()
		m.topupAmountWeiTotal.Add(f)
	}

	m.prevFundings = rm.TotalFundings
	m.prevAmountWei.Set(totalAmt)
}

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
	senderBalance   prometheus.Gauge
	limitsReached   *prometheus.CounterVec
	checks          prometheus.Gauge
	fundings        prometheus.Gauge
	amountSentWei   prometheus.Gauge
	lastCheckTime   prometheus.Gauge
	lastFundingTime prometheus.Gauge
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
	}
}

// ReportLimitReached increments the Prometheus counter for a rate-limited topup.
func (m *metrics) ReportLimitReached(addr common.Address, limitType string) {
	m.limitsReached.WithLabelValues(addr.Hex(), limitType).Inc()
}

// updateFromRebalancerMetrics syncs Prometheus gauges from the latest RebalancerMetrics snapshot.
func (m *metrics) updateFromRebalancerMetrics(rm rebalancer.RebalancerMetrics) {
	m.checks.Set(float64(rm.TotalChecks))
	m.fundings.Set(float64(rm.TotalFundings))
	if rm.TotalAmountSent != nil {
		f, _ := new(big.Float).SetInt(rm.TotalAmountSent).Float64()
		m.amountSentWei.Set(f)
	}
	m.lastCheckTime.Set(float64(rm.LastCheckTime))
	m.lastFundingTime.Set(float64(rm.LastFundTime))
}

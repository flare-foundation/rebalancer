package rebalancer

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metrics holds Prometheus metrics for the internal rebalancer.
type metrics struct {
	senderBalance prometheus.Gauge
	limitsReached *prometheus.CounterVec
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
	}
}

// ReportLimitReached increments the Prometheus counter for a rate-limited topup.
func (m *metrics) ReportLimitReached(addr common.Address, limitType string) {
	m.limitsReached.WithLabelValues(addr.Hex(), limitType).Inc()
}

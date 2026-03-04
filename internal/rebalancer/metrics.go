package rebalancer

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// metrics holds Prometheus metrics for the internal rebalancer.
type metrics struct {
	senderBalance prometheus.Gauge
}

// newMetrics creates and registers Prometheus metrics for the rebalancer.
func newMetrics() *metrics {
	return &metrics{
		senderBalance: promauto.NewGauge(prometheus.GaugeOpts{
			Namespace: "rebalancer",
			Name:      "sender_balance_wei",
			Help:      "Current balance of the rebalancer sender address in wei",
		}),
	}
}

package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Metrics 服务级别 Prometheus 指标
type Metrics struct {
	rechargeTotal        prometheus.Counter
	spendTotal           prometheus.Counter
	publisherRevenueTotal prometheus.Counter
	kafkaEventsTotal     *prometheus.CounterVec
	kafkaErrorsTotal     prometheus.Counter
}

func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		rechargeTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "billing",
			Name:      "recharge_amount_total",
			Help:      "Total recharge amount",
		}),
		spendTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "billing",
			Name:      "spend_amount_total",
			Help:      "Total spend amount",
		}),
		publisherRevenueTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "billing",
			Name:      "publisher_revenue_total",
			Help:      "Total publisher revenue",
		}),
		kafkaEventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Namespace: "billing",
			Name:      "kafka_events_total",
			Help:      "Kafka events consumed by type",
		}, []string{"type"}),
		kafkaErrorsTotal: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "billing",
			Name:      "kafka_errors_total",
			Help:      "Kafka consumer errors",
		}),
	}
	reg.MustRegister(
		m.rechargeTotal,
		m.spendTotal,
		m.publisherRevenueTotal,
		m.kafkaEventsTotal,
		m.kafkaErrorsTotal,
	)
	return m
}

func (m *Metrics) RecordRecharge(_ int64, amount float64)        { m.rechargeTotal.Add(amount) }
func (m *Metrics) RecordSpend(_ int64, amount float64)           { m.spendTotal.Add(amount) }
func (m *Metrics) RecordPublisherRevenue(_ int64, amount float64) { m.publisherRevenueTotal.Add(amount) }
func (m *Metrics) RecordKafkaEvent(evtType string)               { m.kafkaEventsTotal.WithLabelValues(evtType).Inc() }
func (m *Metrics) RecordKafkaError()                             { m.kafkaErrorsTotal.Inc() }

func Handler(reg prometheus.Gatherer) http.Handler {
	return promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
}

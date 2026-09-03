package metrics

import "github.com/prometheus/client_golang/prometheus"

var seatCapacityActions = []string{
	"reserve_invitation",
	"consume_invitation",
	"claim_share_join",
	"confirm",
	"release",
	"release_member",
}

// SeatCapacityMetrics exposes product-side billing intents without using
// workspace IDs as labels. Operators can alert on an action becoming old or
// entering the terminal dead-letter state while preserving tenant privacy.
type SeatCapacityMetrics struct {
	Pending            *prometheus.GaugeVec
	DeadLettered       *prometheus.GaugeVec
	OldestPendingAge   *prometheus.GaugeVec
	RefreshErrors      prometheus.Counter
	RefreshUnavailable prometheus.Gauge
}

func NewSeatCapacityMetrics() *SeatCapacityMetrics {
	m := &SeatCapacityMetrics{
		Pending: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "seat_capacity_outbox",
			Name:      "pending",
			Help:      "Product-side seat capacity intents waiting to settle by action.",
		}, []string{"action"}),
		DeadLettered: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "seat_capacity_outbox",
			Name:      "dead_lettered",
			Help:      "Terminal seat capacity intents requiring operator repair by action.",
		}, []string{"action"}),
		OldestPendingAge: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "seat_capacity_outbox",
			Name:      "oldest_pending_age_seconds",
			Help:      "Age in seconds of the oldest unsettled seat capacity intent by action.",
		}, []string{"action"}),
		RefreshErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Namespace: "multica",
			Subsystem: "seat_capacity_outbox",
			Name:      "refresh_errors_total",
			Help:      "Total failures while refreshing seat capacity outbox metrics.",
		}),
		RefreshUnavailable: prometheus.NewGauge(prometheus.GaugeOpts{
			Namespace: "multica",
			Subsystem: "seat_capacity_outbox",
			Name:      "refresh_unavailable",
			Help:      "Whether the latest seat capacity outbox metric refresh failed.",
		}),
	}
	m.ResetOutbox()
	return m
}

func (m *SeatCapacityMetrics) ResetOutbox() {
	if m == nil {
		return
	}
	m.Pending.Reset()
	m.DeadLettered.Reset()
	m.OldestPendingAge.Reset()
	for _, action := range seatCapacityActions {
		m.SetOutbox(action, 0, 0, 0)
	}
}

func (m *SeatCapacityMetrics) SetOutbox(action string, pending, deadLettered int64, oldestPendingAgeSeconds float64) {
	if m == nil {
		return
	}
	m.Pending.WithLabelValues(action).Set(float64(pending))
	m.DeadLettered.WithLabelValues(action).Set(float64(deadLettered))
	m.OldestPendingAge.WithLabelValues(action).Set(oldestPendingAgeSeconds)
}

func (m *SeatCapacityMetrics) RecordOutboxRefreshError() {
	if m == nil {
		return
	}
	m.RefreshErrors.Inc()
}

func (m *SeatCapacityMetrics) SetOutboxRefreshUnavailable(unavailable bool) {
	if m == nil {
		return
	}
	value := 0.0
	if unavailable {
		value = 1
	}
	m.RefreshUnavailable.Set(value)
}

func (m *SeatCapacityMetrics) Collectors() []prometheus.Collector {
	return []prometheus.Collector{
		m.Pending, m.DeadLettered, m.OldestPendingAge,
		m.RefreshErrors, m.RefreshUnavailable,
	}
}

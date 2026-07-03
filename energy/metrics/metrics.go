// Package metrics publishes the aitra_* energy metrics to Prometheus. It is the
// "display" end of the no-hardware link: the aggregator hands it derived
// MeasurementRecords and it sets the corresponding gauges/counters. Label sets
// follow the stable-metric contract; adding or removing a label on a stable
// metric is a breaking change.
package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"

	"github.com/aitra-ai/aitra-server/energy"
	"github.com/aitra-ai/aitra-server/energy/aggregator"
)

// PrometheusSink satisfies aggregator.MetricsSink.
var _ aggregator.MetricsSink = (*PrometheusSink)(nil)

// PrometheusSink holds the aitra_* collectors and the site factors needed to
// derive carbon and cost from J/token.
type PrometheusSink struct {
	site        energy.SiteParams
	carbonLabel string
	costLabel   string

	jPerToken      *prometheus.GaugeVec
	cv             *prometheus.GaugeVec
	stable         *prometheus.GaugeVec
	idlePower      *prometheus.GaugeVec
	tokensPerJoule *prometheus.GaugeVec
	co2            *prometheus.GaugeVec
	cost           *prometheus.GaugeVec
	nsEnergy       *prometheus.CounterVec
	nsTokens       *prometheus.CounterVec
}

// NewPrometheusSink registers the collectors on reg (the platform's registry, or
// a fresh one in tests; nil uses the default registry).
func NewPrometheusSink(reg prometheus.Registerer, site energy.SiteParams) *PrometheusSink {
	if reg == nil {
		reg = prometheus.DefaultRegisterer
	}
	f := promauto.With(reg)

	carbon := site.CarbonSource
	if carbon == "" {
		carbon = "manual"
	}

	return &PrometheusSink{
		site:        site,
		carbonLabel: carbon,
		costLabel:   "manual",
		jPerToken: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aitra_j_per_token", Help: "Energy per output token (J/token).",
		}, []string{"namespace", "workload", "model", "hardware", "precision", "calibration_tier", "attribution_method"}),
		cv: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aitra_measurement_cv", Help: "Rolling coefficient of variation of J/token.",
		}, []string{"node", "model_name"}),
		stable: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aitra_measurement_window_stable", Help: "1 when CV < threshold, else 0 (flagged, not suppressed).",
		}, []string{"node", "model_name"}),
		idlePower: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aitra_idle_power_watts", Help: "Accelerator power draw during idle (zero-token) windows.",
		}, []string{"node"}),
		tokensPerJoule: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aitra_tokens_per_joule", Help: "Inverse efficiency: tokens per joule.",
		}, []string{"namespace", "workload", "model", "hardware"}),
		co2: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aitra_co2_per_token_grams", Help: "Carbon per token (gCO2), derived from J/token, PUE and grid intensity.",
		}, []string{"namespace", "workload", "model", "hardware", "carbon_source"}),
		cost: f.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aitra_cost_per_million_tokens_usd", Help: "Electricity cost per million tokens (USD).",
		}, []string{"namespace", "workload", "model", "hardware", "cost_source"}),
		nsEnergy: f.NewCounterVec(prometheus.CounterOpts{
			Name: "aitra_namespace_energy_joules_total", Help: "Cumulative energy attributed to a namespace.",
		}, []string{"namespace", "attribution_method"}),
		nsTokens: f.NewCounterVec(prometheus.CounterOpts{
			Name: "aitra_namespace_tokens_total", Help: "Cumulative output tokens per namespace.",
		}, []string{"namespace"}),
	}
}

// Observe publishes one derived measurement record.
func (s *PrometheusSink) Observe(r energy.MeasurementRecord) {
	s.jPerToken.WithLabelValues(
		r.Namespace, r.Workload, r.Model, r.Hardware, r.Precision,
		string(r.CalibrationTier), string(r.AttributionMethod),
	).Set(r.JPerToken)

	s.cv.WithLabelValues(r.Node, r.Model).Set(r.CV)

	stable := 0.0
	if r.Stable {
		stable = 1.0
	}
	s.stable.WithLabelValues(r.Node, r.Model).Set(stable)

	s.tokensPerJoule.WithLabelValues(r.Namespace, r.Workload, r.Model, r.Hardware).
		Set(energy.TokensPerJoule(r.JPerToken))
	s.co2.WithLabelValues(r.Namespace, r.Workload, r.Model, r.Hardware, s.carbonLabel).
		Set(energy.CO2PerTokenGrams(r.JPerToken, s.site))
	s.cost.WithLabelValues(r.Namespace, r.Workload, r.Model, r.Hardware, s.costLabel).
		Set(energy.CostPerMillionTokensUSD(r.JPerToken, s.site))

	s.nsEnergy.WithLabelValues(r.Namespace, string(r.AttributionMethod)).Add(r.EnergyJoules)
	s.nsTokens.WithLabelValues(r.Namespace).Add(float64(r.OutputTokens))
}

// ObserveIdle publishes idle power for a node.
func (s *PrometheusSink) ObserveIdle(node string, watts float64) {
	s.idlePower.WithLabelValues(node).Set(watts)
}

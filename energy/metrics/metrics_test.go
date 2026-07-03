package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"

	"opencsg.com/csghub-server/energy"
)

// mustValue gathers from the registry and returns the value of the named metric
// with exactly the given label set. It avoids prometheus/testutil so this test
// does not expand the module graph (and the project's go.sum) for a helper.
func mustValue(t *testing.T, reg *prometheus.Registry, name string, labels map[string]string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	require.NoError(t, err)
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			got := map[string]string{}
			for _, lp := range m.GetLabel() {
				got[lp.GetName()] = lp.GetValue()
			}
			if len(got) != len(labels) {
				continue
			}
			match := true
			for k, v := range labels {
				if got[k] != v {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			if g := m.GetGauge(); g != nil {
				return g.GetValue()
			}
			if c := m.GetCounter(); c != nil {
				return c.GetValue()
			}
		}
	}
	require.Failf(t, "metric not found", "%s %v", name, labels)
	return 0
}

func TestPrometheusSinkObserve(t *testing.T) {
	reg := prometheus.NewRegistry() // isolated registry per test, no global state
	site := energy.SiteParams{PUE: 1.5, GridGCO2PerKWh: 400, CostPerKWh: 0.12, CarbonSource: "manual"}
	s := NewPrometheusSink(reg, site)

	s.Observe(energy.MeasurementRecord{
		Namespace: "team-a", Workload: "chat", Model: "qwen", Hardware: "h100", Precision: "fp16",
		CalibrationTier: energy.CalibrationUncalibrated, AttributionMethod: energy.AttributionDirect,
		Node: "n1", EnergyJoules: 200, OutputTokens: 100, JPerToken: 2.0, CV: 0.0, Stable: true,
	})

	require.InEpsilon(t, 2.0, mustValue(t, reg, "aitra_j_per_token", map[string]string{
		"namespace": "team-a", "workload": "chat", "model": "qwen", "hardware": "h100",
		"precision": "fp16", "calibration_tier": "uncalibrated", "attribution_method": "direct",
	}), 1e-9)
	require.Equal(t, 1.0, mustValue(t, reg, "aitra_measurement_window_stable",
		map[string]string{"node": "n1", "model_name": "qwen"}))
	require.InEpsilon(t, 0.5, mustValue(t, reg, "aitra_tokens_per_joule", map[string]string{
		"namespace": "team-a", "workload": "chat", "model": "qwen", "hardware": "h100",
	}), 1e-9)
	require.InEpsilon(t, 2.0*1.5/3_600_000*400, mustValue(t, reg, "aitra_co2_per_token_grams", map[string]string{
		"namespace": "team-a", "workload": "chat", "model": "qwen", "hardware": "h100", "carbon_source": "manual",
	}), 1e-9)
	require.InEpsilon(t, 200.0, mustValue(t, reg, "aitra_namespace_energy_joules_total",
		map[string]string{"namespace": "team-a", "attribution_method": "direct"}), 1e-9)
	require.InEpsilon(t, 100.0, mustValue(t, reg, "aitra_namespace_tokens_total",
		map[string]string{"namespace": "team-a"}), 1e-9)

	// counters accumulate across windows
	s.Observe(energy.MeasurementRecord{
		Namespace: "team-a", Model: "qwen", Hardware: "h100",
		AttributionMethod: energy.AttributionDirect, CalibrationTier: energy.CalibrationUncalibrated,
		Node: "n1", EnergyJoules: 50, OutputTokens: 25, JPerToken: 2.0,
	})
	require.InEpsilon(t, 250.0, mustValue(t, reg, "aitra_namespace_energy_joules_total",
		map[string]string{"namespace": "team-a", "attribution_method": "direct"}), 1e-9)

	s.ObserveIdle("n9", 75)
	require.Equal(t, 75.0, mustValue(t, reg, "aitra_idle_power_watts", map[string]string{"node": "n9"}))
}

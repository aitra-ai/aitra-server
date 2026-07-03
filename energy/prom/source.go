package prom

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aitra-ai/aitra-server/energy/aggregator"
)

// Source implements aggregator.EnergySource by querying Prometheus for
// DCGM-exporter metrics. It is read-only and out-of-band.
var _ aggregator.EnergySource = (*Source)(nil)

// Config names the metrics and labels to query. Defaults target DCGM-exporter
// with Kubernetes pod attribution enabled (DCGM_EXPORTER_KUBERNETES=true). Note
// the pod-attribution label set is deployment-dependent — verify against your
// exporter (`curl :9400/metrics`) and override here if it differs.
type Config struct {
	EnergyMetric   string // cumulative energy counter, in millijoules
	PowerMetric    string // instantaneous power gauge, in watts
	NamespaceLabel string
	PodLabel       string
	NodeLabel      string
}

func (c Config) withDefaults() Config {
	if c.EnergyMetric == "" {
		c.EnergyMetric = "DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION" // mJ counter
	}
	if c.PowerMetric == "" {
		c.PowerMetric = "DCGM_FI_DEV_POWER_USAGE" // W gauge
	}
	if c.NamespaceLabel == "" {
		c.NamespaceLabel = "namespace"
	}
	if c.PodLabel == "" {
		c.PodLabel = "pod"
	}
	if c.NodeLabel == "" {
		c.NodeLabel = "Hostname" // DCGM-exporter's node label
	}
	return c
}

// Source reads energy via a Prometheus Client.
type Source struct {
	client Client
	cfg    Config
}

// NewSource builds an EnergySource over client.
func NewSource(client Client, cfg Config) *Source {
	return &Source{client: client, cfg: cfg.withDefaults()}
}

// Name identifies the provider on every record.
func (s *Source) Name() string { return "dcgm" }

// WindowJoules returns Δjoules for scope over [start, end). DCGM's energy
// counter is in millijoules, so the windowed increase is divided by 1000. The
// result is clamped at 0 to honour the "never negative" contract (counter resets
// or extrapolation can otherwise produce a small negative).
func (s *Source) WindowJoules(ctx context.Context, scope aggregator.Scope, start, end time.Time) (float64, error) {
	dur := end.Sub(start)
	if dur <= 0 {
		return 0, fmt.Errorf("prom: invalid window [%s, %s]", start, end)
	}
	query := fmt.Sprintf("sum(increase(%s[%s]))", s.metricSelector(s.cfg.EnergyMetric, scope), promDuration(dur))
	milliJoules, err := s.client.Query(ctx, query, end)
	if err != nil {
		return 0, err
	}
	joules := milliJoules / 1000.0
	if joules < 0 {
		joules = 0
	}
	return joules, nil
}

// IdlePower returns current total power (W) for scope.
func (s *Source) IdlePower(ctx context.Context, scope aggregator.Scope) (float64, error) {
	query := fmt.Sprintf("sum(%s)", s.metricSelector(s.cfg.PowerMetric, scope))
	return s.client.Query(ctx, query, time.Time{})
}

// metricSelector renders metric{label="v",...}, omitting the braces when scope is empty.
func (s *Source) metricSelector(metric string, scope aggregator.Scope) string {
	var parts []string
	if scope.Namespace != "" {
		parts = append(parts, fmt.Sprintf("%s=%q", s.cfg.NamespaceLabel, scope.Namespace))
	}
	if scope.PodSelector != "" {
		parts = append(parts, fmt.Sprintf("%s=~%q", s.cfg.PodLabel, scope.PodSelector))
	}
	if scope.Node != "" {
		parts = append(parts, fmt.Sprintf("%s=%q", s.cfg.NodeLabel, scope.Node))
	}
	if len(parts) == 0 {
		return metric
	}
	return fmt.Sprintf("%s{%s}", metric, strings.Join(parts, ","))
}

// promDuration formats a duration as Prometheus range syntax in whole seconds
// (e.g. 60s, 300s). time.Duration.String() ("1m0s") is not valid PromQL.
func promDuration(d time.Duration) string {
	secs := int64(d.Seconds())
	if secs < 1 {
		secs = 1
	}
	return fmt.Sprintf("%ds", secs)
}

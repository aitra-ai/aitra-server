package prom

import (
	"context"
	"fmt"
	"time"

	"github.com/aitra-ai/aitra-server/energy/aggregator"
)

// TokenSource implements aggregator.TokenSource by querying Prometheus for an
// inference server's cumulative output-token counter. It is the
// "generic-prometheus" inference provider: any server exposing a monotonic
// output-token counter (vLLM, TGI, SGLang, Triton, ...) is supported by setting
// TokensMetric. It is read-only and out-of-band, symmetric to the energy Source.
var _ aggregator.TokenSource = (*TokenSource)(nil)

// TokenConfig names the metric and label used to read output tokens. Defaults
// target vLLM's generation-token counter, labelled by model_name.
type TokenConfig struct {
	TokensMetric string // cumulative output-token counter (monotonic)
	KeyLabel     string // label matched against the deployment's metering key
}

func (c TokenConfig) withDefaults() TokenConfig {
	if c.TokensMetric == "" {
		c.TokensMetric = "vllm:generation_tokens_total"
	}
	if c.KeyLabel == "" {
		c.KeyLabel = "model_name"
	}
	return c
}

// TokenSource reads output tokens via a Prometheus Client.
type TokenSource struct {
	client Client
	cfg    TokenConfig
	name   string
}

// NewTokenSource builds a TokenSource over client. name identifies the provider
// on every record (e.g. "vllm", "generic-prometheus"); empty defaults to the latter.
func NewTokenSource(client Client, cfg TokenConfig, name string) *TokenSource {
	if name == "" {
		name = "generic-prometheus"
	}
	return &TokenSource{client: client, cfg: cfg.withDefaults(), name: name}
}

// Name identifies the inference provider on every record.
func (s *TokenSource) Name() string { return s.name }

// WindowTokens returns Δoutput-tokens for the series identified by meteringKey
// over [start, end), as the windowed increase of the cumulative counter. An
// empty meteringKey sums every series of the metric (single-model setups). The
// result is clamped at 0 to absorb counter resets / extrapolation underflow.
func (s *TokenSource) WindowTokens(ctx context.Context, meteringKey string, start, end time.Time) (uint64, error) {
	dur := end.Sub(start)
	if dur <= 0 {
		return 0, fmt.Errorf("prom: invalid token window [%s, %s]", start, end)
	}
	query := fmt.Sprintf("sum(increase(%s[%s]))", s.selector(meteringKey), promDuration(dur))
	v, err := s.client.Query(ctx, query, end)
	if err != nil {
		return 0, err
	}
	if v < 0 {
		v = 0
	}
	return uint64(v), nil
}

// selector renders metric{keyLabel="meteringKey"}, or the bare metric when no key.
func (s *TokenSource) selector(meteringKey string) string {
	if meteringKey == "" {
		return s.cfg.TokensMetric
	}
	return fmt.Sprintf("%s{%s=%q}", s.cfg.TokensMetric, s.cfg.KeyLabel, meteringKey)
}

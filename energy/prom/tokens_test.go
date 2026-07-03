package prom_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/aitra-ai/aitra-server/energy/prom"
)

func TestWindowTokensSumsIncrease(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	// Two replicas, increase 1200 and 800 tokens -> 2000.
	ts.body = `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"pod":"a"},"value":[1749816000,"1200"]},
		{"metric":{"pod":"b"},"value":[1749816000,"800"]}
	]}}`

	src := prom.NewTokenSource(prom.NewClient(ts.URL, ""), prom.TokenConfig{}, "vllm")
	tokens, err := src.WindowTokens(context.Background(), "qwen3", base.Add(-time.Minute), base)
	require.NoError(t, err)
	require.Equal(t, uint64(2000), tokens)
	require.Equal(t, "vllm", src.Name())

	// query shape: sum(increase(vllm:generation_tokens_total{model_name="qwen3"}[60s]))
	require.Contains(t, ts.lastQuery, "sum(increase(vllm:generation_tokens_total{")
	require.Contains(t, ts.lastQuery, `model_name="qwen3"`)
	require.Contains(t, ts.lastQuery, "[60s]")
}

func TestWindowTokensEmptyKeyIsBareMetric(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	ts.body = `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{},"value":[1749816000,"512"]}
	]}}`
	src := prom.NewTokenSource(prom.NewClient(ts.URL, ""), prom.TokenConfig{}, "")
	tokens, err := src.WindowTokens(context.Background(), "", base.Add(-time.Minute), base)
	require.NoError(t, err)
	require.Equal(t, uint64(512), tokens)
	require.Equal(t, "generic-prometheus", src.Name(), "empty name defaults")
	require.Contains(t, ts.lastQuery, "sum(increase(vllm:generation_tokens_total[60s]))", "no key -> no braces")
}

func TestWindowTokensCustomMetric(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	ts.body = `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1749816000,"42"]}]}}`
	// e.g. TGI / SGLang counter via a custom metric + label.
	src := prom.NewTokenSource(prom.NewClient(ts.URL, ""),
		prom.TokenConfig{TokensMetric: "tgi_request_generated_tokens", KeyLabel: "model"}, "tgi")
	_, err := src.WindowTokens(context.Background(), "llama", base.Add(-time.Minute), base)
	require.NoError(t, err)
	require.Contains(t, ts.lastQuery, `sum(increase(tgi_request_generated_tokens{model="llama"}[60s]))`)
}

func TestWindowTokensNegativeClampedToZero(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	ts.body = `{"status":"success","data":{"resultType":"vector","result":[{"metric":{},"value":[1749816000,"-5"]}]}}`
	src := prom.NewTokenSource(prom.NewClient(ts.URL, ""), prom.TokenConfig{}, "vllm")
	tokens, err := src.WindowTokens(context.Background(), "", base.Add(-time.Minute), base)
	require.NoError(t, err)
	require.Equal(t, uint64(0), tokens, "counter reset must not underflow to a huge uint64")
}

func TestWindowTokensZeroWindowErrors(t *testing.T) {
	src := prom.NewTokenSource(prom.NewClient("http://unused", ""), prom.TokenConfig{}, "vllm")
	_, err := src.WindowTokens(context.Background(), "", base, base)
	require.Error(t, err)
}

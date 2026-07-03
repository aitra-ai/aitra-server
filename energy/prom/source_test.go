package prom_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/aitra-ai/aitra-server/energy/aggregator"
	"github.com/aitra-ai/aitra-server/energy/prom"
)

// testServer records the last query it received and returns a canned body.
type testServer struct {
	*httptest.Server
	lastQuery string
	lastTime  string
	status    int
	body      string
}

func newTestServer() *testServer {
	ts := &testServer{status: http.StatusOK}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ts.lastQuery = r.URL.Query().Get("query")
		ts.lastTime = r.URL.Query().Get("time")
		w.WriteHeader(ts.status)
		_, _ = w.Write([]byte(ts.body))
	}))
	return ts
}

var base = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

func TestWindowJoulesSumsAndConvertsMilliJoules(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	// Two GPU series, increase 100000 and 50000 mJ -> 150000 mJ total -> 150 J.
	ts.body = `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{"gpu":"0"},"value":[1749816000,"100000"]},
		{"metric":{"gpu":"1"},"value":[1749816000,"50000"]}
	]}}`

	src := prom.NewSource(prom.NewClient(ts.URL, ""), prom.Config{})
	joules, err := src.WindowJoules(context.Background(),
		aggregator.Scope{Namespace: "team-a", PodSelector: "vllm-.*", Node: "n1"},
		base.Add(-time.Minute), base)
	require.NoError(t, err)
	require.InEpsilon(t, 150.0, joules, 1e-9, "mJ summed and converted to J")

	// query shape: sum(increase(METRIC{selector}[60s])) evaluated at end time
	require.Contains(t, ts.lastQuery, "sum(increase(DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION{")
	require.Contains(t, ts.lastQuery, `namespace="team-a"`)
	require.Contains(t, ts.lastQuery, `pod=~"vllm-.*"`)
	require.Contains(t, ts.lastQuery, `Hostname="n1"`)
	require.Contains(t, ts.lastQuery, "[60s]")
	require.Equal(t, strconv.FormatInt(base.Unix(), 10), ts.lastTime, "instant query pinned to window end")
}

func TestWindowJoulesEmptyResultIsZero(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	ts.body = `{"status":"success","data":{"resultType":"vector","result":[]}}`

	src := prom.NewSource(prom.NewClient(ts.URL, ""), prom.Config{})
	joules, err := src.WindowJoules(context.Background(), aggregator.Scope{}, base.Add(-time.Minute), base)
	require.NoError(t, err)
	require.Equal(t, 0.0, joules)
	// no scope -> bare metric, no braces
	require.Contains(t, ts.lastQuery, "sum(increase(DCGM_FI_DEV_TOTAL_ENERGY_CONSUMPTION[60s]))")
}

func TestIdlePower(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	ts.body = `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{},"value":[1749816000,"73.5"]}
	]}}`
	src := prom.NewSource(prom.NewClient(ts.URL, ""), prom.Config{})
	watts, err := src.IdlePower(context.Background(), aggregator.Scope{Node: "n1"})
	require.NoError(t, err)
	require.InEpsilon(t, 73.5, watts, 1e-9)
	require.Contains(t, ts.lastQuery, "sum(DCGM_FI_DEV_POWER_USAGE{")
}

func TestQueryNon200IsError(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	ts.status = http.StatusBadGateway
	ts.body = "upstream down"
	src := prom.NewSource(prom.NewClient(ts.URL, ""), prom.Config{})
	_, err := src.WindowJoules(context.Background(), aggregator.Scope{}, base.Add(-time.Minute), base)
	require.Error(t, err)
	require.Contains(t, err.Error(), "502")
}

func TestQueryNegativeIncreaseClampedToZero(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	// extrapolation / counter reset can yield a small negative; must not propagate.
	ts.body = `{"status":"success","data":{"resultType":"vector","result":[
		{"metric":{},"value":[1749816000,"-12"]}
	]}}`
	src := prom.NewSource(prom.NewClient(ts.URL, ""), prom.Config{})
	joules, err := src.WindowJoules(context.Background(), aggregator.Scope{}, base.Add(-time.Minute), base)
	require.NoError(t, err)
	require.Equal(t, 0.0, joules, "never negative (EnergyProvider contract)")
}

func TestStatusErrorPropagated(t *testing.T) {
	ts := newTestServer()
	defer ts.Close()
	ts.body = `{"status":"error","errorType":"bad_data","error":"parse error"}`
	src := prom.NewSource(prom.NewClient(ts.URL, ""), prom.Config{})
	_, err := src.IdlePower(context.Background(), aggregator.Scope{})
	require.Error(t, err)
	require.Contains(t, err.Error(), "parse error")
}

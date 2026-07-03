package aggregator_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"opencsg.com/csghub-server/energy"
	"opencsg.com/csghub-server/energy/aggregator"
	"opencsg.com/csghub-server/energy/storage/memory"
)

// --- fakes: all out-of-band, none touch the inference path ---

type fakeDeploys struct{ list []aggregator.Deployment }

func (f *fakeDeploys) ListRunning(context.Context) ([]aggregator.Deployment, error) {
	return f.list, nil
}

type fakeEnergy struct {
	name   string
	joules func(scope aggregator.Scope, start, end time.Time) (float64, error)
	idle   float64
}

func (f *fakeEnergy) WindowJoules(_ context.Context, s aggregator.Scope, start, end time.Time) (float64, error) {
	return f.joules(s, start, end)
}
func (f *fakeEnergy) IdlePower(context.Context, aggregator.Scope) (float64, error) {
	return f.idle, nil
}
func (f *fakeEnergy) Name() string { return f.name }

type fakeTokens struct {
	name   string
	tokens func(key string, start, end time.Time) (uint64, error)
}

func (f *fakeTokens) WindowTokens(_ context.Context, key string, start, end time.Time) (uint64, error) {
	return f.tokens(key, start, end)
}
func (f *fakeTokens) Name() string { return f.name }

type idleObs struct {
	node  string
	watts float64
}

type recordingSink struct {
	records []energy.MeasurementRecord
	idle    []idleObs
}

func (s *recordingSink) Observe(r energy.MeasurementRecord) { s.records = append(s.records, r) }
func (s *recordingSink) ObserveIdle(node string, watts float64) {
	s.idle = append(s.idle, idleObs{node, watts})
}

func constJoules(v float64) func(aggregator.Scope, time.Time, time.Time) (float64, error) {
	return func(aggregator.Scope, time.Time, time.Time) (float64, error) { return v, nil }
}
func constTokens(v uint64) func(string, time.Time, time.Time) (uint64, error) {
	return func(string, time.Time, time.Time) (uint64, error) { return v, nil }
}

var base = time.Date(2026, 6, 13, 12, 0, 0, 0, time.UTC)

// End-to-end no-hardware proof: fake energy + fake tokens → real core → recorded
// records, with the system's honesty invariants intact.
func TestRunCycleEndToEnd(t *testing.T) {
	deploys := &fakeDeploys{list: []aggregator.Deployment{
		{ID: 1, Namespace: "team-a", Node: "n1", Model: "qwen", Hardware: "h100"}, // workload empty -> "unknown"
		{ID: 2, Namespace: "team-b", Node: "n2", Model: "llama", Hardware: "h100", Workload: "chat"},
	}}
	// d1: 200J/100tok -> 2.0; d2: 50J/1000tok -> 0.05. Energy keyed by scope
	// namespace, tokens keyed by metering key.
	enMap := map[string]float64{"team-a": 200, "team-b": 50}
	tokMap := map[string]uint64{"svc-a": 100, "svc-b": 1000}
	es := &fakeEnergy{name: "fake-dcgm", idle: 60, joules: func(s aggregator.Scope, _, _ time.Time) (float64, error) {
		return enMap[s.Namespace], nil
	}}
	ts := &fakeTokens{name: "fake-meter", tokens: func(key string, _, _ time.Time) (uint64, error) {
		return tokMap[key], nil
	}}
	deploys.list[0].Scope = aggregator.Scope{Namespace: "team-a"}
	deploys.list[1].Scope = aggregator.Scope{Namespace: "team-b"}
	deploys.list[0].MeteringKey = "svc-a"
	deploys.list[1].MeteringKey = "svc-b"

	sink := &recordingSink{}
	a := aggregator.New(aggregator.Config{Window: time.Minute}, deploys, es, ts, sink)

	require.NoError(t, a.RunCycle(context.Background(), base))
	require.Len(t, sink.records, 2)

	byNS := map[string]energy.MeasurementRecord{}
	for _, r := range sink.records {
		byNS[r.Namespace] = r
		require.NotEmpty(t, r.AttributionMethod, "attribution never empty")
		require.Equal(t, "fake-dcgm", r.EnergyProvider, "energy provenance recorded")
		require.Equal(t, "fake-meter", r.InferenceProvider, "token provenance recorded")
		require.Equal(t, energy.CalibrationUncalibrated, r.CalibrationTier)
	}
	require.InEpsilon(t, 2.0, byNS["team-a"].JPerToken, 1e-9)
	require.InEpsilon(t, 0.05, byNS["team-b"].JPerToken, 1e-9)
	require.Equal(t, "unknown", byNS["team-a"].Workload, "missing workload -> unknown, not error")
	require.Equal(t, "chat", byNS["team-b"].Workload)
}

// The cluster value the records imply must be Σenergy ÷ Σtokens, not the mean.
func TestClusterAcrossDeployments(t *testing.T) {
	deploys := &fakeDeploys{list: []aggregator.Deployment{
		{ID: 1, Namespace: "a", Node: "n1", Model: "m", Scope: aggregator.Scope{Namespace: "a"}, MeteringKey: "svc-a"},
		{ID: 2, Namespace: "b", Node: "n2", Model: "m", Scope: aggregator.Scope{Namespace: "b"}, MeteringKey: "svc-b"},
	}}
	enMap := map[string]float64{"a": 200, "b": 50}
	tokMap := map[string]uint64{"svc-a": 100, "svc-b": 1000}
	es := &fakeEnergy{name: "e", joules: func(s aggregator.Scope, _, _ time.Time) (float64, error) { return enMap[s.Namespace], nil }}
	ts := &fakeTokens{name: "t", tokens: func(key string, _, _ time.Time) (uint64, error) { return tokMap[key], nil }}
	sink := &recordingSink{}
	a := aggregator.New(aggregator.Config{Window: time.Minute}, deploys, es, ts, sink)
	require.NoError(t, a.RunCycle(context.Background(), base))

	cluster := energy.ClusterJPerToken(sink.records)
	require.InEpsilon(t, 250.0/1100.0, cluster, 1e-9)
	require.Greater(t, (2.0+0.05)/2.0-cluster, 0.5, "cluster value must not be the per-series mean")
}

// Idle window: zero tokens -> no J/token record, idle power observed instead.
func TestIdleWindow(t *testing.T) {
	deploys := &fakeDeploys{list: []aggregator.Deployment{{ID: 1, Node: "n1", Model: "m"}}}
	es := &fakeEnergy{name: "e", idle: 73.5, joules: constJoules(40)}
	ts := &fakeTokens{name: "t", tokens: constTokens(0)} // idle
	sink := &recordingSink{}
	a := aggregator.New(aggregator.Config{Window: time.Minute}, deploys, es, ts, sink)
	require.NoError(t, a.RunCycle(context.Background(), base))

	require.Empty(t, sink.records, "idle window is not counted in J/token")
	require.Len(t, sink.idle, 1)
	require.Equal(t, "n1", sink.idle[0].node)
	require.Equal(t, 73.5, sink.idle[0].watts)
}

// Per-series CV gate works across cycles: steady series is stable, a spike flags it.
func TestCVConvergenceAcrossCycles(t *testing.T) {
	deploys := &fakeDeploys{list: []aggregator.Deployment{{ID: 1, Node: "n1", Model: "m"}}}
	jouleSeq := []float64{1000, 1000, 1000, 2000} // tokens fixed at 100 -> jpt 10,10,10,20
	cycle := 0
	es := &fakeEnergy{name: "e", joules: func(aggregator.Scope, time.Time, time.Time) (float64, error) {
		return jouleSeq[cycle], nil
	}}
	ts := &fakeTokens{name: "t", tokens: constTokens(100)}
	sink := &recordingSink{}
	a := aggregator.New(aggregator.Config{Window: time.Minute}, deploys, es, ts, sink)

	for c := range jouleSeq {
		cycle = c
		require.NoError(t, a.RunCycle(context.Background(), base.Add(time.Duration(c)*time.Minute)))
	}

	require.Len(t, sink.records, len(jouleSeq))
	require.InEpsilon(t, 10.0, sink.records[0].JPerToken, 1e-9)
	require.True(t, sink.records[2].Stable, "steady series should be stable")
	require.InEpsilon(t, 20.0, sink.records[3].JPerToken, 1e-9)
	require.False(t, sink.records[3].Stable, "a spike must flag the window unstable, not suppress it")
}

// A failure on one deployment is isolated; others still produce records, and the
// cycle never errors out — protecting both the loop and inference timeliness.
func TestFailureIsolation(t *testing.T) {
	deploys := &fakeDeploys{list: []aggregator.Deployment{
		{ID: 1, Namespace: "bad", Node: "n1", Model: "m", Scope: aggregator.Scope{Namespace: "bad"}},
		{ID: 2, Namespace: "good", Node: "n2", Model: "m", Scope: aggregator.Scope{Namespace: "good"}},
	}}
	es := &fakeEnergy{name: "e", joules: func(s aggregator.Scope, _, _ time.Time) (float64, error) {
		if s.Namespace == "bad" {
			return 0, errors.New("prometheus unreachable")
		}
		return 100, nil
	}}
	ts := &fakeTokens{name: "t", tokens: constTokens(50)}
	sink := &recordingSink{}
	a := aggregator.New(aggregator.Config{Window: time.Minute}, deploys, es, ts, sink)

	require.NoError(t, a.RunCycle(context.Background(), base), "one bad deployment must not fail the cycle")
	require.Len(t, sink.records, 1)
	require.Equal(t, "good", sink.records[0].Namespace)
}

// With a StorageBackend attached, a cycle's measured records are persisted and
// answer a chargeback query; idle deployments produce no stored record.
func TestPersistsToStorage(t *testing.T) {
	deploys := &fakeDeploys{list: []aggregator.Deployment{
		{ID: 1, Namespace: "team-a", Node: "n1", Model: "m", Scope: aggregator.Scope{Namespace: "team-a"}, MeteringKey: "svc-a"},
		{ID: 2, Namespace: "team-b", Node: "n2", Model: "m", Scope: aggregator.Scope{Namespace: "team-b"}, MeteringKey: "svc-b"}, // idle
	}}
	enMap := map[string]float64{"team-a": 200, "team-b": 999}
	tokMap := map[string]uint64{"svc-a": 100, "svc-b": 0} // svc-b idle
	es := &fakeEnergy{name: "e", joules: func(s aggregator.Scope, _, _ time.Time) (float64, error) { return enMap[s.Namespace], nil }}
	ts := &fakeTokens{name: "t", tokens: func(key string, _, _ time.Time) (uint64, error) { return tokMap[key], nil }}
	sink := &recordingSink{}

	store := memory.New()
	a := aggregator.New(aggregator.Config{Window: time.Minute}, deploys, es, ts, sink, aggregator.WithStorage(store))
	require.NoError(t, a.RunCycle(context.Background(), base))

	recs := store.Records()
	require.Len(t, recs, 1, "only the non-idle deployment persists a record")
	require.Equal(t, "team-a", recs[0].Namespace)

	rows, err := store.QueryChargeback(context.Background(), energy.ChargebackQuery{
		Start: base.Add(-time.Hour), End: base.Add(time.Hour), PUE: 1.0,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 200.0, rows[0].EnergyJoules)
	require.Equal(t, uint64(100), rows[0].OutputTokens)
}

// A nil StorageBackend (the default) is safe: the cycle still measures and emits
// metrics, it just doesn't persist.
func TestNilStorageIsSafe(t *testing.T) {
	deploys := &fakeDeploys{list: []aggregator.Deployment{{ID: 1, Node: "n1", Model: "m", Scope: aggregator.Scope{Namespace: "a"}}}}
	es := &fakeEnergy{name: "e", joules: constJoules(100)}
	ts := &fakeTokens{name: "t", tokens: constTokens(50)}
	sink := &recordingSink{}
	a := aggregator.New(aggregator.Config{Window: time.Minute}, deploys, es, ts, sink) // no WithStorage
	require.NoError(t, a.RunCycle(context.Background(), base))
	require.Len(t, sink.records, 1, "metrics still emitted without storage")
}

// A cancelled context stops the cycle promptly without measuring.
func TestContextCancelledStopsCycle(t *testing.T) {
	deploys := &fakeDeploys{list: []aggregator.Deployment{{ID: 1, Node: "n1", Model: "m"}}}
	es := &fakeEnergy{name: "e", joules: constJoules(100)}
	ts := &fakeTokens{name: "t", tokens: constTokens(50)}
	sink := &recordingSink{}
	a := aggregator.New(aggregator.Config{Window: time.Minute}, deploys, es, ts, sink)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := a.RunCycle(ctx, base)
	require.ErrorIs(t, err, context.Canceled)
	require.Empty(t, sink.records)
}

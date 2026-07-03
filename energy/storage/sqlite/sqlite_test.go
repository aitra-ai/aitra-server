package sqlite_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/aitra-ai/aitra-server/energy"
	"github.com/aitra-ai/aitra-server/energy/storage/sqlite"
)

func newBackend(t *testing.T) *sqlite.Backend {
	t.Helper()
	b, err := sqlite.New("") // shared in-memory
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, b.Close()) })
	return b
}

// Registered via init() and selectable through the registry by Helm value name.
func TestRegisteredViaFactory(t *testing.T) {
	b, err := energy.NewStorage("sqlite", map[string]string{"path": ""})
	require.NoError(t, err, "sqlite backend registers itself via init()")
	require.NoError(t, b.Write(context.Background(), energy.MeasurementRecord{
		Namespace: "x", AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 1,
	}))
	require.NoError(t, b.Close())
}

// File-backed store persists across reopen — chargeback survives a process restart.
func TestFileBackedPersists(t *testing.T) {
	ctx := context.Background()
	path := t.TempDir() + "/energy.db"

	b1, err := sqlite.New(path)
	require.NoError(t, err)
	require.NoError(t, b1.Write(ctx, energy.MeasurementRecord{
		Namespace: "team-a", EnergyJoules: 100, OutputTokens: 10,
		AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 1000,
	}))
	require.NoError(t, b1.Close())

	b2, err := sqlite.New(path)
	require.NoError(t, err)
	defer b2.Close()
	rows, err := b2.QueryChargeback(ctx, energy.ChargebackQuery{
		Start: time.UnixMilli(0), End: time.UnixMilli(5000), PUE: 1.0,
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, 100.0, rows[0].EnergyJoules)
}

// Mirrors the memory backend's contract: in-range summation, out-of-range
// exclusion, proportional-never-hidden, PUE applied to kWh.
func TestChargebackAggregation(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)

	require.NoError(t, b.WriteBatch(ctx, []energy.MeasurementRecord{
		{Namespace: "a", EnergyJoules: 100, OutputTokens: 10, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 1000},
		{Namespace: "a", EnergyJoules: 50, OutputTokens: 5, AttributionMethod: energy.AttributionProportional, TimestampUnixMs: 2000},
		{Namespace: "b", EnergyJoules: 10, OutputTokens: 2, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 1500},
		{Namespace: "a", EnergyJoules: 999, OutputTokens: 999, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 99999}, // out of range
	}))

	rows, err := b.QueryChargeback(ctx, energy.ChargebackQuery{
		Start: time.UnixMilli(0), End: time.UnixMilli(5000), PUE: 2.0,
	})
	require.NoError(t, err)
	require.Len(t, rows, 2, "out-of-range record must be excluded")

	byNS := map[string]energy.NamespaceCharge{}
	for _, r := range rows {
		byNS[r.Namespace] = r
	}

	a := byNS["a"]
	require.Equal(t, 150.0, a.EnergyJoules, "in-range windows summed")
	require.Equal(t, uint64(15), a.OutputTokens)
	require.Equal(t, energy.AttributionProportional, a.AttributionMethod, "proportional never hidden in rollup")
	require.InEpsilon(t, 150.0/3_600_000*2.0, a.EnergyKWhWithPUE, 1e-12)

	require.Equal(t, energy.AttributionDirect, byNS["b"].AttributionMethod)
}

// Empty batch is a no-op, and an empty range yields no rows (never a nil-deref).
func TestEmptyCases(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)
	require.NoError(t, b.WriteBatch(ctx, nil))
	rows, err := b.QueryChargeback(ctx, energy.ChargebackQuery{Start: time.UnixMilli(0), End: time.UnixMilli(1)})
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestRetentionPurge(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)
	require.NoError(t, b.WriteBatch(ctx, []energy.MeasurementRecord{
		{Namespace: "a", OutputTokens: 1, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 1000},
		{Namespace: "a", OutputTokens: 1, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 2000},
		{Namespace: "a", OutputTokens: 1, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 3000},
	}))
	n, err := b.RetentionPurge(ctx, time.UnixMilli(2500))
	require.NoError(t, err)
	require.Equal(t, int64(2), n, "records before the cutoff are purged")

	rows, err := b.QueryChargeback(ctx, energy.ChargebackQuery{Start: time.UnixMilli(0), End: time.UnixMilli(10000)})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, uint64(1), rows[0].OutputTokens, "only the post-cutoff record remains")
}

// AC-11: a 30-day chargeback query over ~52k windows must return in well under
// 10s. With the timestamp index this is a bounded scan; we assert both
// correctness of the Σenergy/Σtokens rollup and the latency budget.
func TestChargebackQuery30Day(t *testing.T) {
	ctx := context.Background()
	b := newBackend(t)

	// 30 days × 24h × 60min ≈ 43,200 one-minute windows per namespace. Two
	// namespaces over 18 days of 1-min windows gives ~51,840 rows in range.
	const windows = 25920 // per namespace
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	const batchSize = 2000
	batch := make([]energy.MeasurementRecord, 0, batchSize)
	flush := func() {
		if len(batch) == 0 {
			return
		}
		require.NoError(t, b.WriteBatch(ctx, batch))
		batch = batch[:0]
	}
	for _, ns := range []string{"team-a", "team-b"} {
		for i := 0; i < windows; i++ {
			batch = append(batch, energy.MeasurementRecord{
				Namespace:         ns,
				EnergyJoules:      10,
				OutputTokens:      100,
				AttributionMethod: energy.AttributionDirect,
				TimestampUnixMs:   start.Add(time.Duration(i) * time.Minute).UnixMilli(),
			})
			if len(batch) == batchSize {
				flush()
			}
		}
	}
	flush()

	q := energy.ChargebackQuery{Start: start, End: start.Add(31 * 24 * time.Hour), PUE: 1.35}
	t0 := time.Now()
	rows, err := b.QueryChargeback(ctx, q)
	elapsed := time.Since(t0)
	require.NoError(t, err)

	require.Len(t, rows, 2)
	for _, r := range rows {
		require.Equal(t, 10.0*windows, r.EnergyJoules, "Σenergy over all windows")
		require.Equal(t, uint64(100*windows), r.OutputTokens, "Σtokens over all windows")
	}
	require.Less(t, elapsed, 10*time.Second, "AC-11: 30-day chargeback query under 10s (took %s)", elapsed)
	t.Logf("AC-11: queried %d rows in %s", 2*windows, elapsed)
}

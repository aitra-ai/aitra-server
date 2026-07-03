package memory_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/aitra-ai/aitra-server/energy"
	"github.com/aitra-ai/aitra-server/energy/storage/memory"
)

func TestMemoryChargebackAggregation(t *testing.T) {
	ctx := context.Background()
	b := memory.New()

	require.NoError(t, b.WriteBatch(ctx, []energy.MeasurementRecord{
		{Namespace: "a", EnergyJoules: 100, OutputTokens: 10, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 1000},
		{Namespace: "a", EnergyJoules: 50, OutputTokens: 5, AttributionMethod: energy.AttributionProportional, TimestampUnixMs: 2000},
		{Namespace: "b", EnergyJoules: 10, OutputTokens: 2, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 1500},
		{Namespace: "a", EnergyJoules: 999, OutputTokens: 999, AttributionMethod: energy.AttributionDirect, TimestampUnixMs: 99999}, // out of range
	}))

	rows, err := b.QueryChargeback(ctx, energy.ChargebackQuery{
		Start: time.UnixMilli(0),
		End:   time.UnixMilli(5000),
		PUE:   2.0,
	})
	require.NoError(t, err)

	byNS := map[string]energy.NamespaceCharge{}
	for _, r := range rows {
		byNS[r.Namespace] = r
	}

	require.Len(t, rows, 2, "out-of-range record must be excluded")

	a := byNS["a"]
	require.Equal(t, 150.0, a.EnergyJoules, "in-range windows summed")
	require.Equal(t, uint64(15), a.OutputTokens)
	require.Equal(t, energy.AttributionProportional, a.AttributionMethod, "proportional never hidden in rollup")
	require.InEpsilon(t, 150.0/3_600_000*2.0, a.EnergyKWhWithPUE, 1e-12)

	bRow := byNS["b"]
	require.Equal(t, energy.AttributionDirect, bRow.AttributionMethod)
}

func TestMemoryRegisteredAndWrite(t *testing.T) {
	b, err := energy.NewStorage("memory", nil)
	require.NoError(t, err, "memory backend registers itself via init()")
	require.NoError(t, b.Write(context.Background(), energy.MeasurementRecord{Namespace: "x"}))
	require.NoError(t, b.Close())
}

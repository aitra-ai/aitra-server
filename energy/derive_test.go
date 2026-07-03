package energy

import (
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

// AC-3: the cluster J/token must be Σenergy ÷ Σtokens, never the arithmetic mean
// of per-series J/token. The two records below have deliberately lopsided
// traffic so the correct value (~0.198) is far from the naive mean (5.05).
func TestClusterJPerTokenIsSumOfEnergyDividedBySumOfTokens(t *testing.T) {
	records := []MeasurementRecord{
		{EnergyJoules: 100, OutputTokens: 10, JPerToken: 10.0},  // low traffic, high J/token
		{EnergyJoules: 100, OutputTokens: 1000, JPerToken: 0.1}, // high traffic, low J/token
	}

	got := ClusterJPerToken(records)

	want := 200.0 / 1010.0 // Σenergy ÷ Σtokens
	require.InEpsilon(t, want, got, 1e-9)

	naiveMean := (10.0 + 0.1) / 2.0 // the WRONG way
	require.Greater(t, math.Abs(got-naiveMean), 1.0,
		"cluster J/token must not equal the per-series average")
}

// AC-4: every record carries a non-empty attribution method, and proportional
// is never silently dropped.
func TestAttributionMethodNeverEmpty(t *testing.T) {
	base := WindowInput{EnergyJoules: 50, OutputTokens: 100}

	t.Run("empty attribution is rejected", func(t *testing.T) {
		in := base
		in.AttributionMethod = ""
		_, err := ComputeRecord(in, 0, DefaultCVThreshold)
		require.ErrorIs(t, err, ErrEmptyAttribution)
	})

	for _, method := range []AttributionMethod{AttributionDirect, AttributionProportional} {
		t.Run(string(method)+" is preserved", func(t *testing.T) {
			in := base
			in.AttributionMethod = method
			rec, err := ComputeRecord(in, 0, DefaultCVThreshold)
			require.NoError(t, err)
			require.NotEmpty(t, rec.AttributionMethod)
			require.Equal(t, method, rec.AttributionMethod)
		})
	}
}

func TestComputeRecordGuards(t *testing.T) {
	t.Run("idle window (zero tokens)", func(t *testing.T) {
		in := WindowInput{AttributionMethod: AttributionDirect, EnergyJoules: 30, OutputTokens: 0}
		_, err := ComputeRecord(in, 0, DefaultCVThreshold)
		require.ErrorIs(t, err, ErrIdleWindow)
	})
	t.Run("negative energy", func(t *testing.T) {
		in := WindowInput{AttributionMethod: AttributionDirect, EnergyJoules: -1, OutputTokens: 10}
		_, err := ComputeRecord(in, 0, DefaultCVThreshold)
		require.ErrorIs(t, err, ErrNegativeEnergy)
	})
}

func TestComputeRecordDefaults(t *testing.T) {
	in := WindowInput{
		AttributionMethod: AttributionDirect,
		EnergyJoules:      200,
		OutputTokens:      100,
		// Workload and CalibrationTier intentionally left empty.
	}
	rec, err := ComputeRecord(in, 0.05, DefaultCVThreshold)
	require.NoError(t, err)
	require.Equal(t, "unknown", rec.Workload, "missing workload defaults to unknown, not error")
	require.Equal(t, CalibrationUncalibrated, rec.CalibrationTier, "empty tier is uncalibrated, never faked")
	require.InEpsilon(t, 2.0, rec.JPerToken, 1e-9)
	require.False(t, rec.Stable, "CV 0.05 > 0.03 threshold must flag unstable")
}

func TestDerivedMetrics(t *testing.T) {
	jpt := 2.0
	site := SiteParams{PUE: 1.5, GridGCO2PerKWh: 400, CostPerKWh: 0.12}

	require.InEpsilon(t, 2.0*1.5/3_600_000*400, CO2PerTokenGrams(jpt, site), 1e-12)
	require.InEpsilon(t, 0.1, CostPerMillionTokensUSD(jpt, site), 1e-9) // = 0.1 USD / M tokens
	require.InEpsilon(t, 0.5, TokensPerJoule(jpt), 1e-12)
	require.Equal(t, 0.0, TokensPerJoule(0), "guard divide-by-zero")
}

func TestCVTracker(t *testing.T) {
	t.Run("flat series is stable", func(t *testing.T) {
		c := NewCVTracker(100)
		for i := 0; i < 100; i++ {
			c.Add(5.0)
		}
		require.Equal(t, 0.0, c.CV())
		require.True(t, IsStable(c.CV(), DefaultCVThreshold))
	})
	t.Run("spread series is flagged unstable", func(t *testing.T) {
		c := NewCVTracker(100)
		for _, v := range []float64{1, 2, 3, 4, 5} {
			c.Add(v)
		}
		require.Greater(t, c.CV(), DefaultCVThreshold)
		require.False(t, IsStable(c.CV(), DefaultCVThreshold))
	})
	t.Run("rolling buffer evicts oldest", func(t *testing.T) {
		c := NewCVTracker(3)
		for _, v := range []float64{1, 2, 3, 4, 5} {
			c.Add(v)
		}
		require.Equal(t, 3, c.Len(), "buffer never exceeds its size")
	})
	t.Run("fewer than two samples is treated as stable", func(t *testing.T) {
		c := NewCVTracker(100)
		c.Add(42)
		require.Equal(t, 0.0, c.CV())
	})
}

func TestErrorsAreDistinct(t *testing.T) {
	require.False(t, errors.Is(ErrIdleWindow, ErrEmptyAttribution))
}

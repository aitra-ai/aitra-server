package energy

import (
	"errors"
	"math"
)

// secondsPerKWhJoules is the joules in one kWh (3.6e6), used to convert window
// energy into kWh before applying price / carbon intensity.
const joulesPerKWh = 3_600_000.0

// DefaultCVThreshold is the stability gate: a window is stable when CV < this.
const DefaultCVThreshold = 0.03

var (
	// ErrIdleWindow marks a window with zero output tokens. It is not an error
	// in the data — the caller records idle power separately and excludes the
	// window from J/token rather than discarding it.
	ErrIdleWindow = errors.New("energy: idle window (zero output tokens)")
	// ErrEmptyAttribution enforces that every record carries an attribution method.
	ErrEmptyAttribution = errors.New("energy: attribution method must not be empty")
	// ErrNegativeEnergy guards the EnergyProvider contract that Δjoules is never negative.
	ErrNegativeEnergy = errors.New("energy: window energy must not be negative")
)

// JPerToken is the primary measured metric: Δjoules ÷ Δoutput_tokens. It returns
// 0 for a zero-token window; idle handling is the caller's responsibility.
func JPerToken(joules float64, tokens uint64) float64 {
	if tokens == 0 {
		return 0
	}
	return joules / float64(tokens)
}

// ClusterJPerToken is Σenergy ÷ Σtokens across records — NOT the arithmetic mean
// of per-series J/token. Averaging would weight high- and low-traffic series
// equally and produce a wrong cluster value. This invariant is guarded by
// TestClusterJPerTokenIsSumOfEnergyDividedBySumOfTokens.
func ClusterJPerToken(records []MeasurementRecord) float64 {
	var sumEnergy float64
	var sumTokens uint64
	for _, r := range records {
		sumEnergy += r.EnergyJoules
		sumTokens += r.OutputTokens
	}
	if sumTokens == 0 {
		return 0
	}
	return sumEnergy / float64(sumTokens)
}

// CO2PerTokenGrams = J/token × PUE ÷ 3,600,000 × grid_gCO2/kWh.
func CO2PerTokenGrams(jPerToken float64, s SiteParams) float64 {
	return jPerToken * s.PUE / joulesPerKWh * s.GridGCO2PerKWh
}

// CostPerMillionTokensUSD = J/token × PUE ÷ 3,600,000 × $/kWh × 1e6.
func CostPerMillionTokensUSD(jPerToken float64, s SiteParams) float64 {
	return jPerToken * s.PUE / joulesPerKWh * s.CostPerKWh * 1e6
}

// TokensPerJoule is the inverse efficiency view: 1 ÷ J/token.
func TokensPerJoule(jPerToken float64) float64 {
	if jPerToken == 0 {
		return 0
	}
	return 1 / jPerToken
}

// JoulesToKWh converts raw window joules to kWh (no PUE applied).
func JoulesToKWh(joules float64) float64 {
	return joules / joulesPerKWh
}

// IsStable reports whether a coefficient of variation passes the gate. An
// unstable window is flagged, never suppressed.
func IsStable(cv, threshold float64) bool {
	return cv < threshold
}

// CVTracker is a fixed-size rolling buffer that yields the coefficient of
// variation (σ/μ) over the most recent samples. Memory is ~8 bytes/sample, i.e.
// ~800 bytes for the default 100-window buffer per (model × node) combination.
type CVTracker struct {
	size int
	buf  []float64
	next int
	full bool
}

// NewCVTracker creates a tracker over the last size samples (defaults to 100).
func NewCVTracker(size int) *CVTracker {
	if size <= 0 {
		size = 100
	}
	return &CVTracker{size: size, buf: make([]float64, 0, size)}
}

// Add appends a sample, evicting the oldest once the buffer is full.
func (c *CVTracker) Add(v float64) {
	if len(c.buf) < c.size {
		c.buf = append(c.buf, v)
		return
	}
	c.buf[c.next] = v
	c.next = (c.next + 1) % c.size
	c.full = true
}

// Len returns the number of samples currently held.
func (c *CVTracker) Len() int { return len(c.buf) }

// CV returns the population coefficient of variation over the buffered samples.
// It returns 0 when there are fewer than two samples or the mean is zero, so a
// freshly started series is treated as stable until it has data to prove otherwise.
func (c *CVTracker) CV() float64 {
	n := len(c.buf)
	if n < 2 {
		return 0
	}
	var sum float64
	for _, v := range c.buf {
		sum += v
	}
	mean := sum / float64(n)
	if mean == 0 {
		return 0
	}
	var sq float64
	for _, v := range c.buf {
		d := v - mean
		sq += d * d
	}
	std := math.Sqrt(sq / float64(n))
	return std / mean
}

// WindowInput is the raw per-window data handed to ComputeRecord.
type WindowInput struct {
	TimestampUnixMs int64
	Cluster         string
	Node            string
	Namespace       string
	Workload        string
	Model           string
	Hardware        string
	Precision       string
	Team            string
	CostCentre      string

	EnergyJoules      float64
	OutputTokens      uint64
	AttributionMethod AttributionMethod
	CalibrationTier   CalibrationTier
	EnergyProvider    string
	InferenceProvider string
}

// ComputeRecord derives a MeasurementRecord from one window. It enforces the
// honesty invariants of the system:
//   - attribution method is mandatory (ErrEmptyAttribution),
//   - energy is never negative (ErrNegativeEnergy),
//   - a zero-token window is idle, not counted (ErrIdleWindow),
//   - an unset calibration tier defaults to uncalibrated, never faked.
//
// cv is the rolling coefficient of variation for this series at window end.
func ComputeRecord(in WindowInput, cv, cvThreshold float64) (MeasurementRecord, error) {
	if in.AttributionMethod == "" {
		return MeasurementRecord{}, ErrEmptyAttribution
	}
	if in.EnergyJoules < 0 {
		return MeasurementRecord{}, ErrNegativeEnergy
	}
	if in.OutputTokens == 0 {
		return MeasurementRecord{}, ErrIdleWindow
	}

	tier := in.CalibrationTier
	if tier == "" {
		tier = CalibrationUncalibrated
	}
	workload := in.Workload
	if workload == "" {
		workload = "unknown"
	}

	return MeasurementRecord{
		TimestampUnixMs:   in.TimestampUnixMs,
		Cluster:           in.Cluster,
		Node:              in.Node,
		Namespace:         in.Namespace,
		Workload:          workload,
		Model:             in.Model,
		Hardware:          in.Hardware,
		Precision:         in.Precision,
		Team:              in.Team,
		CostCentre:        in.CostCentre,
		EnergyJoules:      in.EnergyJoules,
		OutputTokens:      in.OutputTokens,
		JPerToken:         JPerToken(in.EnergyJoules, in.OutputTokens),
		CalibrationTier:   tier,
		AttributionMethod: in.AttributionMethod,
		CV:                cv,
		Stable:            IsStable(cv, cvThreshold),
		EnergyProvider:    in.EnergyProvider,
		InferenceProvider: in.InferenceProvider,
	}, nil
}

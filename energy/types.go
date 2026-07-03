// Package energy is aitra-meter's measurement core: the canonical contracts and
// derivation logic for GPU/NPU inference energy accounting. Everything here is
// pure Go with no hardware or infrastructure dependency, so the J/token semantics
// are implemented exactly once and shared by every deployment shape.
package energy

// CalibrationTier records how a J/token value is calibrated. In v0.8.0 the
// calibration table is empty, so every record is Uncalibrated — that is the
// correct, honest state and must not be faked for cosmetics.
type CalibrationTier string

const (
	CalibrationAitraBenchmark CalibrationTier = "aitra_benchmark"
	CalibrationReference      CalibrationTier = "reference"
	CalibrationSelfCalibrated CalibrationTier = "self_calibrated"
	CalibrationUncalibrated   CalibrationTier = "uncalibrated"
)

// AttributionMethod is written into every MeasurementRecord and is never empty.
// proportional is never hidden — it appears in every chargeback export row.
type AttributionMethod string

const (
	AttributionDirect       AttributionMethod = "direct"
	AttributionProportional AttributionMethod = "proportional"
)

// MeasurementRecord is the shared contract between the agent, aggregation,
// storage, dashboard and Prometheus labels. It is defined first; every other
// layer is built around it.
type MeasurementRecord struct {
	TimestampUnixMs int64  // window end time (ms)
	Cluster         string // from config
	Node            string // k8s node name
	Namespace       string // k8s namespace
	Workload        string // from pod annotation; "unknown" if missing
	Model           string // from inference server metric label
	Hardware        string // from node label (h100/910b/...)
	Precision       string // fp16/fp8/bf16/unknown

	Team       string // from pod annotation
	CostCentre string // from pod annotation

	EnergyJoules float64 // raw GPU/NPU energy this window (never negative)
	OutputTokens uint64  // delta output tokens this window
	JPerToken    float64 // EnergyJoules / OutputTokens

	CalibrationTier   CalibrationTier
	AttributionMethod AttributionMethod
	CV                float64 // rolling coefficient of variation at window end
	Stable            bool    // CV < threshold

	EnergyProvider    string // nvml / dcgm / zeus (recorded on every row)
	InferenceProvider string // vllm / generic-prometheus (recorded on every row)
}

// SiteParams carries the per-site factors used to derive carbon and cost from
// J/token. They come from SiteConfig / Helm values, never from a hardware read.
type SiteParams struct {
	PUE            float64 // power usage effectiveness multiplier
	GridGCO2PerKWh float64 // grid carbon intensity, gCO2/kWh
	CostPerKWh     float64 // electricity price, USD/kWh
	CarbonSource   string  // electricitymaps / watttime / manual (badge: live/fallback)
}

// Device is a single measurable accelerator enumerated by an EnergyProvider.
type Device struct {
	Index int
	UUID  string
	Name  string
}

package energy

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// EnergyProvider reads accelerator energy. Implementations register themselves
// via RegisterEnergy in init() and are selected by name from config. The
// aggregation core never imports a concrete implementation.
type EnergyProvider interface {
	// BeginWindow snapshots starting energy. windowID is unique per window.
	BeginWindow(ctx context.Context, windowID string) error
	// EndWindow returns Δjoules since BeginWindow. It never returns a negative value.
	EndWindow(ctx context.Context, windowID string) (float64, error)
	// IdlePower returns current power (W); called when RequestsRunning == 0.
	IdlePower(ctx context.Context) (float64, error)
	// Devices enumerates the measurable accelerators on this node.
	Devices(ctx context.Context) ([]Device, error)
	// Name is the unique identifier used in metric labels and logs.
	Name() string
}

// InferenceMetricsProvider reads token throughput from an inference server.
type InferenceMetricsProvider interface {
	// OutputTokens returns the cumulative output token counter; it must be
	// monotonically increasing (the agent computes the per-window delta).
	OutputTokens(ctx context.Context) (uint64, error)
	// RequestsRunning returns in-flight requests; 0 marks an idle window.
	RequestsRunning(ctx context.Context) (int, error)
	// ModelName returns the model currently being served.
	ModelName(ctx context.Context) (string, error)
	// Name is the unique identifier.
	Name() string
}

// ChargebackQuery selects a period and the PUE to apply when aggregating.
type ChargebackQuery struct {
	Start time.Time
	End   time.Time
	PUE   float64
}

// NamespaceCharge is one aggregated chargeback row. AttributionMethod is always
// populated so proportional attribution is visible in every export.
type NamespaceCharge struct {
	Namespace         string
	EnergyJoules      float64
	EnergyKWhWithPUE  float64
	OutputTokens      uint64
	AttributionMethod AttributionMethod
}

// StorageBackend persists MeasurementRecords and answers chargeback queries.
type StorageBackend interface {
	Write(ctx context.Context, record MeasurementRecord) error
	WriteBatch(ctx context.Context, records []MeasurementRecord) error
	QueryChargeback(ctx context.Context, query ChargebackQuery) ([]NamespaceCharge, error)
	Close() error
}

// Factory signatures: providers are constructed from a string->string config
// map so Helm values can select and configure them without code changes.
type (
	EnergyFactory    func(config map[string]string) (EnergyProvider, error)
	InferenceFactory func(config map[string]string) (InferenceMetricsProvider, error)
	StorageFactory   func(config map[string]string) (StorageBackend, error)
)

var (
	registryMu        sync.RWMutex
	energyRegistry    = map[string]EnergyFactory{}
	inferenceRegistry = map[string]InferenceFactory{}
	storageRegistry   = map[string]StorageFactory{}
)

// RegisterEnergy registers an EnergyProvider factory under name. Panics on a
// duplicate name so registration mistakes fail loudly at startup.
func RegisterEnergy(name string, f EnergyFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := energyRegistry[name]; dup {
		panic("energy: duplicate EnergyProvider registration: " + name)
	}
	energyRegistry[name] = f
}

// RegisterInference registers an InferenceMetricsProvider factory under name.
func RegisterInference(name string, f InferenceFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := inferenceRegistry[name]; dup {
		panic("energy: duplicate InferenceMetricsProvider registration: " + name)
	}
	inferenceRegistry[name] = f
}

// RegisterStorage registers a StorageBackend factory under name.
func RegisterStorage(name string, f StorageFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, dup := storageRegistry[name]; dup {
		panic("energy: duplicate StorageBackend registration: " + name)
	}
	storageRegistry[name] = f
}

// NewEnergy constructs the named EnergyProvider.
func NewEnergy(name string, config map[string]string) (EnergyProvider, error) {
	registryMu.RLock()
	f, ok := energyRegistry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("energy: unknown EnergyProvider %q (registered: %v)", name, registered(energyRegistry))
	}
	return f(config)
}

// NewInference constructs the named InferenceMetricsProvider.
func NewInference(name string, config map[string]string) (InferenceMetricsProvider, error) {
	registryMu.RLock()
	f, ok := inferenceRegistry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("energy: unknown InferenceMetricsProvider %q (registered: %v)", name, registered(inferenceRegistry))
	}
	return f(config)
}

// NewStorage constructs the named StorageBackend.
func NewStorage(name string, config map[string]string) (StorageBackend, error) {
	registryMu.RLock()
	f, ok := storageRegistry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("energy: unknown StorageBackend %q (registered: %v)", name, registered(storageRegistry))
	}
	return f(config)
}

// registered returns the sorted names in a registry, for error messages.
func registered[T any](m map[string]T) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	sort.Strings(names)
	return names
}

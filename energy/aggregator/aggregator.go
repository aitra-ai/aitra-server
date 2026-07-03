// Package aggregator orchestrates the energy measurement cycle. It runs entirely
// out-of-band from the inference request path: a background ticker reads
// already-recorded data (energy from Prometheus, tokens from the metering
// ledger), feeds the pure energy core, and publishes metrics. It never calls the
// AI Gateway, never sits in a request, and never holds a lock the request path
// needs — so it cannot affect inference latency. Decoupling is enforced
// structurally: this package depends only on the small interfaces below, none of
// which touch the data plane.
package aggregator

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/aitra-ai/aitra-server/energy"
)

// Scope identifies which accelerators a deployment's energy reading covers. For
// the Prometheus source it becomes a label selector; for fakes it is just a key.
type Scope struct {
	Namespace   string
	PodSelector string // e.g. a pod-name regex or label selector
	Node        string
}

// Deployment is the aggregator's minimal view of a running deployment, mapped
// from the platform's deploy record by a DeploymentLister. Keeping it minimal
// decouples the aggregator from the full DB model.
type Deployment struct {
	ID         int64
	Cluster    string
	Namespace  string
	Node       string
	Workload   string
	Model      string
	Hardware   string
	Precision  string
	Team       string
	CostCentre string
	Scope      Scope
	// MeteringKey is the account_metering customer_id (= deployment SvcName) used
	// to join energy with the already-recorded token usage.
	MeteringKey string
}

// DeploymentLister returns the running deployments to measure this cycle.
type DeploymentLister interface {
	ListRunning(ctx context.Context) ([]Deployment, error)
}

// EnergySource returns out-of-band energy readings for a scope. Implementations
// query Prometheus (real) or return scripted values (tests) — never hardware
// directly from this package.
type EnergySource interface {
	// WindowJoules returns Δjoules for scope over [start, end). Never negative.
	WindowJoules(ctx context.Context, scope Scope, start, end time.Time) (float64, error)
	// IdlePower returns current power draw (W) for scope, used on idle windows.
	IdlePower(ctx context.Context, scope Scope) (float64, error)
	// Name identifies the provider; recorded on every row.
	Name() string
}

// TokenSource returns the Δoutput-tokens for a deployment over a window, read
// from the already-persisted metering ledger (not from the live request path).
// meteringKey is the deployment's account_metering customer_id (SvcName).
type TokenSource interface {
	WindowTokens(ctx context.Context, meteringKey string, start, end time.Time) (uint64, error)
	Name() string
}

// MetricsSink receives derived results. Implementations publish to Prometheus.
type MetricsSink interface {
	Observe(rec energy.MeasurementRecord)
	ObserveIdle(node string, watts float64)
}

// Config tunes the measurement cadence and stability gate.
type Config struct {
	Window       time.Duration // measurement window; default 60s
	CycleTimeout time.Duration // per-cycle hard timeout; default 2m
	CVWindowSize int           // rolling CV buffer size; default 100
	CVThreshold  float64       // stability gate; default energy.DefaultCVThreshold
}

func (c Config) withDefaults() Config {
	if c.Window <= 0 {
		c.Window = 60 * time.Second
	}
	if c.CycleTimeout <= 0 {
		c.CycleTimeout = 2 * time.Minute
	}
	if c.CVWindowSize <= 0 {
		c.CVWindowSize = 100
	}
	if c.CVThreshold <= 0 {
		c.CVThreshold = energy.DefaultCVThreshold
	}
	return c
}

// Aggregator ties the sources to the energy core. It is safe to Start once; each
// cycle runs sequentially so the per-series CV trackers need no per-tracker lock.
type Aggregator struct {
	cfg     Config
	deploys DeploymentLister
	energy  EnergySource
	tokens  TokenSource
	sink    MetricsSink
	storage energy.StorageBackend // optional; nil => metrics only, no persistence

	mu  sync.Mutex
	cvt map[string]*energy.CVTracker // key: model + \x00 + node
}

// Option configures an Aggregator at construction.
type Option func(*Aggregator)

// WithStorage attaches a StorageBackend. Each cycle's records are batch-persisted
// to it so the chargeback/audit query path has a data source. Without it the
// aggregator only publishes metrics (the default), so persistence is opt-in.
func WithStorage(s energy.StorageBackend) Option {
	return func(a *Aggregator) { a.storage = s }
}

// New builds an Aggregator, applying config defaults and any options.
func New(cfg Config, d DeploymentLister, e EnergySource, t TokenSource, s MetricsSink, opts ...Option) *Aggregator {
	a := &Aggregator{
		cfg:     cfg.withDefaults(),
		deploys: d,
		energy:  e,
		tokens:  t,
		sink:    s,
		cvt:     map[string]*energy.CVTracker{},
	}
	for _, opt := range opts {
		opt(a)
	}
	return a
}

// Start launches the background loop. It returns immediately; the loop stops when
// ctx is cancelled. Inference is never on this goroutine's path.
func (a *Aggregator) Start(ctx context.Context) {
	go func() {
		a.runCycleGuarded(ctx) // run once on startup, like the GPU billing job
		ticker := time.NewTicker(a.cfg.Window)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				a.runCycleGuarded(ctx)
			}
		}
	}()
}

// runCycleGuarded bounds a cycle with a timeout and swallows errors so a slow or
// failing cycle only delays metrics, never inference.
func (a *Aggregator) runCycleGuarded(ctx context.Context) {
	cctx, cancel := context.WithTimeout(ctx, a.cfg.CycleTimeout)
	defer cancel()
	if err := a.RunCycle(cctx, time.Now()); err != nil {
		slog.Error("energy aggregator: cycle failed", "error", err)
	}
}

// RunCycle measures every running deployment for the window ending at now. It is
// deterministic given its inputs (now is injected) so it is unit-testable
// without a clock. A failure on one deployment is isolated and skipped.
func (a *Aggregator) RunCycle(ctx context.Context, now time.Time) error {
	start := now.Add(-a.cfg.Window)
	deps, err := a.deploys.ListRunning(ctx)
	if err != nil {
		return fmt.Errorf("list running deployments: %w", err)
	}
	var batch []energy.MeasurementRecord
	for _, d := range deps {
		if err := ctx.Err(); err != nil {
			return err // respect cycle timeout / cancellation promptly
		}
		if rec, ok := a.measureOne(ctx, d, start, now); ok {
			batch = append(batch, rec)
		}
	}
	a.persist(ctx, batch)
	return nil
}

// persist batch-writes a cycle's records to storage, if configured. A storage
// failure is logged and swallowed — persistence must never stall the loop or
// affect inference, only delay the chargeback/audit data.
func (a *Aggregator) persist(ctx context.Context, batch []energy.MeasurementRecord) {
	if a.storage == nil || len(batch) == 0 {
		return
	}
	if err := a.storage.WriteBatch(ctx, batch); err != nil {
		slog.Error("energy aggregator: persist batch failed", "count", len(batch), "error", err)
	}
}

// measureOne measures a single deployment. It returns the derived record and
// true when a J/token measurement was produced; an idle/errored/skipped
// deployment returns false. Errors are logged and the deployment is skipped —
// never propagated in a way that could stall the loop or inference.
func (a *Aggregator) measureOne(ctx context.Context, d Deployment, start, now time.Time) (energy.MeasurementRecord, bool) {
	joules, err := a.energy.WindowJoules(ctx, d.Scope, start, now)
	if err != nil {
		slog.Warn("energy aggregator: read energy failed, skipping deployment",
			"deploy", d.ID, "namespace", d.Namespace, "error", err)
		return energy.MeasurementRecord{}, false
	}
	tokens, err := a.tokens.WindowTokens(ctx, d.MeteringKey, start, now)
	if err != nil {
		slog.Warn("energy aggregator: read tokens failed, skipping deployment",
			"deploy", d.ID, "namespace", d.Namespace, "error", err)
		return energy.MeasurementRecord{}, false
	}

	// Zero-token window is idle: not counted in J/token, idle power recorded separately.
	if tokens == 0 {
		if watts, err := a.energy.IdlePower(ctx, d.Scope); err == nil {
			a.sink.ObserveIdle(d.Node, watts)
		}
		return energy.MeasurementRecord{}, false
	}

	jpt := energy.JPerToken(joules, tokens)
	tracker := a.trackerFor(d.Model, d.Node)
	tracker.Add(jpt)

	rec, err := energy.ComputeRecord(energy.WindowInput{
		TimestampUnixMs: now.UnixMilli(),
		Cluster:         d.Cluster,
		Node:            d.Node,
		Namespace:       d.Namespace,
		Workload:        d.Workload,
		Model:           d.Model,
		Hardware:        d.Hardware,
		Precision:       d.Precision,
		Team:            d.Team,
		CostCentre:      d.CostCentre,
		EnergyJoules:    joules,
		OutputTokens:    tokens,
		// proportional split for shared servers is a later step; direct keeps
		// attribution non-empty today (TestAttributionMethodNeverEmpty).
		AttributionMethod: energy.AttributionDirect,
		EnergyProvider:    a.energy.Name(),
		InferenceProvider: a.tokens.Name(),
	}, tracker.CV(), a.cfg.CVThreshold)
	if err != nil {
		slog.Warn("energy aggregator: compute record failed, skipping",
			"deploy", d.ID, "error", err)
		return energy.MeasurementRecord{}, false
	}
	a.sink.Observe(rec)
	return rec, true
}

// trackerFor returns the rolling CV tracker for a model×node series, creating it
// on first use.
func (a *Aggregator) trackerFor(model, node string) *energy.CVTracker {
	a.mu.Lock()
	defer a.mu.Unlock()
	key := model + "\x00" + node
	t, ok := a.cvt[key]
	if !ok {
		t = energy.NewCVTracker(a.cfg.CVWindowSize)
		a.cvt[key] = t
	}
	return t
}

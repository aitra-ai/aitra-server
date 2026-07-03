// Package service is the composition root for the energy aggregator: it wires the
// real adapters (Prometheus energy, metering tokens, deploy lister, Prometheus
// metrics) and starts the background loop in the host process. It takes resolved
// primitives rather than the full config/common packages, so it stays out of
// those heavy imports.
package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/energy"
	"github.com/aitra-ai/aitra-server/energy/aggregator"
	"github.com/aitra-ai/aitra-server/energy/deploylister"
	"github.com/aitra-ai/aitra-server/energy/metering"
	"github.com/aitra-ai/aitra-server/energy/metrics"
	"github.com/aitra-ai/aitra-server/energy/prom"
	_ "github.com/aitra-ai/aitra-server/energy/storage/memory" // registers "memory" backend
	_ "github.com/aitra-ai/aitra-server/energy/storage/sqlite" // registers "sqlite" backend
)

// Options carries everything the wiring needs, resolved by the caller.
type Options struct {
	PrometheusAddr      string  // Prometheus query API; empty = feature disabled
	PrometheusBasicAuth string  // optional "base64(user:pass)"
	RunningStatus       int     // deploy "running" status code
	InferenceTypes      []int64 // deploy type codes counted (inference, serverless)
	Site                energy.SiteParams
	Window              time.Duration // measurement window; 0 = aggregator default (60s)

	// StorageBackend selects the persistence backend by registered name
	// (sqlite|memory). Empty or "none" keeps the aggregator metrics-only.
	StorageBackend string
	// StoragePath is the backend's location (e.g. the SQLite file path). Empty
	// with the sqlite backend yields an in-memory database.
	StoragePath string
}

// Start wires the real adapters and launches the background aggregator in this
// process. It is a no-op (logs and returns) when no Prometheus address is set,
// so the feature is off by default. The aggregator reads Prometheus and the
// metering ledger out-of-band; it never touches the inference request path.
func Start(ctx context.Context, db *database.DB, opts Options) {
	if opts.PrometheusAddr == "" {
		slog.Info("energy aggregator: disabled (no prometheus address configured)")
		return
	}

	energySrc := prom.NewSource(prom.NewClient(opts.PrometheusAddr, opts.PrometheusBasicAuth), prom.Config{})
	tokenSrc := metering.NewSourceWithDB(db)
	lister := deploylister.NewListerWithDB(db, opts.RunningStatus, opts.InferenceTypes)
	// nil registerer => Prometheus default registry, which the API server's
	// /metrics endpoint scrapes.
	sink := metrics.NewPrometheusSink(nil, opts.Site)

	var aggOpts []aggregator.Option
	if store := openStorage(ctx, opts); store != nil {
		aggOpts = append(aggOpts, aggregator.WithStorage(store))
	}

	agg := aggregator.New(aggregator.Config{Window: opts.Window}, lister, energySrc, tokenSrc, sink, aggOpts...)
	agg.Start(ctx)
	slog.Info("energy aggregator: started",
		"prometheus", opts.PrometheusAddr, "window", opts.Window, "storage", opts.StorageBackend)
}

// openStorage constructs the configured StorageBackend, or returns nil to run
// metrics-only. A construction failure is logged and degrades to metrics-only
// rather than blocking the (already opt-in) feature. When a backend is opened it
// is closed on ctx cancellation so the database is flushed on shutdown.
func openStorage(ctx context.Context, opts Options) energy.StorageBackend {
	name := opts.StorageBackend
	if name == "" || name == "none" {
		return nil
	}
	store, err := energy.NewStorage(name, map[string]string{"path": opts.StoragePath})
	if err != nil {
		slog.Error("energy aggregator: storage init failed, running metrics-only",
			"backend", name, "error", err)
		return nil
	}
	go func() {
		<-ctx.Done()
		if err := store.Close(); err != nil {
			slog.Warn("energy aggregator: storage close failed", "backend", name, "error", err)
		}
	}()
	return store
}

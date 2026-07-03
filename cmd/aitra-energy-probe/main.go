// Command aitra-energy-probe runs the aitra-meter energy aggregator standalone,
// against real hardware, without the rest of the platform. It wires:
//
//   - energy  : Prometheus / DCGM-exporter (real GPU energy)         [prom.Source]
//   - tokens  : Prometheus / vLLM generation_tokens_total            [prom.TokenSource]
//   - deploys : a single static deployment supplied by flags         [staticLister]
//   - sink    : the aitra_* Prometheus metrics, served on --listen   [metrics.PrometheusSink]
//   - storage : SQLite, so chargeback/audit has a real data source   [storage/sqlite]
//
// It prints J/token every window and exposes /metrics, so the measurement can be
// watched and debugged live on a GPU box. It is the no-platform sibling of
// energy/service: same aggregator, same honesty invariants, different wiring.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/energy"
	"opencsg.com/csghub-server/energy/aggregator"
	"opencsg.com/csghub-server/energy/metering"
	"opencsg.com/csghub-server/energy/metrics"
	"opencsg.com/csghub-server/energy/prom"
	"opencsg.com/csghub-server/energy/storage/sqlite"
)

func main() {
	var (
		promAddr   = flag.String("prometheus", "http://localhost:9090", "Prometheus query API base URL")
		gpus       = flag.String("gpus", "", "GPU index regex for energy scope, e.g. \"0|1\" (empty = all GPUs)")
		gpuLabel   = flag.String("gpu-label", "gpu", "DCGM label to scope energy by (gpu|pod|UUID)")
		model       = flag.String("model", "", "vLLM model_name to scope tokens by (empty = all series)")
		tokMetric   = flag.String("token-metric", "vllm:generation_tokens_total", "Prometheus counter for output tokens")
		tokenSrcKind = flag.String("token-source", "vllm", "token source: vllm (Prometheus) | metering (account_metering DB)")
		dbDSN       = flag.String("db-dsn", "", "Postgres DSN for the metering token source")
		meteringKey = flag.String("metering-key", "", "account_metering customer_id to sum tokens for; defaults to --model")
		node       = flag.String("node", "gpu-node", "node label for CV/idle series")
		hardware   = flag.String("hardware", "h100", "hardware tier label")
		namespace  = flag.String("namespace", "probe", "namespace label for the measured series")
		window     = flag.Duration("window", 30*time.Second, "measurement window")
		listen     = flag.String("listen", ":9402", "address to serve aitra_* /metrics on")
		dbPath     = flag.String("sqlite", "", "SQLite file for persistence (empty = in-memory)")
		pue        = flag.Float64("pue", 1.0, "PUE multiplier for carbon/cost derivation")
		gridGCO2   = flag.Float64("grid-gco2-kwh", 0, "grid carbon intensity gCO2/kWh (0 = unknown)")
		costPerKWh = flag.Float64("cost-kwh", 0, "electricity price USD/kWh (0 = unknown)")
		deployments = flag.String("deployments", "", `JSON array of {"model","gpus","meteringKey","hardware","node","namespace"} for multi-model measurement; overrides the single --model/--gpus flags`)
	)
	flag.Parse()

	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := prom.NewClient(*promAddr, "")
	energySrc := prom.NewSource(client, prom.Config{PodLabel: *gpuLabel})

	key := *meteringKey
	if key == "" {
		key = *model
	}

	// Token source: vLLM's Prometheus counter (no-platform path) or the platform's
	// account_metering ledger (full-platform path: tokens flow through the aigateway).
	var tokenSrc aggregator.TokenSource
	switch *tokenSrcKind {
	case "metering":
		db, err := database.NewDB(ctx, database.DBConfig{Dialect: database.DialectPostgres, DSN: *dbDSN})
		if err != nil {
			slog.Error("probe: open metering DB", "error", err)
			os.Exit(1)
		}
		tokenSrc = metering.NewSourceWithDB(db)
		slog.Info("probe: token source = account_metering", "metering_key", key)
	default:
		tokenSrc = prom.NewTokenSource(client, prom.TokenConfig{TokensMetric: *tokMetric}, "vllm")
		slog.Info("probe: token source = vllm prometheus", "metric", *tokMetric)
	}

	deps := buildDeployments(*deployments, *namespace, *node, *hardware, *model, *gpus, key)
	lister := &staticLister{deps: deps}
	for _, d := range deps {
		slog.Info("probe: measuring deployment", "model", d.Model, "hardware", d.Hardware,
			"gpus", d.Scope.PodSelector, "metering_key", d.MeteringKey)
	}

	site := energy.SiteParams{PUE: *pue, GridGCO2PerKWh: *gridGCO2, CostPerKWh: *costPerKWh, CarbonSource: "manual"}
	reg := prometheus.NewRegistry()

	store, err := sqlite.New(*dbPath)
	if err != nil {
		slog.Error("probe: open sqlite", "error", err)
		os.Exit(1)
	}
	defer store.Close()

	sink := &apiSink{
		inner:       metrics.NewPrometheusSink(reg, site),
		site:        site,
		store:       store,
		tokenSource: *tokenSrcKind,
	}

	agg := aggregator.New(aggregator.Config{Window: *window}, lister, energySrc, tokenSrc, sink,
		aggregator.WithStorage(store))
	agg.Start(ctx)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	mux.HandleFunc("/api/v1/energy/summary", sink.handleSummary)
	mux.HandleFunc("/api/v1/energy/table", sink.handleTable)
	mux.HandleFunc("/api/v1/energy/series", sink.handleSeries)
	mux.HandleFunc("/api/v1/energy/chargeback", sink.handleChargeback)
	srv := &http.Server{Addr: *listen, Handler: mux}
	go func() {
		slog.Info("probe: serving aitra_* metrics", "listen", *listen, "prometheus", *promAddr,
			"gpus", *gpus, "model", *model, "window", *window)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("probe: metrics server", "error", err)
		}
	}()

	<-ctx.Done()
	slog.Info("probe: shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

// staticLister yields a fixed set of deployments — the GPU models under test.
type staticLister struct{ deps []aggregator.Deployment }

func (l *staticLister) ListRunning(context.Context) ([]aggregator.Deployment, error) {
	return l.deps, nil
}

// depSpec is one deployment in the --deployments JSON.
type depSpec struct {
	Model       string `json:"model"`
	GPUs        string `json:"gpus"`
	MeteringKey string `json:"meteringKey"`
	Hardware    string `json:"hardware"`
	Node        string `json:"node"`
	Namespace   string `json:"namespace"`
}

// buildDeployments parses the --deployments JSON into aggregator deployments,
// falling back to a single deployment from the scalar flags when it is empty.
// Per-spec fields default to the scalar-flag values when omitted.
func buildDeployments(jsonStr, defNS, defNode, defHW, defModel, defGPUs, defKey string) []aggregator.Deployment {
	if jsonStr == "" {
		return []aggregator.Deployment{{
			Namespace: defNS, Node: defNode, Model: defModel, Hardware: defHW,
			MeteringKey: defKey, Scope: aggregator.Scope{PodSelector: defGPUs},
		}}
	}
	var specs []depSpec
	if err := json.Unmarshal([]byte(jsonStr), &specs); err != nil {
		slog.Error("probe: parse --deployments", "error", err)
		os.Exit(1)
	}
	deps := make([]aggregator.Deployment, 0, len(specs))
	for _, s := range specs {
		ns, nd, hw := s.Namespace, s.Node, s.Hardware
		if ns == "" {
			ns = defNS
		}
		if nd == "" {
			nd = defNode
		}
		if hw == "" {
			hw = defHW
		}
		mk := s.MeteringKey
		if mk == "" {
			mk = s.Model
		}
		deps = append(deps, aggregator.Deployment{
			Namespace: ns, Node: nd, Model: s.Model, Hardware: hw,
			MeteringKey: mk, Scope: aggregator.Scope{PodSelector: s.GPUs},
		})
	}
	return deps
}

// apiSink logs each measurement, retains a recent-window ring for the JSON API,
// and forwards to the real Prometheus sink.
type apiSink struct {
	inner       aggregator.MetricsSink
	site        energy.SiteParams
	store       *sqlite.Backend
	tokenSource string

	mu     sync.Mutex
	recent []energy.MeasurementRecord
}

const recentCap = 240 // ~2h of 30s windows

func (l *apiSink) Observe(r energy.MeasurementRecord) {
	slog.Info("measurement",
		"model", r.Model, "j_per_token", r.JPerToken, "energy_j", r.EnergyJoules,
		"tokens", r.OutputTokens, "cv", r.CV, "stable", r.Stable,
		"tokens_per_joule", energy.TokensPerJoule(r.JPerToken))

	l.mu.Lock()
	l.recent = append(l.recent, r)
	if len(l.recent) > recentCap {
		l.recent = l.recent[len(l.recent)-recentCap:]
	}
	l.mu.Unlock()

	l.inner.Observe(r)
}

func (l *apiSink) ObserveIdle(node string, watts float64) {
	slog.Info("idle window", "node", node, "watts", watts)
	l.inner.ObserveIdle(node, watts)
}

func (l *apiSink) snapshot() []energy.MeasurementRecord {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]energy.MeasurementRecord, len(l.recent))
	copy(out, l.recent)
	return out
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	_ = json.NewEncoder(w).Encode(v)
}

// handleSummary returns the latest measurement plus derived carbon/cost.
func (l *apiSink) handleSummary(w http.ResponseWriter, _ *http.Request) {
	recent := l.snapshot()
	if len(recent) == 0 {
		writeJSON(w, map[string]any{"available": false, "token_source": l.tokenSource})
		return
	}
	r := recent[len(recent)-1]
	writeJSON(w, map[string]any{
		"available":                   true,
		"token_source":                l.tokenSource,
		"model":                       r.Model,
		"hardware":                    r.Hardware,
		"node":                        r.Node,
		"namespace":                   r.Namespace,
		"j_per_token":                 r.JPerToken,
		"tokens_per_joule":            energy.TokensPerJoule(r.JPerToken),
		"energy_joules":               r.EnergyJoules,
		"output_tokens":               r.OutputTokens,
		"cv":                          r.CV,
		"stable":                      r.Stable,
		"co2_per_token_grams":         energy.CO2PerTokenGrams(r.JPerToken, l.site),
		"cost_per_million_tokens_usd": energy.CostPerMillionTokensUSD(r.JPerToken, l.site),
		"calibration_tier":            string(r.CalibrationTier),
		"attribution_method":          string(r.AttributionMethod),
		"energy_provider":             r.EnergyProvider,
		"inference_provider":          r.InferenceProvider,
		"pue":                         l.site.PUE,
		"carbon_source":               l.site.CarbonSource,
		"timestamp_unix_ms":           r.TimestampUnixMs,
	})
}

// handleTable returns the latest measurement per (namespace, workload, model,
// hardware) series, plus the cluster Σenergy÷Σtokens — the multi-model view.
func (l *apiSink) handleTable(w http.ResponseWriter, _ *http.Request) {
	recent := l.snapshot()
	type seriesKey struct{ ns, wl, model, hw string }
	latest := map[seriesKey]energy.MeasurementRecord{}
	order := make([]seriesKey, 0)
	for _, r := range recent {
		k := seriesKey{r.Namespace, r.Workload, r.Model, r.Hardware}
		if _, ok := latest[k]; !ok {
			order = append(order, k)
		}
		latest[k] = r // recent is time-ordered; last write per key wins
	}
	type row struct {
		Model           string  `json:"model"`
		Hardware        string  `json:"hardware"`
		Namespace       string  `json:"namespace"`
		Workload        string  `json:"workload"`
		JPerToken       float64 `json:"j_per_token"`
		TokensPerJoule  float64 `json:"tokens_per_joule"`
		EnergyJoules    float64 `json:"energy_joules"`
		OutputTokens    uint64  `json:"output_tokens"`
		CV              float64 `json:"cv"`
		Stable          bool    `json:"stable"`
		CO2PerToken     float64 `json:"co2_per_token_grams"`
		CostPerMillion  float64 `json:"cost_per_million_tokens_usd"`
		CalibrationTier string  `json:"calibration_tier"`
		Attribution     string  `json:"attribution_method"`
		TimestampUnixMs int64   `json:"timestamp_unix_ms"`
	}
	rows := make([]row, 0, len(order))
	latestRecs := make([]energy.MeasurementRecord, 0, len(order))
	for _, k := range order {
		r := latest[k]
		latestRecs = append(latestRecs, r)
		rows = append(rows, row{
			Model: r.Model, Hardware: r.Hardware, Namespace: r.Namespace, Workload: r.Workload,
			JPerToken: r.JPerToken, TokensPerJoule: energy.TokensPerJoule(r.JPerToken),
			EnergyJoules: r.EnergyJoules, OutputTokens: r.OutputTokens, CV: r.CV, Stable: r.Stable,
			CO2PerToken: energy.CO2PerTokenGrams(r.JPerToken, l.site),
			CostPerMillion:  energy.CostPerMillionTokensUSD(r.JPerToken, l.site),
			CalibrationTier: string(r.CalibrationTier), Attribution: string(r.AttributionMethod),
			TimestampUnixMs: r.TimestampUnixMs,
		})
	}
	clusterJPT := energy.ClusterJPerToken(latestRecs) // Σenergy ÷ Σtokens, not the mean
	writeJSON(w, map[string]any{
		"rows":          rows,
		"token_source":  l.tokenSource,
		"pue":           l.site.PUE,
		"carbon_source": l.site.CarbonSource,
		"cluster": map[string]any{
			"j_per_token":                 clusterJPT,
			"tokens_per_joule":            energy.TokensPerJoule(clusterJPT),
			"co2_per_token_grams":         energy.CO2PerTokenGrams(clusterJPT, l.site),
			"cost_per_million_tokens_usd": energy.CostPerMillionTokensUSD(clusterJPT, l.site),
			"models":                      len(rows),
		},
	})
}

// handleSeries returns recent windows (with model) for per-model trend lines.
func (l *apiSink) handleSeries(w http.ResponseWriter, _ *http.Request) {
	recent := l.snapshot()
	type pt struct {
		T      int64   `json:"t"`
		JPT    float64 `json:"jpt"`
		CV     float64 `json:"cv"`
		Tokens uint64  `json:"tokens"`
		Stable bool    `json:"stable"`
		Model  string  `json:"model"`
	}
	pts := make([]pt, 0, len(recent))
	for _, r := range recent {
		pts = append(pts, pt{T: r.TimestampUnixMs, JPT: r.JPerToken, CV: r.CV, Tokens: r.OutputTokens, Stable: r.Stable, Model: r.Model})
	}
	writeJSON(w, map[string]any{"points": pts})
}

// handleChargeback returns the namespace rollup from SQLite for the last 7 days.
func (l *apiSink) handleChargeback(w http.ResponseWriter, req *http.Request) {
	end := time.Now()
	start := end.Add(-7 * 24 * time.Hour)
	rows, err := l.store.QueryChargeback(req.Context(), energy.ChargebackQuery{Start: start, End: end, PUE: l.site.PUE})
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		writeJSON(w, map[string]any{"error": err.Error()})
		return
	}
	type row struct {
		Namespace         string  `json:"namespace"`
		EnergyJoules      float64 `json:"energy_joules"`
		EnergyKWhWithPUE  float64 `json:"energy_kwh_with_pue"`
		OutputTokens      uint64  `json:"output_tokens"`
		AttributionMethod string  `json:"attribution_method"`
		CostUSD           float64 `json:"cost_usd"`
	}
	out := make([]row, 0, len(rows))
	for _, c := range rows {
		out = append(out, row{
			Namespace:         c.Namespace,
			EnergyJoules:      c.EnergyJoules,
			EnergyKWhWithPUE:  c.EnergyKWhWithPUE,
			OutputTokens:      c.OutputTokens,
			AttributionMethod: string(c.AttributionMethod),
			CostUSD:           c.EnergyKWhWithPUE * l.site.CostPerKWh,
		})
	}
	writeJSON(w, map[string]any{"rows": out, "period_days": 7})
}

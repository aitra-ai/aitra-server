package healthcheck

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/common/config"
)

const (
	checkInterval     = 5 * time.Minute
	requestTimeout    = 10 * time.Second
	maxFailures       = 3
	healthStatusOK    = "online"
	healthStatusDown  = "offline"
)

// HealthChecker periodically pings enabled models and updates their status.
type HealthChecker struct {
	config    *config.Config
	llmStore  database.LLMConfigStore
	logStore  database.ModelHealthLogStore
	client    *http.Client
	mu        sync.Mutex
	failures  map[int64]int // model ID → consecutive failure count
}

// NewHealthChecker creates and starts the health checker goroutine.
func NewHealthChecker(cfg *config.Config) *HealthChecker {
	hc := &HealthChecker{
		config:   cfg,
		llmStore: database.NewLLMConfigStore(cfg),
		logStore: database.NewModelHealthLogStore(),
		client:   &http.Client{Timeout: requestTimeout},
		failures: make(map[int64]int),
	}
	go hc.run()
	return hc
}

func (hc *HealthChecker) run() {
	// Initial check after 30 seconds
	time.Sleep(30 * time.Second)
	hc.checkAll()

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()
	for range ticker.C {
		hc.checkAll()
	}
}

func (hc *HealthChecker) checkAll() {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	configs, _, err := hc.llmStore.Index(ctx, 200, 1, nil)
	if err != nil {
		slog.Error("healthcheck: failed to list models", "error", err)
		return
	}

	slog.Info("healthcheck: starting check", "model_count", len(configs))

	var wg sync.WaitGroup
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		wg.Add(1)
		go func(c database.LLMConfig) {
			defer wg.Done()
			hc.checkModel(ctx, c)
		}(*cfg)
	}
	wg.Wait()
}

func (hc *HealthChecker) checkModel(ctx context.Context, cfg database.LLMConfig) {
	start := time.Now()
	status := healthStatusOK
	var errMsg string

	// Simple health check: try to reach the endpoint
	endpoint := cfg.ApiEndpoint
	if endpoint == "" {
		return
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodHead, endpoint, nil)
	if err != nil {
		status = healthStatusDown
		errMsg = fmt.Sprintf("invalid endpoint: %v", err)
	} else {
		resp, err := hc.client.Do(req)
		if err != nil {
			status = healthStatusDown
			errMsg = fmt.Sprintf("connection failed: %v", err)
		} else {
			resp.Body.Close()
			// Accept any non-5xx as healthy (some providers return 401/405 for HEAD)
			if resp.StatusCode >= 500 {
				status = healthStatusDown
				errMsg = fmt.Sprintf("server error: %d", resp.StatusCode)
			}
		}
	}

	latencyMs := time.Since(start).Milliseconds()

	// Track consecutive failures
	hc.mu.Lock()
	if status == healthStatusDown {
		hc.failures[cfg.ID]++
	} else {
		hc.failures[cfg.ID] = 0
	}
	consecutiveFailures := hc.failures[cfg.ID]
	hc.mu.Unlock()

	// Update model enabled status based on failure count
	now := time.Now()
	if consecutiveFailures >= maxFailures && cfg.Enabled {
		// Auto-disable after 3 consecutive failures
		slog.Warn("healthcheck: disabling model after consecutive failures",
			"model", cfg.ModelName, "failures", consecutiveFailures)
		hc.llmStore.UpdateHealthStatus(context.Background(), cfg.ID, false, now)
	} else if status == healthStatusOK && !cfg.Enabled {
		// Auto-enable when recovered (but only if it was auto-disabled, not manually)
		// For safety, we don't auto-re-enable here — admin should review
	}

	// Always update last check time
	if cfg.Enabled {
		hc.llmStore.UpdateHealthStatus(context.Background(), cfg.ID, cfg.Enabled, now)
	}

	// Record health log
	log := &database.ModelHealthLog{
		ModelID:    cfg.ID,
		ModelName:  cfg.ModelName,
		Provider:   cfg.Provider,
		Status:     status,
		LatencyMs:  latencyMs,
		ErrorMsg:   errMsg,
		CheckedAt:  now,
	}
	if err := hc.logStore.Create(context.Background(), log); err != nil {
		slog.Error("healthcheck: failed to record log", "error", err, "model", cfg.ModelName)
	}

	slog.Debug("healthcheck: model checked",
		"model", cfg.ModelName, "status", status, "latency_ms", latencyMs)
}

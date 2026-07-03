package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/gin-gonic/gin"
	"opencsg.com/csghub-server/aigateway/types"
	"opencsg.com/csghub-server/builder/proxy"
	"opencsg.com/csghub-server/common/config"
)

// fallbackProbeWriter is a response writer that captures the first response to check for errors.
// It buffers everything until we decide to commit or discard.
type fallbackProbeWriter struct {
	header     http.Header
	statusCode int
	body       bytes.Buffer
	committed  bool
}

func newFallbackProbeWriter() *fallbackProbeWriter {
	return &fallbackProbeWriter{
		header:     make(http.Header),
		statusCode: http.StatusOK,
	}
}

func (w *fallbackProbeWriter) Header() http.Header     { return w.header }
func (w *fallbackProbeWriter) WriteHeader(code int)     { w.statusCode = code }
func (w *fallbackProbeWriter) Write(b []byte) (int, error) { return w.body.Write(b) }

func (w *fallbackProbeWriter) isServerError() bool {
	return w.statusCode >= 500
}

// proxyWithFallback attempts to proxy a request to the primary endpoint.
// If it gets a 5xx response AND there are fallback endpoints, it retries
// with each fallback in order (up to maxRetries).
// For streaming requests, the probe buffers the first response — if it's
// an error (5xx), it discards and retries. If it's 2xx, it flushes the
// buffered data to the real writer and then pipes the rest.
//
// Returns the actual provider used.
func proxyWithFallback(
	c *gin.Context,
	realWriter CommonResponseWriter,
	chatReq *ChatCompletionRequest,
	model *types.Model,
	cfg *config.Config,
	maxRetries int,
) string {
	// Build ordered list of endpoints to try
	type endpoint struct {
		provider string
		url      string
		authHead string
	}
	endpoints := []endpoint{
		{provider: model.Provider, url: model.Endpoint, authHead: model.AuthHead},
	}
	for _, fb := range model.Fallbacks {
		endpoints = append(endpoints, endpoint{provider: fb.Provider, url: fb.Endpoint, authHead: fb.AuthHead})
	}

	// Limit retries
	if maxRetries <= 0 {
		maxRetries = 2
	}
	maxAttempts := maxRetries + 1
	if maxAttempts > len(endpoints) {
		maxAttempts = len(endpoints)
	}

	// Save original request body for replays
	bodyBytes, _ := json.Marshal(chatReq)

	for i := 0; i < maxAttempts; i++ {
		ep := endpoints[i]

		slog.Info("attempting provider", "attempt", i+1, "provider", ep.provider, "endpoint", ep.url)

		// Reset request body for each attempt
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		c.Request.ContentLength = int64(len(bodyBytes))

		// Set auth headers for this endpoint
		injectAuthHeaders(c, cfg, ep.provider, ep.url, ep.authHead)

		// Parse target
		target := ep.url
		proxyToApi := ""
		if ep.url != "" {
			if uri, err := url.ParseRequestURI(ep.url); err == nil {
				proxyToApi = uri.Path
			}
		}

		rp, err := proxy.NewReverseProxy(target)
		if err != nil {
			slog.Error("failed to create reverse proxy", "error", err, "endpoint", ep.url)
			continue
		}

		// For the last attempt or if no fallbacks, go direct to real writer
		if i == maxAttempts-1 || len(model.Fallbacks) == 0 {
			c.Header("X-Provider", ep.provider)
			rp.ServeHTTP(realWriter, c.Request, proxyToApi, "")
			return ep.provider
		}

		// Probe attempt: use a buffered writer with timeout
		probe := newFallbackProbeWriter()
		probeCtx, probeCancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
		done := make(chan struct{})
		go func() {
			rp.ServeHTTP(probe, c.Request, proxyToApi, "")
			close(done)
		}()

		select {
		case <-done:
			probeCancel()
			// Completed
		case <-probeCtx.Done():
			probeCancel()
			// Timeout — treat as failure
			slog.Warn("provider timeout, trying fallback", "provider", ep.provider, "attempt", i+1)
			continue
		}

		if probe.isServerError() {
			slog.Warn("provider returned 5xx, trying fallback",
				"provider", ep.provider,
				"status", probe.statusCode,
				"attempt", i+1,
				"body", probe.body.String()[:min(200, probe.body.Len())],
			)
			continue
		}

		// Success — flush probe buffer to real writer
		c.Header("X-Provider", ep.provider)
		for k, vals := range probe.header {
			for _, v := range vals {
				realWriter.Header().Set(k, v)
			}
		}
		realWriter.WriteHeader(probe.statusCode)
		realWriter.Write(probe.body.Bytes())
		realWriter.Flush()
		return ep.provider
	}

	// Should not reach here, but just in case
	return endpoints[0].provider
}

func injectAuthHeaders(c *gin.Context, cfg *config.Config, provider, endpoint, authHead string) {
	authJSON := buildProviderAuthHead(cfg, provider, endpoint, authHead)
	if authJSON == "" {
		return
	}
	var authMap map[string]string
	if err := json.Unmarshal([]byte(authJSON), &authMap); err != nil {
		slog.Warn("invalid auth head for fallback", "provider", provider)
		return
	}
	for k, v := range authMap {
		c.Request.Header.Set(k, v)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

package handler

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/builder/store/cache"
	"github.com/aitra-ai/aitra-server/common/config"
)

// ResponseCache provides response caching for non-streaming chat completions.
// Cache key = SHA256(model + messages JSON + temperature).
// Streaming requests are never cached.
type ResponseCache struct {
	cache   cache.RedisClient
	enabled bool
	ttl     time.Duration
}

func NewResponseCache(cfg *config.Config) (*ResponseCache, error) {
	if !cfg.AIGateway.CacheEnabled {
		return &ResponseCache{enabled: false}, nil
	}
	cacheClient, err := cache.NewCache(context.Background(), cache.RedisConfig{
		Addr:     cfg.Redis.Endpoint,
		Username: cfg.Redis.User,
		Password: cfg.Redis.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect Redis for response cache: %w", err)
	}
	ttl := time.Duration(cfg.AIGateway.CacheTTL) * time.Second
	if ttl <= 0 {
		ttl = time.Hour
	}
	return &ResponseCache{
		cache:   cacheClient,
		enabled: true,
		ttl:     ttl,
	}, nil
}

type cacheEntry struct {
	StatusCode int               `json:"status_code"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// buildCacheKey creates a deterministic hash from model + messages + temperature.
func buildCacheKey(chatReq *ChatCompletionRequest) string {
	h := sha256.New()
	h.Write([]byte(chatReq.Model))
	msgBytes, _ := json.Marshal(chatReq.Messages)
	h.Write(msgBytes)
	h.Write([]byte(fmt.Sprintf("%.2f", chatReq.Temperature)))
	return "aitra:cache:" + hex.EncodeToString(h.Sum(nil))
}

// Middleware returns a gin middleware for response caching.
// Must be placed AFTER body parsing but BEFORE the actual proxy handler.
// It works by:
// 1. Checking if the request is cacheable (non-streaming, no X-No-Cache header)
// 2. On cache hit: return cached response immediately
// 3. On cache miss: let the request proceed, capture response, store in cache
func (rc *ResponseCache) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if !rc.enabled {
			c.Next()
			return
		}

		// Only cache POST /v1/chat/completions
		if c.Request.Method != http.MethodPost {
			c.Next()
			return
		}

		// Read and restore body for downstream
		bodyBytes, err := io.ReadAll(c.Request.Body)
		if err != nil {
			c.Next()
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(bodyBytes))

		// Parse request to check stream flag
		var chatReq ChatCompletionRequest
		if err := json.Unmarshal(bodyBytes, &chatReq); err != nil {
			c.Next()
			return
		}

		// Don't cache streaming requests
		if chatReq.Stream {
			c.Next()
			return
		}

		// User opt-out
		if c.GetHeader("X-No-Cache") == "true" {
			c.Next()
			return
		}

		cacheKey := buildCacheKey(&chatReq)

		// Try cache hit
		cached, err := rc.cache.Get(c.Request.Context(), cacheKey)
		if err == nil && cached != "" {
			var entry cacheEntry
			if json.Unmarshal([]byte(cached), &entry) == nil {
				slog.Debug("cache hit", "key", cacheKey[:30])
				c.Header("X-Cache", "HIT")
				for k, v := range entry.Headers {
					c.Header(k, v)
				}
				c.Data(entry.StatusCode, "application/json", []byte(entry.Body))
				c.Abort()
				return
			}
		}

		// Cache miss — capture response
		c.Header("X-Cache", "MISS")

		// Use a response capture writer
		capturer := &responseCapturer{
			ResponseWriter: c.Writer,
			body:           &bytes.Buffer{},
			statusCode:     http.StatusOK,
		}
		c.Writer = capturer

		c.Next()

		// After handler completes, store in cache if successful
		if capturer.statusCode >= 200 && capturer.statusCode < 300 && capturer.body.Len() > 0 {
			entry := cacheEntry{
				StatusCode: capturer.statusCode,
				Headers: map[string]string{
					"Content-Type": capturer.Header().Get("Content-Type"),
				},
				Body: capturer.body.String(),
			}
			entryJSON, _ := json.Marshal(entry)
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				if err := rc.cache.SetEx(ctx, cacheKey, string(entryJSON), rc.ttl); err != nil {
					slog.Error("failed to cache response", "error", err, "key", cacheKey[:30])
				}
			}()
		}
	}
}

// responseCapturer wraps gin.ResponseWriter to capture the response body.
type responseCapturer struct {
	gin.ResponseWriter
	body       *bytes.Buffer
	statusCode int
}

func (w *responseCapturer) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *responseCapturer) Write(b []byte) (int, error) {
	w.body.Write(b)
	return w.ResponseWriter.Write(b)
}

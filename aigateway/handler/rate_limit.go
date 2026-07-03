package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/builder/store/cache"
	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/common/config"
)

// RateLimiter provides RPM (requests per minute) limiting for aigateway endpoints.
// Uses Redis sliding window (sorted set) for accurate per-user rate limiting.
type RateLimiter struct {
	cache      cache.RedisClient
	rlStore    database.UserRateLimitStore
	userStore  database.UserStore
	defaultRPM int
	defaultTPM int
}

func NewRateLimiter(cfg *config.Config) (*RateLimiter, error) {
	cacheClient, err := cache.NewCache(context.Background(), cache.RedisConfig{
		Addr:     cfg.Redis.Endpoint,
		Username: cfg.Redis.User,
		Password: cfg.Redis.Password,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect Redis for rate limiter: %w", err)
	}
	return &RateLimiter{
		cache:      cacheClient,
		rlStore:    database.NewUserRateLimitStore(),
		userStore:  database.NewUserStore(),
		defaultRPM: cfg.AIGateway.DefaultRPM,
		defaultTPM: cfg.AIGateway.DefaultTPM,
	}, nil
}

// Middleware returns a gin middleware that enforces RPM limits per user.
func (rl *RateLimiter) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		username := httpbase.GetCurrentUser(c)
		if username == "" {
			c.Next()
			return
		}

		// Look up user's RPM limit
		rpm := rl.defaultRPM
		user, err := rl.userStore.FindByUsername(c.Request.Context(), username)
		if err == nil {
			userRL, err := rl.rlStore.FindByUserID(c.Request.Context(), user.ID)
			if err == nil && userRL != nil {
				rpm = userRL.RPM
			}
		}

		// 0 = unlimited
		if rpm <= 0 {
			c.Next()
			return
		}

		// Sliding window rate limit using Redis sorted set
		key := fmt.Sprintf("aitra:ratelimit:rpm:%s", username)
		now := time.Now()
		windowStart := now.Add(-1 * time.Minute)

		// Lua script: remove old entries, add current, count
		luaScript := `
local key = KEYS[1]
local now = tonumber(ARGV[1])
local window_start = tonumber(ARGV[2])
local limit = tonumber(ARGV[3])
local member = ARGV[4]

redis.call('ZREMRANGEBYSCORE', key, '-inf', window_start)
local count = redis.call('ZCARD', key)
if count >= limit then
    return count
end
redis.call('ZADD', key, now, member)
redis.call('EXPIRE', key, 120)
return -1
`
		member := fmt.Sprintf("%d:%d", now.UnixNano(), now.UnixMicro()%10000)
		result, err := rl.cache.RunScript(c.Request.Context(), luaScript,
			[]string{key},
			now.UnixMilli(),
			windowStart.UnixMilli(),
			rpm,
			member,
		)
		if err != nil {
			slog.Error("rate limit Redis error, allowing request", "error", err, "user", username)
			c.Next()
			return
		}

		count, ok := result.(int64)
		if !ok {
			c.Next()
			return
		}

		if count >= 0 {
			// Rate limited
			retryAfter := 60 - int(time.Since(windowStart).Seconds())
			if retryAfter < 1 {
				retryAfter = 1
			}
			c.Header("Retry-After", strconv.Itoa(retryAfter))
			c.Header("X-RateLimit-Limit-RPM", strconv.Itoa(rpm))
			c.Header("X-RateLimit-Remaining", "0")
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": gin.H{
					"type":    "rate_limit_error",
					"code":    "rate_limit_exceeded",
					"message": fmt.Sprintf("Rate limit exceeded: %d requests per minute. Please retry after %d seconds.", rpm, retryAfter),
				},
			})
			c.Abort()
			return
		}

		// Set rate limit headers
		c.Header("X-RateLimit-Limit-RPM", strconv.Itoa(rpm))
		c.Next()
	}
}

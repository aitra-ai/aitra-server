package middleware

import (
	"log/slog"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/common/utils/trace"
)

func Log() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		if ctx.Request.URL.Path == "/healthz" {
			ctx.Next()
			return
		}

		startTime := time.Now()
		_ = trace.GetOrGenTraceID(ctx)

		ctx.Next()

		latency := time.Since(startTime).Milliseconds()
		slog.InfoContext(ctx, "http request", slog.String("ip", ctx.ClientIP()),
			slog.String("method", ctx.Request.Method),
			slog.Int("latency(ms)", int(latency)),
			slog.Int("status", ctx.Writer.Status()),
			slog.String("current_user", httpbase.GetCurrentUser(ctx)),
			slog.Any("auth_type", httpbase.GetAuthType(ctx)),
			slog.String("url", ctx.Request.URL.RequestURI()),
			slog.String("full_path", ctx.FullPath()),
		)
	}
}

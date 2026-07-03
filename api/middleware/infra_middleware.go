package middleware

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"github.com/aitra-ai/aitra-server/builder/instrumentation"
	"github.com/aitra-ai/aitra-server/common/config"
)

func SetInfraMiddleware(r *gin.Engine, config *config.Config, serviceName string) {
	r.Use(Recovery())
	instrumentation.SetupOtelMiddleware(r, config, serviceName)
	r.Use(Log())
	r.Use(Request())

	// Unified health check
	// Since readinessProbe cannot send a head request, use the get method
	if serviceName != instrumentation.RProxy {
		r.GET("/healthz", func(ctx *gin.Context) {
			ctx.Status(http.StatusOK)
		})
	}
}

//go:build !ee && !saas

package router

import (
	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/middleware"
	"github.com/aitra-ai/aitra-server/common/config"
)

func extendRoutes(_ *gin.RouterGroup, _ middleware.MiddlewareCollection, _ *config.Config) error {
	return nil
}

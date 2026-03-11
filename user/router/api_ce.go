//go:build !ee && !saas

package router

import (
	"github.com/gin-gonic/gin"
	"opencsg.com/csghub-server/api/middleware"
	"opencsg.com/csghub-server/common/config"
	"opencsg.com/csghub-server/user/handler"
)

func extendRoutes(rg *gin.RouterGroup, mc middleware.MiddlewareCollection, cfg *config.Config) error {
	sandboxH := handler.NewSandboxHandler(cfg)

	public := rg.Group("/sandbox")
	{
		public.GET("/featured", sandboxH.ListFeaturedSpaces)
		public.GET("/instances/:id/status", sandboxH.GetSandboxStatus)
	}

	auth := rg.Group("/sandbox")
	auth.Use(mc.Auth.NeedLogin)
	{
		auth.POST("/spaces/:namespace/:name/launch", sandboxH.LaunchSandbox)
		auth.DELETE("/instances/:id", sandboxH.StopSandbox)
		auth.GET("/instances", sandboxH.ListMySandboxes)
	}

	adminGroup := rg.Group("/admin/sandbox")
	adminGroup.Use(mc.Auth.NeedAdmin)
	{
		adminGroup.GET("/featured", sandboxH.AdminListFeaturedSpaces)
		adminGroup.POST("/featured", sandboxH.AdminCreateFeaturedSpace)
		adminGroup.PUT("/featured/:id", sandboxH.AdminUpdateFeaturedSpace)
		adminGroup.DELETE("/featured/:id", sandboxH.AdminDeleteFeaturedSpace)
		adminGroup.GET("/instances", sandboxH.AdminListInstances)
	}

	return nil
}

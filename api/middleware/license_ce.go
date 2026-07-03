//go:build !ee && !saas

package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/common/config"
)

func CheckLicense(_ *config.Config) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
	}
}

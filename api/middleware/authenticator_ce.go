//go:build !saas

package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/common/config"
)

func NeedPhoneVerified(config *config.Config) gin.HandlerFunc {
	return MustLogin()
}

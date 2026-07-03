//go:build !ee && !saas

package router

import (
	"github.com/gin-gonic/gin"
	bldmq "github.com/aitra-ai/aitra-server/builder/mq"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/mq"
)

func createAdvancedRoutes(apiGroup *gin.RouterGroup, config *config.Config, mqHandler mq.MessageQueue, mqFactory bldmq.MessageQueueFactory) error {
	return nil
}

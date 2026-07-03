//go:build !ee && !saas

package accounting

import (
	bldmq "github.com/aitra-ai/aitra-server/builder/mq"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/mq"
)

func createAdvancedConsumer(cfg *config.Config, mqHandler mq.MessageQueue, mqFactory bldmq.MessageQueueFactory) error {
	return nil
}

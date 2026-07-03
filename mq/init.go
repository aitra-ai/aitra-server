package mq

import (
	"github.com/aitra-ai/aitra-server/common/config"
)

var (
	SystemMQ MessageQueue
)

func GetOrInit(config *config.Config) (MessageQueue, error) {
	if SystemMQ != nil {
		return SystemMQ, nil
	}
	mq, err := NewNats(config)
	if err != nil {
		return nil, err
	}
	if err := mq.GetJetStream(); err != nil {
		return nil, err
	}
	SystemMQ = mq
	return SystemMQ, nil
}

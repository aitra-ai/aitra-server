package reporter

import (
	"github.com/aitra-ai/aitra-server/common/config"
	"time"

	ltypes "github.com/aitra-ai/aitra-server/logcollector/types"
)

// Config returns a default configuration
func Config(config *config.Config) *ltypes.LogCollectorConfig {
	return &ltypes.LogCollectorConfig{
		LokiURL:              config.LogCollector.LokiURL,
		MaxConcurrentStreams: config.LogCollector.MaxConcurrentStreams,
		BatchSize:            config.LogCollector.BatchSize,
		BatchDelay:           time.Duration(config.LogCollector.BatchDelay) * time.Second,
		DropMsgTimeout:       time.Duration(config.LogCollector.DropMsgTimeout) * time.Second,
		MaxRetries:           config.LogCollector.MaxRetries,
		RetryInterval:        time.Duration(config.LogCollector.RetryInterval) * time.Second,
		HealthCheckInterval:  time.Duration(config.LogCollector.HealthInterval) * time.Second,
	}
}

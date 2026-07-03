package sender

import (
	"context"
	"time"

	"github.com/aitra-ai/aitra-server/builder/loki"
	"github.com/aitra-ai/aitra-server/common/types"
)

// LogSender is the interface for sending logs to a backend
type LogSender interface {
	// SendLogs sends a batch of log entries
	SendLogs(ctx context.Context, entries []types.LogEntry) error
	// Health checks the health of the log sending backend
	Health(ctx context.Context) error
	// GetLastReportedTimestamp gets the timestamp of the last successfully sent log
	GetLastReportedTimestamp(ctx context.Context) (time.Time, error)
	// StreamAllLogs streams all logs from the backend
	StreamAllLogs(ctx context.Context, id string, start time.Time, lables map[string]string, timeLoc *time.Location) (chan string, error)
	QueryRange(ctx context.Context, params loki.QueryRangeParams) (*loki.LokiQueryResponse, error)
}

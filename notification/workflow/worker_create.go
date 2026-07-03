package workflow

import (
	"log/slog"

	"github.com/aitra-ai/aitra-server/builder/temporal"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/notification/notifychannel/worker"

	// blank import to register workers via their init() function.
	_ "github.com/aitra-ai/aitra-server/notification/notifychannel/channel/email/workflow"
	_ "github.com/aitra-ai/aitra-server/notification/notifychannel/channel/internalmsg/workflow"
)

func createWorker(cfg *config.Config, workflowClient temporal.Client) {
	for name, creator := range worker.GetWorkerCreators() {
		slog.Info("Starting worker for notification channel", "channel", name)
		creator(cfg, workflowClient)
	}
	extendWorker(cfg, workflowClient)
}

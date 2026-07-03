package workflow

import (
	"fmt"

	temporalActivity "go.temporal.io/sdk/activity"
	temporalWorker "go.temporal.io/sdk/worker"
	"github.com/aitra-ai/aitra-server/builder/rpc"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/builder/temporal"
	"github.com/aitra-ai/aitra-server/common/config"
	activity "github.com/aitra-ai/aitra-server/notification/notifychannel/channel/internalmsg/workflow/activity"
	"github.com/aitra-ai/aitra-server/notification/notifychannel/worker"
)

const (
	WorkflowBroadcastInternalMessageQueueName string = "workflow_broadcast_internal_message_queue"
	InsertUserMessageBatchActivity            string = "InsertUserMessageBatchActivity"
	LogUserMessageFailuresActivity            string = "LogUserMessageFailuresActivity"
)

func init() {
	worker.RegisterWorker("internalmsg", createBroadcastInternalMessageWorker)
}

func createBroadcastInternalMessageWorker(config *config.Config, temporalClient temporal.Client) {
	storage := database.NewNotificationStore()
	userSvcAddr := fmt.Sprintf("%s:%d", config.User.Host, config.User.Port)
	userSvcClient := rpc.NewUserSvcHttpClient(userSvcAddr, rpc.AuthWithApiKey(config.APIToken))

	act := activity.NewBroadcastMessageActivity(storage, userSvcClient)
	beWorker := temporalClient.NewWorker(WorkflowBroadcastInternalMessageQueueName, temporalWorker.Options{})
	beWorker.RegisterWorkflow(BroadcastInternalMessageWorkflow)
	beWorker.RegisterActivityWithOptions(act.InsertUserMessageBatchActivity, temporalActivity.RegisterOptions{Name: InsertUserMessageBatchActivity})
	beWorker.RegisterActivityWithOptions(act.LogUserMessageFailuresActivity, temporalActivity.RegisterOptions{Name: LogUserMessageFailuresActivity})
}

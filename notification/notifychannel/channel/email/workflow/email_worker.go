package workflow

import (
	"fmt"
	"log/slog"

	temporalActivity "go.temporal.io/sdk/activity"
	temporalWorker "go.temporal.io/sdk/worker"
	"github.com/aitra-ai/aitra-server/builder/rpc"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/builder/temporal"
	"github.com/aitra-ai/aitra-server/common/config"
	emailclient "github.com/aitra-ai/aitra-server/notification/notifychannel/channel/email/client"
	"github.com/aitra-ai/aitra-server/notification/notifychannel/channel/email/workflow/activity"
	"github.com/aitra-ai/aitra-server/notification/notifychannel/worker"
)

const (
	GetEmailFromNotificationSettingActivity string = "GetEmailFromNotificationSettingActivity"
	GetEmailFromUserActivity                string = "GetEmailFromUserActivity"
	SendEmailBatchActivity                  string = "SendEmailBatchActivity"
)

func init() {
	worker.RegisterWorker("email", createBroadcastEmailWorker)
}

func createBroadcastEmailWorker(config *config.Config, temporalClient temporal.Client) {
	storage := database.NewNotificationStore()
	userSvcAddr := fmt.Sprintf("%s:%d", config.User.Host, config.User.Port)
	userSvcClient := rpc.NewUserSvcHttpClient(userSvcAddr, rpc.AuthWithApiKey(config.APIToken))

	var emailService emailclient.EmailService
	var err error
	if config.Notification.DirectMailEnabled {
		emailService, err = emailclient.NewDirectMailClient(config)
		if err != nil {
			slog.Error("failed to create direct mail client", "error", err)
			return
		}
	} else {
		emailService = emailclient.NewEmailService(config)
	}

	act := activity.NewBroadcastEmailActivity(storage, userSvcClient, emailService)
	beWorker := temporalClient.NewWorker(WorkflowBroadcastEmailQueueName, temporalWorker.Options{})
	beWorker.RegisterWorkflow(BroadcastEmailWorkflow)
	beWorker.RegisterActivityWithOptions(act.GetEmailFromNotificationSettingActivity, temporalActivity.RegisterOptions{Name: GetEmailFromNotificationSettingActivity})
	beWorker.RegisterActivityWithOptions(act.GetEmailFromUserActivity, temporalActivity.RegisterOptions{Name: GetEmailFromUserActivity})
	beWorker.RegisterActivityWithOptions(act.SendEmailBatchActivity, temporalActivity.RegisterOptions{Name: SendEmailBatchActivity})
}

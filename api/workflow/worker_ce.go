//go:build !ee && !saas

package workflow

import (
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"go.temporal.io/sdk/worker"
	"github.com/aitra-ai/aitra-server/api/workflow/activity"
	"github.com/aitra-ai/aitra-server/builder/git"
	"github.com/aitra-ai/aitra-server/builder/git/gitserver"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/builder/temporal"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/component"
	"github.com/aitra-ai/aitra-server/component/callback"
)

const HandlePushQueueName = "workflow_handle_push_queue"

var activities activity.Activities

func StartWorkflow(cfg *config.Config, registerAsWorker bool) error {
	gitcallback, err := callback.NewGitCallback(cfg)
	if err != nil {
		return err
	}
	recom, err := component.NewRecomComponent(cfg)
	if err != nil {
		return err
	}
	gitserver, err := git.NewGitServer(cfg)
	if err != nil {
		return err
	}
	multisync, err := component.NewMultiSyncComponent(cfg)
	if err != nil {
		return err
	}
	client, err := temporal.NewClient(client.Options{
		HostPort: cfg.WorkFLow.Endpoint,
		Logger:   log.NewStructuredLogger(slog.Default()),
		ConnectionOptions: client.ConnectionOptions{
			GetSystemInfoTimeout: time.Duration(cfg.Temporal.GetSystemInfoTimeout) * time.Second,
		},
	}, "csghub-api")
	if err != nil {
		return fmt.Errorf("unable to create workflow client, error: %w", err)
	}
	if err != nil {
		return err
	}
	rftScanner, err := component.NewRuntimeArchitectureComponent(cfg)
	if err != nil {
		return err
	}
	repoComponent, err := component.NewRepoComponent(cfg)
	if err != nil {
		return err
	}

	return StartWorkflowDI(
		cfg, gitcallback, recom,
		gitserver, multisync, database.NewSyncClientSettingStore(), client,
		rftScanner, repoComponent, registerAsWorker,
	)
}

func StartWorkflowDI(
	cfg *config.Config,
	callback callback.GitCallbackComponent,
	recom component.RecomComponent,
	gitServer gitserver.GitServer,
	multisync component.MultiSyncComponent,
	syncClientSetting database.SyncClientSettingStore,
	temporalClient temporal.Client,
	rftScanner component.RuntimeArchitectureComponent,
	repoComponent component.RepoComponent,
	registerAsWorker bool,
) error {
	if registerAsWorker {
		worker := temporalClient.NewWorker(HandlePushQueueName, worker.Options{})
		act := activity.NewActivities(cfg, callback, recom, gitServer, multisync, syncClientSetting, rftScanner, repoComponent)
		worker.RegisterActivity(act)

		worker.RegisterWorkflow(HandlePushWorkflow)
		worker.RegisterWorkflow(RuntimeFrameworkWorkflow)

		RegisterCronWorker(cfg, temporalClient, act)
		err := RegisterCronJobs(cfg, temporalClient)
		if err != nil {
			return fmt.Errorf("failed to register cron jobs:  %w", err)
		}
	}

	err := temporalClient.Start()
	if err != nil {
		return fmt.Errorf("failed to start worker:  %w", err)
	}
	return nil

}

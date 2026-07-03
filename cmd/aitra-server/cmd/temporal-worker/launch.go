package temporal_worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/aitra-ai/aitra-server/moderation/checker"

	"github.com/aitra-ai/aitra-server/builder/deploy"
	"github.com/aitra-ai/aitra-server/builder/deploy/common"
	"github.com/aitra-ai/aitra-server/builder/deploy/imagebuilder"
	"github.com/aitra-ai/aitra-server/builder/deploy/imagerunner"
	"github.com/aitra-ai/aitra-server/builder/event"
	"github.com/aitra-ai/aitra-server/builder/git"
	"github.com/aitra-ai/aitra-server/builder/instrumentation"
	"github.com/aitra-ai/aitra-server/component/reporter"

	"github.com/spf13/cobra"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	serverworkflow "github.com/aitra-ai/aitra-server/api/workflow"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/builder/temporal"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
	moderationworkflow "github.com/aitra-ai/aitra-server/moderation/workflow"
	notificationworkflow "github.com/aitra-ai/aitra-server/notification/workflow"
	userworkflow "github.com/aitra-ai/aitra-server/user/workflow"
)

var initVersionWorker = func(cfg *config.Config) error {
	return nil
}

var cmdLaunch = &cobra.Command{
	Use:     "launch",
	Short:   "Launch temporal worker server",
	Example: serverExample(),
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		cfg, err := config.LoadConfig()
		if err != nil {
			return fmt.Errorf("failed to load config, error: %w", err)
		}
		stopOtel, err := instrumentation.SetupOTelSDK(context.Background(), cfg, instrumentation.TemporalWorker)
		if err != nil {
			panic(err)
		}

		dbConfig := database.DBConfig{
			Dialect: database.DatabaseDialect(cfg.Database.Driver),
			DSN:     cfg.Database.DSN,
		}
		if err := database.InitDB(dbConfig); err != nil {
			return fmt.Errorf("failed to init database, error: %w", err)
		}

		if cfg.SensitiveCheck.Enable {
			slog.Info("init sensitive checker")
			checker.Init(cfg)
		}

		slog.Info("init event publisher")
		err = event.InitEventPublisher(cfg)
		if err != nil {
			return fmt.Errorf("fail to initialize message queue, %w", err)
		}

		deploy.DeployWorkflow = func(buildTask, runTask *database.DeployTask) {
			if err := serverworkflow.StartNewDeployTaskWithCancelOld(buildTask, runTask); err != nil {
				slog.Error("start new deploy task failed", slog.Any("error", err))
			}
		}
		slog.Info("init model inference deployer")
		err = deploy.Init(common.BuildDeployConfig(cfg), cfg, false)
		if err != nil {
			return fmt.Errorf("failed to init deploy: %w", err)
		}

		slog.Info("starting temporal client")
		temporalClient, err := temporal.NewClient(client.Options{
			HostPort: cfg.WorkFLow.Endpoint,
			Logger:   log.NewStructuredLogger(slog.Default()),
			ConnectionOptions: client.ConnectionOptions{
				GetSystemInfoTimeout: time.Duration(cfg.Temporal.GetSystemInfoTimeout) * time.Second,
			},
		}, instrumentation.TemporalWorker)
		if err != nil {
			return fmt.Errorf("unable to create temporal client, error: %w", err)
		}

		slog.Info("start server temporal workflow")
		err = serverworkflow.StartWorkflow(cfg, true)
		if err != nil {
			return fmt.Errorf("failed to start server workflow, error: %w", err)
		}

		slog.Info("start moderation temporal workflow")
		err = moderationworkflow.StartWorker(cfg)
		if err != nil {
			return fmt.Errorf("failed to start moderation worker, error: %w", err)
		}

		slog.Info("start notification temporal workflow")
		err = notificationworkflow.StartWorkflow(cfg)
		if err != nil {
			return fmt.Errorf("failed to start notification worker, error: %w", err)
		}

		slog.Info("start user temporal workflow")
		err = userworkflow.StartWorker(cfg)
		if err != nil {
			return fmt.Errorf("failed to start user worker, error: %w", err)
		}

		slog.Info("start deploy temporal workflow")
		deployCfg := common.BuildDeployConfig(cfg)

		gitserver, err := git.NewGitServer(cfg)
		if err != nil {
			return err
		}
		lr, err := reporter.NewAndStartLogCollector(context.TODO(), cfg, types.ClientTypeCSGHUB)
		if err != nil {
			return fmt.Errorf("failed to create log reporter:%w", err)
		}
		ib, err := imagebuilder.NewRemoteBuilder(cfg.Space.BuilderEndpoint, deployCfg)
		if err != nil {
			panic(fmt.Errorf("failed to create image builder:%w", err))
		}
		ir, err := imagerunner.NewRemoteRunner(cfg.Space.RunnerEndpoint, deployCfg)
		if err != nil {
			panic(fmt.Errorf("failed to create image runner:%w", err))
		}
		ds := database.NewDeployTaskStore()
		ts := database.NewAccessTokenStore()
		ss := database.NewSpaceStore()
		ms := database.NewModelStore()
		rfs := database.NewRuntimeFrameworksStore()
		urs := database.NewUserResourcesStore()
		mds := database.NewMetadataStore()
		cls := database.NewClusterInfoStore()
		err = serverworkflow.StartDeployWorker(cmd.Context(), cfg, temporalClient, lr, ib, ir, gitserver, ds, ts, ss, ms, rfs, urs, mds, cls)
		if err != nil {
			return fmt.Errorf("failed to start deploy worker, error: %w", err)
		}

		if err := initVersionWorker(cfg); err != nil {
			return fmt.Errorf("failed to start user worker, error: %w", err)
		}

		err = temporalClient.Start()
		if err != nil {
			return fmt.Errorf("failed to start worker, error: %w", err)
		}

		slog.Info("all worker started～～～")

		<-cmd.Context().Done()

		slog.Info("worker shutting down")
		_ = stopOtel(context.Background())
		temporalClient.Stop()
		return nil
	},
}

func serverExample() string {
	return `aitra-server temporal-worker --config=config/config.yaml`
}

package start

import (
	"context"
	"fmt"
	"log/slog"
	"net/url"
	"time"

	"github.com/spf13/cobra"
	"github.com/aitra-ai/aitra-server/api/router"
	"github.com/aitra-ai/aitra-server/api/workflow"
	"github.com/aitra-ai/aitra-server/builder/deploy"
	"github.com/aitra-ai/aitra-server/builder/deploy/common"
	"github.com/aitra-ai/aitra-server/builder/event"
	"github.com/aitra-ai/aitra-server/builder/redis"
	"github.com/aitra-ai/aitra-server/builder/store/cache"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/builder/store/database/migrations"
	"github.com/aitra-ai/aitra-server/common/config"
	commontypes "github.com/aitra-ai/aitra-server/common/types"
	"github.com/aitra-ai/aitra-server/docs"
	"github.com/aitra-ai/aitra-server/energy"
	energyservice "github.com/aitra-ai/aitra-server/energy/service"
)

var enableSwagger bool

func init() {
	serverCmd.Flags().BoolVar(&enableSwagger, "swagger", false, "Start swagger help docs")
}

var serverCmd = &cobra.Command{
	Use:     "server",
	Short:   "Start the API server",
	Example: serverExample(),
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		enableSwagger = enableSwagger || cfg.EnableSwagger

		if enableSwagger {
			//	@securityDefinitions.apikey ApiKey
			//	@in                         header
			//	@name                       Authorization
			//	@description                Bearer token
			publicDomain, err := url.Parse(cfg.APIServer.PublicDomain)
			if err != nil {
				return fmt.Errorf("failed to parse api server public domain: %v", err)
			}
			docs.SwaggerInfo.Title = "CSGHub Server API"
			docs.SwaggerInfo.Description = "CSGHub Server API."
			docs.SwaggerInfo.Version = "1.0"
			docs.SwaggerInfo.Host = publicDomain.Host
			docs.SwaggerInfo.BasePath = "/api/v1"
			docs.SwaggerInfo.Schemes = []string{"http", "https"}
		}

		// Check APIToken length
		if len(cfg.APIToken) < 128 {
			return fmt.Errorf("API token length is less than 128, please check")
		}
		dbConfig := database.DBConfig{
			Dialect: database.DatabaseDialect(cfg.Database.Driver),
			DSN:     cfg.Database.DSN,
		}
		slog.Info("init database connection", "dialect", dbConfig.Dialect)
		if err := database.InitDB(dbConfig); err != nil {
			slog.Error("failed to initialize database", slog.Any("error", err))
			return fmt.Errorf("database initialization failed: %w", err)
		}

		migrator := migrations.NewMigrator(database.GetDB())

		slog.Info("run migration")
		ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
		defer cancel()

		locker, err := cache.NewCache(cmd.Context(), cache.RedisConfig{
			Addr:     cfg.Redis.Endpoint,
			Username: cfg.Redis.User,
			Password: cfg.Redis.Password,
		})
		if err != nil {
			return fmt.Errorf("initializing locker: %w", err)
		}

		err = locker.RunWhileLocked(ctx, "migration_migrate", 1*time.Minute, func(ctx context.Context) error {
			// migration init
			err = migrator.Init(ctx)
			if err != nil {
				slog.Error("failed to init migration", slog.Any("error", err))
				return fmt.Errorf("failed to init migration: %w", err)
			}

			// migration migrate
			group, err := migrator.Migrate(ctx)
			if err != nil {
				return fmt.Errorf("failed to migrate: %w", err)
			}
			if group.IsZero() {
				slog.Info("there are no new migrations to run (database is up to date)")
			}
			slog.Info(fmt.Sprintf("migrated to %s", group))
			return nil
		})

		if err != nil {
			return fmt.Errorf("failed to run migration: %w", err)
		}

		slog.Info("init event publisher")
		err = event.InitEventPublisher(cfg)
		if err != nil {
			return fmt.Errorf("fail to initialize message queue, %w", err)
		}
		s3Internal := len(cfg.S3.InternalEndpoint) > 0
		slog.Info("init distributed locker")
		redisLocker := redis.InitDistributedLocker(cfg)

		slog.Info("init model inference deployer")
		err = deploy.Init(common.DeployConfig{
			ImageBuilderURL:         cfg.Space.BuilderEndpoint,
			ImageRunnerURL:          cfg.Space.RunnerEndpoint,
			MonitorInterval:         10 * time.Second,
			InternalRootDomain:      cfg.Space.InternalRootDomain,
			SpaceDeployTimeoutInMin: cfg.Space.DeployTimeoutInMin,
			ModelDeployTimeoutInMin: cfg.Model.DeployTimeoutInMin,
			ModelDownloadEndpoint:   cfg.Model.DownloadEndpoint,
			ChargingEnable:          cfg.Accounting.ChargingEnable,
			PublicRootDomain:        cfg.Space.PublicRootDomain,
			S3Internal:              s3Internal,
			RedisLocker:             redisLocker,
			UniqueServiceName:       cfg.UniqueServiceName,
			APIToken:                cfg.APIToken,
			APIKey:                  cfg.APIToken,
			HeartBeatTimeInSec:      cfg.Runner.HearBeatIntervalInSec,
		}, cfg, true)
		if err != nil {
			return fmt.Errorf("failed to init deploy: %w", err)
		}

		slog.Info("start temporal workflow")
		err = workflow.StartWorkflow(cfg, false)
		if err != nil {
			return err
		}

		// aitra-meter: start the out-of-band energy aggregator. It runs in this
		// (control-plane) process so its aitra_* metrics are scraped by the same
		// /metrics endpoint; it never touches the inference path. Off unless a
		// Prometheus address is configured.
		slog.Info("start energy aggregator")
		energyservice.Start(cmd.Context(), database.GetDB(), energyservice.Options{
			PrometheusAddr:      cfg.Prometheus.ApiAddress,
			PrometheusBasicAuth: cfg.Prometheus.BasicAuth,
			RunningStatus:       int(common.Running),
			InferenceTypes:      []int64{commontypes.InferenceType, commontypes.ServerlessType},
			Site:                energy.SiteParams{PUE: 1.0, CarbonSource: "manual"}, // TODO: wire from SiteConfig
			Window:              60 * time.Second,
			StorageBackend:      cfg.Energy.StorageBackend,
			StoragePath:         cfg.Energy.StoragePath,
		})

		slog.Info("start api server")
		router.RunServer(cfg, enableSwagger)
		return nil
	},
}

func serverExample() string {
	return `
# for development
aitra-server start server
`
}

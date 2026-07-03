package moderation

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/log"
	"github.com/aitra-ai/aitra-server/builder/instrumentation"
	"github.com/aitra-ai/aitra-server/builder/temporal"

	"github.com/spf13/cobra"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/moderation/checker"
	"github.com/aitra-ai/aitra-server/moderation/router"
)

var cmdLaunch = &cobra.Command{
	Use:     "launch",
	Short:   "Launch moderation server",
	Example: serverExample(),
	RunE: func(cmd *cobra.Command, args []string) (err error) {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}
		slog.Debug("config", slog.Any("data", cfg))
		stopOtel, err := instrumentation.SetupOTelSDK(context.Background(), cfg, instrumentation.Moderation)
		if err != nil {
			panic(err)
		}
		// Check APIToken length
		if len(cfg.APIToken) < 128 {
			return fmt.Errorf("API token length is less than 128, please check")
		}
		dbConfig := database.DBConfig{
			Dialect: database.DatabaseDialect(cfg.Database.Driver),
			DSN:     cfg.Database.DSN,
		}
		if err := database.InitDB(dbConfig); err != nil {
			slog.Error("failed to initialize database", slog.Any("error", err))
			return fmt.Errorf("database initialization failed: %w", err)
		}
		checker.Init(cfg)
		slog.Info("starting temporal client")
		temporalClient, err := temporal.NewClient(client.Options{
			HostPort: cfg.WorkFLow.Endpoint,
			Logger:   log.NewStructuredLogger(slog.Default()),
			ConnectionOptions: client.ConnectionOptions{
				GetSystemInfoTimeout: time.Duration(cfg.Temporal.GetSystemInfoTimeout) * time.Second,
			},
		}, instrumentation.Moderation)
		if err != nil {
			return fmt.Errorf("unable to create temporal client, error: %w", err)
		}

		r, err := router.NewRouter(cfg)
		if err != nil {
			return fmt.Errorf("failed to init router: %w", err)
		}
		slog.Info("moderation http server is running", slog.Any("port", cfg.Moderation.Port))
		server := httpbase.NewGracefulServer(
			httpbase.GraceServerOpt{
				Port: cfg.Moderation.Port,
			},
			r,
		)
		server.Run()

		_ = stopOtel(context.Background())
		temporalClient.Close()
		return nil
	},
}

func serverExample() string {
	return `
# for development
aitra-server moderation launch
`
}

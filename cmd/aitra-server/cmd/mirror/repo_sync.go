package mirror

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/api/workflow"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/builder/temporal"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/mirror"
	"github.com/aitra-ai/aitra-server/mirror/router"
)

var repoSyncCmd = &cobra.Command{
	Use:     "repo-sync",
	Short:   "Start the repoisotry sync server",
	Example: repoSyncExample(),
	RunE: func(*cobra.Command, []string) (err error) {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}

		dbConfig := database.DBConfig{
			Dialect: database.DatabaseDialect(cfg.Database.Driver),
			DSN:     cfg.Database.DSN,
		}
		if err := database.InitDB(dbConfig); err != nil {
			slog.Error("failed to initialize database", slog.Any("error", err))
			return fmt.Errorf("database initialization failed: %w", err)
		}

		r, err := router.NewRouter(cfg)
		if err != nil {
			return fmt.Errorf("failed to init router: %w", err)
		}
		slog.Info("http server is running", slog.Any("port", cfg.RepoSync.Port))
		server := httpbase.NewGracefulServer(
			httpbase.GraceServerOpt{
				Port: cfg.RepoSync.Port,
			},
			r,
		)
		go server.Run()

		// Exception recovery for mirrors.
		mirrorStore := database.NewMirrorStore()
		err = mirrorStore.Recover(context.Background())
		if err != nil {
			return fmt.Errorf("failed to recover mirrors: %w", err)
		}

		err = workflow.StartWorkflow(cfg, false)
		if err != nil {
			return err
		}

		repoSyncer, err := mirror.NewRepoSyncWorker(cfg, cfg.Mirror.WorkerNumber)
		if err != nil {
			return err
		}

		repoSyncer.Run()

		temporal.Stop()

		return nil
	},
}

func repoSyncExample() string {
	return `
# for development
aitra-server mirror repo-sync
`
}

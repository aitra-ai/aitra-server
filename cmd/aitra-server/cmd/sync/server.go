package sync

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/multisync/router"
)

var syncServerCmd = &cobra.Command{
	Use:     "sync-server",
	Short:   "Start the multi source sync server",
	Example: syncServerExample(),
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
		server := httpbase.NewGracefulServer(
			httpbase.GraceServerOpt{
				Port: cfg.Mirror.Port,
			},
			r,
		)
		server.Run()

		return nil
	},
}

func syncServerExample() string {
	return `
# for development
aitra-server sync sync-server
`
}

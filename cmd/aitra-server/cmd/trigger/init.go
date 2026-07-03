package trigger

import (
	"fmt"
	"log/slog"

	"github.com/spf13/cobra"
	"github.com/aitra-ai/aitra-server/builder/git"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
)

func init() {
	Cmd.AddCommand(
		gitCallbackCmd,
		fixOrgDataCmd,
		fixUserDataCmd,
		updateRepoCmd,
		fixRepoSourceCmd,
		migrateRepoPathCmd,
	)
	addCommands()
}

var Cmd = &cobra.Command{
	Use:   "trigger",
	Short: "trigger a specific command",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) (err error) {
		config, err := config.LoadConfig()
		if err != nil {
			return
		}

		dbConfig := database.DBConfig{
			Dialect: database.DatabaseDialect(config.Database.Driver),
			DSN:     config.Database.DSN,
		}

		if err := database.InitDB(dbConfig); err != nil {
			slog.Error("failed to initialize database", slog.Any("error", err))
			return fmt.Errorf("database initialization failed: %w", err)
		}
		rs = database.NewRepoStore()
		gs, err = git.NewGitServer(config)
		if err != nil {
			return
		}

		return
	},
	Run: func(cmd *cobra.Command, args []string) {
		_ = cmd.Help()
	},
}

package deploy

import (
	"github.com/spf13/cobra"
	"log/slog"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/logcollector/router"
)

var logCollectorCmd = &cobra.Command{
	Use:   "logcollector",
	Short: "start logcollector service",
	RunE: func(cmd *cobra.Command, args []string) error {
		cfg, err := config.LoadConfig()
		if err != nil {
			return err
		}
		s, logFactory, err := router.NewHttpServer(cmd.Context(), cfg)
		if err != nil {
			return err
		}

		slog.Info("deploy logcollector is running", slog.Any("port", cfg.LogCollector.Port))
		server := httpbase.NewGracefulServer(
			httpbase.GraceServerOpt{
				Port: cfg.LogCollector.Port,
			},
			s,
		)
		server.Run()
		logFactory.Stop()
		return nil
	},
}

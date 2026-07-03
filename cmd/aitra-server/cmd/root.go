package cmd

import (
	"fmt"
	"log/slog"
	"os"

	temporal_worker "github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/temporal-worker"
	"github.com/aitra-ai/aitra-server/common/log"

	"github.com/spf13/cobra"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/accounting"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/aigateway"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/cron"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/dataviewer"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/deploy"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/errorx"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/git"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/logscan"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/migration"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/mirror"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/moderation"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/notification"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/scaffold"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/start"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/sync"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/trigger"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/user"
	"github.com/aitra-ai/aitra-server/cmd/aitra-server/cmd/version"
	"github.com/aitra-ai/aitra-server/common/config"
)

var (
	logLevel   string
	logFormat  string
	configFile string
)

var RootCmd = &cobra.Command{
	Use:          "aitra-server",
	Short:        "Back-end API server for starhub.",
	SilenceUsage: true,
}

func init() {
	var err error
	defer func() {
		if err != nil {
			panic(err)
		}
	}()

	RootCmd.PersistentFlags().StringVarP(&logLevel, "log-level", "l", "info", "set log level to debug, info, warn, error or fatal (case-insensitive). default is INFO")
	RootCmd.PersistentFlags().StringVarP(&logFormat, "log-format", "f", "json", "set log format to json or text. default is json")
	RootCmd.PersistentFlags().StringVarP(&configFile, "config", "", "", "set config file path.")
	RootCmd.DisableAutoGenTag = true

	cobra.OnInitialize(func() {
		setupLog(logLevel, logFormat)
		config.SetConfigFile(configFile)
	})

	RootCmd.AddCommand(
		migration.Cmd,
		start.Cmd,
		logscan.Cmd,
		trigger.Cmd,
		deploy.Cmd,
		cron.Cmd,
		mirror.Cmd,
		accounting.Cmd,
		sync.Cmd,
		user.Cmd,
		git.Cmd,
		moderation.Cmd,
		dataviewer.Cmd,
		aigateway.Cmd,
		notification.Cmd,
		scaffold.Cmd,
		version.Cmd,
		errorx.Cmd,
		temporal_worker.Cmd,
	)

	addCommands()

}

func setupLog(lvl, format string) {
	logLevel := slog.LevelInfo.Level()
	var logger *slog.Logger
	if len(lvl) > 0 {
		err := logLevel.UnmarshalText([]byte(lvl))
		// logLevel not change if unmarshall failed
		if err != nil {
			fmt.Println("input invalid log level, use default log level INFO")
		}
	}
	// TODO:log source file position
	opt := &slog.HandlerOptions{AddSource: false, Level: logLevel}
	var handler slog.Handler
	switch format {
	case "json":
		handler = slog.NewJSONHandler(os.Stdout, opt)
	default:
		handler = slog.NewTextHandler(os.Stdout, opt)
	}
	// Wrap the default handler with the TraceIDHandler
	h := &log.ContextHandler{Handler: handler}

	fmt.Printf("init logger, level: %s, format: %s\n", logLevel.String(), format)
	logger = slog.New(h)
	slog.SetDefault(logger)
}

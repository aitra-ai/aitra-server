package factory

import (
	"log/slog"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	email "github.com/aitra-ai/aitra-server/notification/notifychannel/channel/email"
	emailclient "github.com/aitra-ai/aitra-server/notification/notifychannel/channel/email/client"
	internalmsg "github.com/aitra-ai/aitra-server/notification/notifychannel/channel/internalmsg"
)

const (
	ChannelNameInternalMessage = "internal-message"
	ChannelNameEmail           = "email"
)

// Register channels
func registerChannels(config *config.Config, factory Factory) {
	// internal message channel
	internalMessageChannel := internalmsg.NewChannel(config, database.NewNotificationStore())
	factory.RegisterChannel(ChannelNameInternalMessage, internalMessageChannel)

	// email channel
	registerEmailChannel(config, factory)

	extendChannels(config, factory)
}

func registerEmailChannel(config *config.Config, factory Factory) {
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

	emailChannel := email.NewChannel(config, emailService)
	factory.RegisterChannel(ChannelNameEmail, emailChannel)
}

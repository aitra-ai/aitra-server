//go:build !ee && !saas

package factory

import (
	"github.com/aitra-ai/aitra-server/common/config"
)

func extendChannels(_ *config.Config, _ Factory) {
}

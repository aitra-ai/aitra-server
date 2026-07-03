//go:build !ee && !saas

package workflow

import (
	"github.com/aitra-ai/aitra-server/builder/temporal"
	"github.com/aitra-ai/aitra-server/common/config"
)

func extendWorker(_ *config.Config, _ temporal.Client) {
}

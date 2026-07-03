package sensitive

import (
	"github.com/aitra-ai/aitra-server/builder/sensitive/internal"
	"github.com/aitra-ai/aitra-server/common/config"
)

func LoadFromDB() internal.Loader {
	return internal.FromDatabase()
}

func LoadFromConfig(cfg *config.Config) internal.Loader {
	return internal.NewConfigLoader(cfg)
}

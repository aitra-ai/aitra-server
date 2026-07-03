package mirror

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/mirror/lfssyncer"
)

type LFSSyncWorker interface {
	SetContext(ctx context.Context)
	Run(mt *database.MirrorTask)
}

func NewLFSSyncWorker(config *config.Config, id int) (LFSSyncWorker, error) {
	return lfssyncer.NewLfsSyncWorker(config, id)

}

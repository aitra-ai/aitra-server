package mirror

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/mirror/reposyncer"
)

type RepoSyncWorker interface {
	Run()
	SyncRepo(ctx context.Context, mirror *database.Mirror, mt *database.MirrorTask) (*database.MirrorTask, error)
}

func NewRepoSyncWorker(config *config.Config, numWorkers int) (RepoSyncWorker, error) {
	return reposyncer.NewRepoSyncWorker(config, numWorkers)
}

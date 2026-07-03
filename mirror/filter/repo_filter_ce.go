//go:build !ee && !saas

package filter

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
)

type RepoFilter struct {
	repoStore   database.RepoStore
	mirrorStore database.MirrorStore
	cfg         *config.Config
}

func NewRepoFilter(cfg *config.Config) Filter {
	return &RepoFilter{
		repoStore:   database.NewRepoStore(),
		mirrorStore: database.NewMirrorStore(),
		cfg:         cfg,
	}
}

func (rf *RepoFilter) ShouldSync(ctx context.Context, repoID int64) (bool, string, error) {
	return true, "", nil
}

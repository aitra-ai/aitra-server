package filter

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

type RepoArgs interface {
	isRepoArgs()
}

type SyncVersionFilterArgs struct {
	RepoType  types.RepositoryType
	Namespace string
	Name      string
}

func (a SyncVersionFilterArgs) isRepoArgs() {}

type RepoFilter interface {
	Match(ctx context.Context, repoArgs RepoArgs) (bool, error)
	BatchMatch(ctx context.Context, repos []database.Repository) (modelsMatched []string, datasetsMatched []string, err error)
}

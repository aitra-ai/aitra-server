package callback

import (
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/filter"
	"github.com/aitra-ai/aitra-server/common/types"
)

type SyncVersionGenerator interface {
	GenSyncVersion(req *types.GiteaCallbackPushReq) error
}

type syncVersionGeneratorImpl struct {
	multiSyncStore database.MultiSyncStore
	ruleStore      database.RuleStore
	repoStore      database.RepoStore
	repoFilter     filter.RepoFilter
}

func NewSyncVersionGenerator() *syncVersionGeneratorImpl {
	return &syncVersionGeneratorImpl{
		multiSyncStore: database.NewMultiSyncStore(),
		ruleStore:      database.NewRuleStore(),
		repoStore:      database.NewRepoStore(),
		repoFilter:     filter.NewRepoFilter(),
	}
}

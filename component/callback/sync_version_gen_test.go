//go:build saas

package callback

import (
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	filterMock "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/common/filter"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/filter"
	"github.com/aitra-ai/aitra-server/common/tests"
	"github.com/aitra-ai/aitra-server/common/types"
)

func TestSyncVersionGenerator_Gen(t *testing.T) {
	mockStores := tests.NewMockStores(t)
	mockFilter := &filterMock.MockRepoFilter{}
	g := &syncVersionGeneratorImpl{
		multiSyncStore: mockStores.MultiSync,
		repoStore:      mockStores.Repo,
		ruleStore:      mockStores.RuleStore,
		repoFilter:     mockFilter,
	}

	mockStores.MultiSyncMock().EXPECT().Create(mock.Anything, database.SyncVersion{
		SourceID:  types.SyncVersionSourceOpenCSG,
		ChangeLog: "foo",
		RepoPath:  "ns/n",
		RepoType:  types.ModelRepo,
	}).Return(nil, nil)

	mockFilter.EXPECT().Match(mock.Anything, filter.SyncVersionFilterArgs{
		Namespace: "ns",
		Name:      "n",
		RepoType:  types.ModelRepo,
	}).Return(true, nil)

	err := g.GenSyncVersion(&types.GiteaCallbackPushReq{
		Repository: types.GiteaCallbackPushReq_Repository{
			FullName: "models_ns/n",
		},
		HeadCommit: types.GiteaCallbackPushReq_HeadCommit{
			Message: "foo",
		},
	})
	require.Nil(t, err)
}

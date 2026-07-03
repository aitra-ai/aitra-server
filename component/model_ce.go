//go:build !ee && !saas

package component

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

func (c *modelComponentImpl) getXnetMigrationProgress(ctx context.Context, repo *database.Repository) int {
	return 0
}

func (c *modelComponentImpl) addOpWeightToModel(ctx context.Context, repoIDs []int64, resModels []*types.Model) {
}

func modelRunUpdateDeployRepo(dp types.DeployRepo, req types.ModelRunReq) types.DeployRepo {
	return dp
}

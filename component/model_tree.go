package component

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

type ModelTreeComponent interface {
	GetModelTree(ctx context.Context, currentUser, namespace, name string) (*types.ModelTree, error)
	ProcessModelTree(ctx context.Context, relations []*types.ModelNode, currentRepo database.Repository)
	ScanModelTree(ctx context.Context, req types.ScanModels) error
}

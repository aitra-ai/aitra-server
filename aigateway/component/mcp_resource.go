package component

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
)

type MCPResourceComponent interface {
	List(ctx context.Context, filter *types.MCPFilter) ([]database.MCPResource, int, error)
}

type mcpResourceComponentImpl struct {
	mcpResStore database.MCPResourceStore
}

func NewMCPResourceComponent(config *config.Config) MCPResourceComponent {
	return &mcpResourceComponentImpl{
		mcpResStore: database.NewMCPResourceStore(),
	}
}

func (c *mcpResourceComponentImpl) List(ctx context.Context, filter *types.MCPFilter) ([]database.MCPResource, int, error) {
	return c.mcpResStore.List(ctx, filter)
}

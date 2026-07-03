//go:build !saas && !ee

package component

import (
	"context"

	"github.com/aitra-ai/aitra-server/common/types"
)

func (m *mcpServerComponentImpl) addOpWeightToMCPs(ctx context.Context, repoIDs []int64, res []*types.MCPServer) {
}

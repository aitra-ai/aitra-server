//go:build !saas && !ee

package imagerunner

import (
	"context"
	"github.com/aitra-ai/aitra-server/common/types"
)

func (h *LocalRunner) LabelNode(ctx context.Context, req *types.NodeLabel) error {
	return nil
}

//go:build !saas && !ee

package component

import (
	"context"
	"errors"

	"github.com/aitra-ai/aitra-server/common/types"
)

func (c *clusterComponentImpl) LabelNode(ctx context.Context, req *types.NodeLabel) error {
	return errors.New("LabelNode is not supported in CE version")
}

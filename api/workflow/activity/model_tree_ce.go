//go:build !ee && !saas

package activity

import (
	"context"

	"github.com/aitra-ai/aitra-server/common/types"
)

func (a *Activities) UpdateModelTree(ctx context.Context, req *types.GiteaCallbackPushReq) error {
	return nil
}

func (a *Activities) ScanModelTree(ctx context.Context, req types.ScanModels) error {
	return nil
}

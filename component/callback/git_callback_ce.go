//go:build !saas

package callback

import (
	"context"

	"github.com/aitra-ai/aitra-server/common/types"
)

func (c *gitCallbackComponentImpl) updateDescriptionFromReadme(ctx context.Context, repoType, namespace, repoName, ref string) error {
	return nil
}

func (c *gitCallbackComponentImpl) MCPScan(ctx context.Context, req *types.GiteaCallbackPushReq) error {
	return nil
}

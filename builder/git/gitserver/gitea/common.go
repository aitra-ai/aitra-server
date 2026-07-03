package gitea

import (
	"context"

	"github.com/aitra-ai/aitra-server/common/types"
)

func (c *Client) BuildRelativePath(ctx context.Context, repoType types.RepositoryType, namespace, name string) (string, error) {
	return "", nil
}

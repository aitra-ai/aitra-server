package gitea

import (
	"context"

	"github.com/OpenCSGs/gitea-go-sdk/gitea"
	"github.com/aitra-ai/aitra-server/builder/git/gitserver"
	"github.com/aitra-ai/aitra-server/common/types"
	"github.com/aitra-ai/aitra-server/common/utils/common"
)

func (c *Client) GetRepoTags(ctx context.Context, req gitserver.GetRepoTagsReq) (tags []*types.Tag, err error) {
	namespace := common.WithPrefix(req.Namespace, repoPrefixByType(req.RepoType))
	giteaTags, _, err := c.giteaClient.ListRepoTags(
		namespace,
		req.Name,
		gitea.ListRepoTagsOptions{
			ListOptions: gitea.ListOptions{
				PageSize: req.Per,
				Page:     req.Page,
			},
		},
	)
	if err != nil {
		return
	}
	for _, giteaTag := range giteaTags {
		tag := &types.Tag{
			Name:    giteaTag.Name,
			Message: giteaTag.Message,
			Commit: types.DatasetTagCommit{
				ID: giteaTag.Commit.SHA,
			},
		}
		tags = append(tags, tag)
	}
	return
}

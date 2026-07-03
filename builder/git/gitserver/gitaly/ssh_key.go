package gitaly

import (
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

func (c *Client) CreateSSHKey(req *types.CreateSSHKeyRequest) (token *database.SSHKey, err error) {
	return
}

//List all SSH keys from gitea

// func (c *Client) ListSSHKeys(username string, per, page int) (tokens []*database.SSHKey, err error) {
// 	giteaSSHKeys, _, err := c.giteaClient.ListPublicKeys(
// 		username,
// 		gitea.ListPublicKeysOptions{
// 			ListOptions: gitea.ListOptions{
// 				Page:     page,
// 				PageSize: per,
// 			},
// 		},
// 	)

// 	if err != nil {
// 		return
// 	}

// 	for _, giteaSSHKey := range giteaSSHKeys {
// 		tokens = append(tokens, &database.SSHKey{
// 			GID:     int(giteaSSHKey.ID),
// 			Name:    giteaSSHKey.Title,
// 			Content: giteaSSHKey.Key,
// 		})
// 	}

// 	return
// }

func (c *Client) DeleteSSHKey(id int) (err error) {
	return
}

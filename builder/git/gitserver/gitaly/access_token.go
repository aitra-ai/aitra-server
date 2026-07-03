package gitaly

import (
	"strings"

	"github.com/google/uuid"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

func (c *Client) CreateUserToken(req *types.CreateUserTokenRequest) (token *database.AccessToken, err error) {
	token = &database.AccessToken{
		Name:        req.TokenName,
		Permission:  req.Permission,
		Application: req.Application,
		ExpiredAt:   req.ExpiredAt,
		Token:       strings.ReplaceAll(uuid.NewString(), "-", ""),
	}
	return
}

func (c *Client) DeleteUserToken(req *types.DeleteUserTokenRequest) (err error) {
	return
}

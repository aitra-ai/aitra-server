package git

import (
	"github.com/aitra-ai/aitra-server/builder/git/membership"
	"github.com/aitra-ai/aitra-server/builder/git/membership/gitea"
	"github.com/aitra-ai/aitra-server/common/config"
)

func NewMemberShip(config config.Config) (membership.GitMemerShip, error) {
	c, err := gitea.NewClient(&config)
	return c, err
}

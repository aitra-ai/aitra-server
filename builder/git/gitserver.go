package git

import (
	"github.com/aitra-ai/aitra-server/builder/git/gitserver"
	"github.com/aitra-ai/aitra-server/builder/git/gitserver/gitaly"
	"github.com/aitra-ai/aitra-server/common/config"
)

func NewGitServer(config *config.Config) (gitserver.GitServer, error) {
	gitServer, err := gitaly.NewClient(config)
	return gitServer, err
}

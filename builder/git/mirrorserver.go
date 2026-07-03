package git

import (
	"errors"

	"github.com/aitra-ai/aitra-server/builder/git/mirrorserver"
	"github.com/aitra-ai/aitra-server/builder/git/mirrorserver/gitea"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
)

func NewMirrorServer(config *config.Config) (mirrorserver.MirrorServer, error) {
	if !config.MirrorServer.Enable {
		return nil, nil
	}
	if config.MirrorServer.Type == types.GitServerTypeGitea {
		mirrorServer, err := gitea.NewMirrorClient(config)
		return mirrorServer, err
	}

	//TODO: implement gitaly based mirrorserver

	return nil, errors.New("undefined mirror server type")
}

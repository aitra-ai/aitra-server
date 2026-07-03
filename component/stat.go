package component

import (
	"context"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
)

type StatComponent interface {
	GetStatSnap(ctx context.Context, req types.StatSnapshotReq) (*types.StatSnapshotResp, error)
	MakeStatSnap(ctx context.Context) error
	StatRunningDeploys(ctx context.Context) (map[int]*types.StatRunningDeploy, error)
}

func NewStatComponent(config *config.Config) (StatComponent, error) {
	return &statComponentImpl{
		config:          config,
		statSnapStore:   database.NewStatSnapStore(),
		deployTaskStore: database.NewDeployTaskStore(),
	}, nil
}

type statComponentImpl struct {
	config          *config.Config
	statSnapStore   database.StatSnapStore
	deployTaskStore database.DeployTaskStore
}

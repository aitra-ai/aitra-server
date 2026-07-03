//go:build !ee && !saas

package component

import (
	"github.com/aitra-ai/aitra-server/builder/deploy"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
)

func NewSpaceResourceComponent(config *config.Config) (SpaceResourceComponent, error) {
	c := &spaceResourceComponentImpl{}
	c.spaceResourceStore = database.NewSpaceResourceStore()
	c.deployer = deploy.NewDeployer()
	c.userStore = database.NewUserStore()
	ac, err := NewAccountingComponent(config)
	if err != nil {
		return nil, err
	}
	c.accountComponent = ac
	c.config = config
	return c, nil
}

type spaceResourceComponentImpl struct {
	spaceResourceStore database.SpaceResourceStore
	deployer           deploy.Deployer
	userStore          database.UserStore
	accountComponent   AccountingComponent
	config             *config.Config
}

func (c *spaceResourceComponentImpl) updatePriceInfo(req *types.SpaceResourceIndexReq, resources []types.SpaceResource) error {
	return nil

}

// func (c *spaceResourceComponentImpl) appendUserResources(ctx context.Context, currentUser string, clusterID string, resources []types.SpaceResource) ([]types.SpaceResource, error) {
// 	return resources, nil
// }

func (c *spaceResourceComponentImpl) deployAvailable(deployType int, hardware types.HardWare) bool {
	if deployType == types.FinetuneType {
		if hardware.Gpu.Num == "" {
			return false
		}
	}
	return true
}

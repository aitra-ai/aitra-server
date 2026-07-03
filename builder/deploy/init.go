package deploy

import (
	"context"
	"fmt"

	"github.com/aitra-ai/aitra-server/common/types"
	"github.com/aitra-ai/aitra-server/component/reporter"

	"github.com/aitra-ai/aitra-server/builder/deploy/common"
	"github.com/aitra-ai/aitra-server/builder/deploy/imagebuilder"
	"github.com/aitra-ai/aitra-server/builder/deploy/imagerunner"
	"github.com/aitra-ai/aitra-server/common/config"
)

var (
	defaultDeployer Deployer
)

func Init(c common.DeployConfig, config *config.Config, startJobs bool) error {
	// ib := imagebuilder.NewLocalBuilder()
	ib, err := imagebuilder.NewRemoteBuilder(c.ImageBuilderURL, c)
	if err != nil {
		panic(fmt.Errorf("failed to create image builder:%w", err))
	}
	ir, err := imagerunner.NewRemoteRunner(c.ImageRunnerURL, c)
	if err != nil {
		panic(fmt.Errorf("failed to create image runner:%w", err))
	}

	logReporter, err := reporter.NewAndStartLogCollector(context.TODO(), config, types.ClientTypeCSGHUB)
	if err != nil {
		return fmt.Errorf("failed to create log reporter:%w", err)
	}

	deployer, err := newDeployer(ib, ir, c, logReporter, config, startJobs)
	if err != nil {
		return fmt.Errorf("failed to create deployer:%w", err)
	}

	deployer.internalRootDomain = c.InternalRootDomain
	defaultDeployer = deployer
	return nil
}

func NewDeployer() Deployer {
	return defaultDeployer
}

package component

import (
	"go.opentelemetry.io/otel"
	mock_accounting "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/accounting"
	mock_deploy "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/deploy"
	mock_git "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/git/gitserver"
	mock_mirror "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/git/mirrorserver"
	mock_importer "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/importer"
	mock_preader "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/parquet"
	mock_rpc "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/rpc"
	mock_rsa "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/rsa"
	mock_s3 "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/store/s3"

	mock_component "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/component"
	mock_cache "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/mirror/cache"
	"github.com/aitra-ai/aitra-server/builder/git/gitserver"
	"github.com/aitra-ai/aitra-server/builder/parquet"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/tests"
	hubCom "github.com/aitra-ai/aitra-server/component"
)

type mockedComponents struct {
	accounting          *mock_component.MockAccountingComponent
	repo                *mock_component.MockRepoComponent
	tag                 *mock_component.MockTagComponent
	space               *mock_component.MockSpaceComponent
	runtimeArchitecture *mock_component.MockRuntimeArchitectureComponent
	sensitive           *mock_component.MockSensitiveComponent
}

type Mocks struct {
	stores            *tests.MockStores
	components        *mockedComponents
	gitServer         *mock_git.MockGitServer
	userSvcClient     *mock_rpc.MockUserSvcClient
	s3Client          *mock_s3.MockClient
	mirrorServer      *mock_mirror.MockMirrorServer
	deployer          *mock_deploy.MockDeployer
	cache             *mock_cache.MockCache
	accountingClient  *mock_accounting.MockAccountingClient
	preader           *mock_preader.MockReader
	limitOffsetReader *mock_preader.MockLimitOffsetCountReader
	moderationClient  *mock_rpc.MockModerationSvcClient
	rsaReader         *mock_rsa.MockKeysReader
	importer          *mock_importer.MockImporter
}

func ProvideTestConfig() *config.Config {
	return &config.Config{}
}

func NewTestDatasetViewerComponent(stores *tests.MockStores, cfg *config.Config, repoComponent hubCom.RepoComponent, gitServer gitserver.GitServer, preader parquet.Reader, limitOffsetReader parquet.LimitOffsetCountReader) *datasetViewerComponentImpl {
	return &datasetViewerComponentImpl{
		cfg:                    cfg,
		repoStore:              stores.Repo,
		repoComponent:          repoComponent,
		gitServer:              gitServer,
		preader:                preader,
		limitOffsetCountReader: limitOffsetReader,
		viewerStore:            stores.ViewerStore,
		tracer:                 otel.Tracer("dataset-viewer"),
	}
}

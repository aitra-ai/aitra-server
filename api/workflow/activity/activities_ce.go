//go:build !ee && !saas

package activity

import (
	"github.com/aitra-ai/aitra-server/builder/git/gitserver"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/component"
	"github.com/aitra-ai/aitra-server/component/callback"
)

type stores struct {
	syncClientSetting database.SyncClientSettingStore
}

type Activities struct {
	config        *config.Config
	callback      callback.GitCallbackComponent
	recom         component.RecomComponent
	gitServer     gitserver.GitServer
	multisync     component.MultiSyncComponent
	rftScanner    component.RuntimeArchitectureComponent
	repoComponent component.RepoComponent
	stores        stores
}

func NewActivities(
	cfg *config.Config,
	callback callback.GitCallbackComponent,
	recom component.RecomComponent,
	gitServer gitserver.GitServer,
	multisync component.MultiSyncComponent,
	syncClientSetting database.SyncClientSettingStore,
	rftScanner component.RuntimeArchitectureComponent,
	repoComponent component.RepoComponent,
) *Activities {
	stores := stores{
		syncClientSetting: syncClientSetting,
	}

	return &Activities{
		config:        cfg,
		callback:      callback,
		recom:         recom,
		gitServer:     gitServer,
		multisync:     multisync,
		stores:        stores,
		rftScanner:    rftScanner,
		repoComponent: repoComponent,
	}
}

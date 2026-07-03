//go:build !ee && !saas

package callback

import (
	"github.com/aitra-ai/aitra-server/common/types"
)

func (g *syncVersionGeneratorImpl) GenSyncVersion(req *types.GiteaCallbackPushReq) error {
	return nil
}

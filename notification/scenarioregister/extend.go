//go:build !ee && !saas

package scenarioregister

import (
	"github.com/aitra-ai/aitra-server/notification/scenariomgr"
)

func extend(_ *scenariomgr.DataProvider) {
}

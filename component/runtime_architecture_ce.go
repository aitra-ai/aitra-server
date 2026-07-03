//go:build !ee && !saas

package component

import (
	"strings"

	"github.com/aitra-ai/aitra-server/builder/store/database"
)

func checkTagName(rf *database.RuntimeFramework, tag string) bool {
	return strings.Contains(rf.FrameImage, tag)
}

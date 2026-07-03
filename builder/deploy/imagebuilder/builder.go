package imagebuilder

import (
	"context"

	"github.com/aitra-ai/aitra-server/common/types"
)

type Builder interface {
	Build(context.Context, *types.ImageBuilderRequest) error
	Stop(context.Context, types.ImageBuildStopReq) error
}

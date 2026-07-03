package imagebuilder

import (
	"context"

	"github.com/aitra-ai/aitra-server/common/types"
)

var _ Builder = (*LocalBuilder)(nil)

type LocalBuilder struct{}

func NewLocalBuilder() *LocalBuilder {
	return &LocalBuilder{}
}

// Build implements Builder.Build
func (*LocalBuilder) Build(ctx context.Context, req *types.ImageBuilderRequest) error {

	return nil
}

// Stop implements Builder.Stop
func (*LocalBuilder) Stop(ctx context.Context, req types.ImageBuildStopReq) error {
	// Simulate stopping the build process
	return nil
}

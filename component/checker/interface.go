package checker

import (
	"context"

	"github.com/aitra-ai/aitra-server/common/types"
)

type GitCallbackChecker interface {
	// Check checks the given value and returns an error if the value is invalid.
	Check(ctx context.Context, req types.GitalyAllowedReq) (bool, error)
}

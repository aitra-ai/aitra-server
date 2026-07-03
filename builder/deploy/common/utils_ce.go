//go:build !saas && !ee

package common

import "github.com/aitra-ai/aitra-server/common/types"

func GenerateScheduler(config DeployConfig) *types.Scheduler {
	return nil
}

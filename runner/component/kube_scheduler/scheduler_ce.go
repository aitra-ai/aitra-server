//go:build !saas && !ee

package kube_scheduler

import "github.com/aitra-ai/aitra-server/common/types"

func NewApplier(config *types.Scheduler) Applier {
	return &DefaultOpApplier{}
}

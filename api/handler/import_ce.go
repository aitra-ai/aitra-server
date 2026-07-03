//go:build !ee && !saas

package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
)

func (h *importHandlerImpl) Import(ctx *gin.Context) {
	httpbase.OK(ctx, nil)
}

func (h *importHandlerImpl) GetGitlabRepos(ctx *gin.Context) {
	httpbase.OK(ctx, nil)
}

func (h *importHandlerImpl) ImportStatus(ctx *gin.Context) {
	httpbase.OK(ctx, nil)
}

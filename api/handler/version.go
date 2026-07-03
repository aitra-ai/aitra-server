package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/version"
)

type VersionHandler struct {
}

func NewVersionHandler() *VersionHandler {
	return &VersionHandler{}
}

func (h *VersionHandler) Version(ctx *gin.Context) {
	httpbase.OK(ctx, gin.H{
		"version": version.StarhubAPIVersion,
		"commit":  version.GitRevision,
	})
}

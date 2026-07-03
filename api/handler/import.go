package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/component"
)

type ImportHandler interface {
	Import(c *gin.Context)
	GetGitlabRepos(ctx *gin.Context)
	ImportStatus(ctx *gin.Context)
}

type importHandlerImpl struct {
	c component.ImportComponent
}

func NewImportHandler(config *config.Config) (ImportHandler, error) {
	c, err := component.NewImportComponent(config)
	if err != nil {
		return nil, err
	}
	return &importHandlerImpl{
		c: c,
	}, nil
}

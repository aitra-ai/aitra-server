package handler

import (
	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/user/component"
)

type NamespaceHandler struct {
	c component.NamespaceComponent
}

func NewNamespaceHandler(config *config.Config) (*NamespaceHandler, error) {
	nc, err := component.NewNamespaceComponent(config)
	if err != nil {
		return nil, err
	}
	return &NamespaceHandler{
		c: nc,
	}, nil
}

// GetInfo godoc
// @Security     ApiKey
// @Summary      Get namespace info [Internal Only].
// @Description  get namespace info
// @Tags         Namespace, InternalOnly
// @Accept       json
// @Produce      json
// @Param        path path string true "namespace"
// @Success      200 {object} types.Response{data=types.Namespace} "OK"
// @Failure      400  {object}  types.APIBadRequest "Bad request"
// @Failure      500  {object}  types.APIInternalServerError "Internal server error"
// @Router       /namespace/{path} [get]
func (h *NamespaceHandler) GetInfo(ctx *gin.Context) {
	path := ctx.Param("path")
	ns, err := h.c.GetInfo(ctx, path)
	if err != nil {
		httpbase.ServerError(ctx, err)
		return
	}
	httpbase.OK(ctx, ns)
}

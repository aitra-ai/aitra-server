package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/common/config"
	"opencsg.com/csghub-server/common/types"
)

type LLMConfigHandler struct {
	store  database.LLMConfigStore
	config *config.Config
}

func NewLLMConfigHandler(cfg *config.Config) (*LLMConfigHandler, error) {
	return &LLMConfigHandler{
		store:  database.NewLLMConfigStore(cfg),
		config: cfg,
	}, nil
}

// ListExternalLLMConfigs lists all LLM configs with type=16 (aigateway external)
// GET /api/v1/admin/llm_configs
func (h *LLMConfigHandler) ListExternalLLMConfigs(c *gin.Context) {
	llmType := database.LLMTypeAigatewayExternal
	search := &types.SearchLLMConfig{
		Type: &llmType,
	}
	configs, _, err := h.store.Index(c.Request.Context(), 100, 1, search)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list llm configs: %w", err))
		return
	}
	httpbase.OK(c, configs)
}

// UpdateExternalLLMConfig updates auth_header and enabled for an LLM config
// PUT /api/v1/admin/llm_configs/:id
func (h *LLMConfigHandler) UpdateExternalLLMConfig(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid id: "+idStr)
		return
	}

	var req types.UpdateLLMConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequest(c, "invalid request body: "+err.Error())
		return
	}

	// Fetch existing record
	existing, err := h.store.GetByID(c.Request.Context(), id)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to get llm config %d: %w", id, err))
		return
	}

	// Apply partial updates
	if req.AuthHeader != nil {
		existing.AuthHeader = *req.AuthHeader
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.ModelName != nil {
		existing.ModelName = *req.ModelName
	}
	if req.ApiEndpoint != nil {
		existing.ApiEndpoint = *req.ApiEndpoint
	}
	if req.Provider != nil {
		existing.Provider = *req.Provider
	}

	updated, err := h.store.Update(c.Request.Context(), *existing)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to update llm config %d: %w", id, err))
		return
	}
	httpbase.OK(c, updated)
}

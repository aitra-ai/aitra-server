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

// ListPublicLLMConfigs returns enabled LLM configs (type=16) without auth_header — no login required.
// GET /api/v1/public/llm_configs
func (h *LLMConfigHandler) ListPublicLLMConfigs(c *gin.Context) {
	llmType := database.LLMTypeAigatewayExternal
	search := &types.SearchLLMConfig{
		Type: &llmType,
	}
	configs, _, err := h.store.Index(c.Request.Context(), 100, 1, search)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list public llm configs: %w", err))
		return
	}

	public := make([]types.PublicLLMConfig, 0, len(configs))
	for _, cfg := range configs {
		if !cfg.Enabled {
			continue
		}
		public = append(public, types.PublicLLMConfig{
			ID:          cfg.ID,
			ModelName:   cfg.ModelName,
			ApiEndpoint: cfg.ApiEndpoint,
			Provider:    cfg.Provider,
			Enabled:     cfg.Enabled,
			CreatedAt:   cfg.CreatedAt,
			UpdatedAt:   cfg.UpdatedAt,
		})
	}
	httpbase.OK(c, public)
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

// CreateExternalLLMConfig creates a new LLM config with type=16 (aigateway external)
// POST /api/v1/admin/llm_configs
func (h *LLMConfigHandler) CreateExternalLLMConfig(c *gin.Context) {
	var req types.CreateLLMConfigReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	if req.ModelName == "" || req.ApiEndpoint == "" {
		httpbase.BadRequest(c, "model_name and api_endpoint are required")
		return
	}

	cfg := database.LLMConfig{
		ModelName:   req.ModelName,
		ApiEndpoint: req.ApiEndpoint,
		AuthHeader:  req.AuthHeader,
		Provider:    req.Provider,
		Type:        database.LLMTypeAigatewayExternal,
		Enabled:     req.Enabled,
	}
	created, err := h.store.Create(c.Request.Context(), cfg)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to create llm config: %w", err))
		return
	}
	httpbase.OK(c, created)
}

// DeleteExternalLLMConfig deletes a LLM config by id
// DELETE /api/v1/admin/llm_configs/:id
func (h *LLMConfigHandler) DeleteExternalLLMConfig(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid id: "+idStr)
		return
	}
	if err := h.store.Delete(c.Request.Context(), id); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to delete llm config %d: %w", id, err))
		return
	}
	httpbase.OK(c, nil)
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

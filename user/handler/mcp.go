package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/common/types"
	"opencsg.com/csghub-server/common/utils/common"
)

type MCPHandler struct {
	store database.MCPResourceStore
}

func NewMCPHandler() *MCPHandler {
	return &MCPHandler{
		store: database.NewMCPResourceStore(),
	}
}

// ListMCPResources lists all MCP resources with pagination.
// GET /api/v1/admin/mcp
func (h *MCPHandler) ListMCPResources(c *gin.Context) {
	per, page, err := common.GetPerAndPageFromContext(c)
	if err != nil {
		httpbase.BadRequest(c, err.Error())
		return
	}
	filter := &types.MCPFilter{
		Per:  per,
		Page: page,
	}
	resources, total, err := h.store.List(c.Request.Context(), filter)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list mcp resources: %w", err))
		return
	}
	httpbase.OK(c, gin.H{
		"data":  resources,
		"total": total,
	})
}

// CreateMCPResource creates a new MCP resource.
// POST /api/v1/admin/mcp
func (h *MCPHandler) CreateMCPResource(c *gin.Context) {
	var input database.MCPResource
	if err := c.ShouldBindJSON(&input); err != nil {
		httpbase.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	if input.Name == "" || input.Url == "" {
		httpbase.BadRequest(c, "name and url are required")
		return
	}
	if input.Protocol == "" {
		input.Protocol = "sse"
	}
	result, err := h.store.Create(c.Request.Context(), &input)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to create mcp resource: %w", err))
		return
	}
	httpbase.OK(c, result)
}

// UpdateMCPResource updates an existing MCP resource.
// PUT /api/v1/admin/mcp/:id
func (h *MCPHandler) UpdateMCPResource(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid id")
		return
	}
	var req struct {
		Name        *string         `json:"name"`
		Description *string         `json:"description"`
		Url         *string         `json:"url"`
		Protocol    *string         `json:"protocol"`
		Owner       *string         `json:"owner"`
		Headers     *map[string]any `json:"headers"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	// Fetch existing record, then overlay provided fields
	existing, err := h.store.FindByID(c.Request.Context(), id)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("mcp resource not found: %w", err))
		return
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Url != nil {
		existing.Url = *req.Url
	}
	if req.Protocol != nil {
		existing.Protocol = *req.Protocol
	}
	if req.Owner != nil {
		existing.Owner = *req.Owner
	}
	if req.Headers != nil {
		existing.Headers = *req.Headers
	}
	result, err := h.store.Update(c.Request.Context(), existing)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to update mcp resource: %w", err))
		return
	}
	httpbase.OK(c, result)
}

// DeleteMCPResource deletes an MCP resource.
// DELETE /api/v1/admin/mcp/:id
func (h *MCPHandler) DeleteMCPResource(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid id")
		return
	}
	if err := h.store.Delete(c.Request.Context(), &database.MCPResource{ID: id}); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to delete mcp resource: %w", err))
		return
	}
	httpbase.OK(c, nil)
}

package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/store/database"
)

type AISkillHandler struct {
	store     database.AISkillStore
	userStore database.UserStore
}

func NewAISkillHandler() *AISkillHandler {
	return &AISkillHandler{
		store:     database.NewAISkillStore(),
		userStore: database.NewUserStore(),
	}
}

// ListPublicSkills returns all enabled skills (no auth required).
// GET /api/v1/public/skills
func (h *AISkillHandler) ListPublicSkills(c *gin.Context) {
	skills, err := h.store.List(c.Request.Context(), true)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list skills: %w", err))
		return
	}
	httpbase.OK(c, skills)
}

// AdminListSkills returns all skills including disabled (admin only).
// GET /api/v1/admin/skills
func (h *AISkillHandler) AdminListSkills(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	skills, err := h.store.List(c.Request.Context(), false)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list skills: %w", err))
		return
	}
	httpbase.OK(c, skills)
}

// AdminCreateSkill creates a new skill.
// POST /api/v1/admin/skills
func (h *AISkillHandler) AdminCreateSkill(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var skill database.AISkill
	if err := c.ShouldBindJSON(&skill); err != nil {
		httpbase.BadRequest(c, "invalid request body")
		return
	}
	if skill.Name == "" || skill.SystemPrompt == "" {
		httpbase.BadRequest(c, "name and system_prompt are required")
		return
	}
	if err := h.store.Create(c.Request.Context(), &skill); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to create skill: %w", err))
		return
	}
	httpbase.OK(c, skill)
}

// AdminUpdateSkill updates an existing skill.
// PUT /api/v1/admin/skills/:id
func (h *AISkillHandler) AdminUpdateSkill(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid skill ID")
		return
	}
	existing, err := h.store.FindByID(c.Request.Context(), id)
	if err != nil {
		httpbase.NotFoundError(c, fmt.Errorf("skill not found"))
		return
	}

	var body struct {
		Name           *string `json:"name"`
		Description    *string `json:"description"`
		SystemPrompt   *string `json:"system_prompt"`
		PreferredModel *string `json:"preferred_model"`
		Icon           *string `json:"icon"`
		Enabled        *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		httpbase.BadRequest(c, "invalid request body")
		return
	}

	if body.Name != nil {
		existing.Name = *body.Name
	}
	if body.Description != nil {
		existing.Description = *body.Description
	}
	if body.SystemPrompt != nil {
		existing.SystemPrompt = *body.SystemPrompt
	}
	if body.PreferredModel != nil {
		existing.PreferredModel = *body.PreferredModel
	}
	if body.Icon != nil {
		existing.Icon = *body.Icon
	}
	if body.Enabled != nil {
		existing.Enabled = *body.Enabled
	}

	if err := h.store.Update(c.Request.Context(), existing); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to update skill: %w", err))
		return
	}
	httpbase.OK(c, existing)
}

// AdminDeleteSkill deletes a skill.
// DELETE /api/v1/admin/skills/:id
func (h *AISkillHandler) AdminDeleteSkill(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid skill ID")
		return
	}
	if err := h.store.Delete(c.Request.Context(), id); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to delete skill: %w", err))
		return
	}
	httpbase.OK(c, gin.H{"message": "skill deleted"})
}

func (h *AISkillHandler) requireAdmin(c *gin.Context) bool {
	username := httpbase.GetCurrentUser(c)
	if username == "" {
		httpbase.UnauthorizedError(c, fmt.Errorf("not logged in"))
		return false
	}
	u, err := h.userStore.FindByUsername(c.Request.Context(), username)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find user: %w", err))
		return false
	}
	if !u.CanAdmin() {
		httpbase.UnauthorizedError(c, fmt.Errorf("admin access required"))
		return false
	}
	return true
}

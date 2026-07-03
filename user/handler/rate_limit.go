package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
)

type RateLimitHandler struct {
	rlStore   database.UserRateLimitStore
	userStore database.UserStore
	config    *config.Config
}

func NewRateLimitHandler(cfg *config.Config) *RateLimitHandler {
	return &RateLimitHandler{
		rlStore:   database.NewUserRateLimitStore(),
		userStore: database.NewUserStore(),
		config:    cfg,
	}
}

// ListRateLimits returns all user rate limit overrides + global defaults.
// GET /api/v1/admin/rate_limits
func (h *RateLimitHandler) ListRateLimits(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	rls, err := h.rlStore.List(c.Request.Context())
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list rate limits: %w", err))
		return
	}
	httpbase.OK(c, gin.H{
		"defaults": gin.H{
			"rpm": h.config.AIGateway.DefaultRPM,
			"tpm": h.config.AIGateway.DefaultTPM,
		},
		"overrides": rls,
	})
}

// UpsertRateLimit creates or updates a user's rate limit override.
// POST /api/v1/admin/rate_limits
func (h *RateLimitHandler) UpsertRateLimit(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var body struct {
		Username string `json:"username"`
		RPM      int    `json:"rpm"`
		TPM      int    `json:"tpm"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Username == "" {
		httpbase.BadRequest(c, "invalid request: username required")
		return
	}
	user, err := h.userStore.FindByUsername(c.Request.Context(), body.Username)
	if err != nil {
		httpbase.NotFoundError(c, fmt.Errorf("user not found: %s", body.Username))
		return
	}
	rl := &database.UserRateLimit{
		UserID:   user.ID,
		Username: body.Username,
		RPM:      body.RPM,
		TPM:      body.TPM,
	}
	if err := h.rlStore.Upsert(c.Request.Context(), rl); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to upsert rate limit: %w", err))
		return
	}
	httpbase.OK(c, rl)
}

// DeleteRateLimit removes a user's rate limit override (falls back to defaults).
// DELETE /api/v1/admin/rate_limits/:id
func (h *RateLimitHandler) DeleteRateLimit(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	userIDStr := c.Param("id")
	userID, err := strconv.ParseInt(userIDStr, 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid user ID")
		return
	}
	if err := h.rlStore.Delete(c.Request.Context(), userID); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to delete rate limit: %w", err))
		return
	}
	httpbase.OK(c, gin.H{"message": "rate limit override removed"})
}

// GetMyRateLimit returns the current user's effective rate limits.
// GET /api/v1/user/rate_limit
func (h *RateLimitHandler) GetMyRateLimit(c *gin.Context) {
	username := httpbase.GetCurrentUser(c)
	if username == "" {
		httpbase.UnauthorizedError(c, fmt.Errorf("not logged in"))
		return
	}
	user, err := h.userStore.FindByUsername(c.Request.Context(), username)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find user: %w", err))
		return
	}

	rpm := h.config.AIGateway.DefaultRPM
	tpm := h.config.AIGateway.DefaultTPM

	userRL, err := h.rlStore.FindByUserID(c.Request.Context(), user.ID)
	if err == nil && userRL != nil {
		rpm = userRL.RPM
		tpm = userRL.TPM
	}

	httpbase.OK(c, gin.H{
		"rpm": rpm,
		"tpm": tpm,
	})
}

func (h *RateLimitHandler) requireAdmin(c *gin.Context) bool {
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

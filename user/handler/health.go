package handler

import (
	"fmt"
	"strconv"

	"github.com/gin-gonic/gin"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/builder/store/database"
)

type HealthHandler struct {
	store database.ModelHealthLogStore
}

func NewHealthHandler() *HealthHandler {
	return &HealthHandler{
		store: database.NewModelHealthLogStore(),
	}
}

// ListHealthLogs returns recent health check logs.
// GET /api/v1/admin/health_logs
func (h *HealthHandler) ListHealthLogs(c *gin.Context) {
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
	if limit < 1 || limit > 200 {
		limit = 50
	}

	modelIDStr := c.Query("model_id")
	if modelIDStr != "" {
		modelID, err := strconv.ParseInt(modelIDStr, 10, 64)
		if err != nil {
			httpbase.BadRequest(c, "invalid model_id")
			return
		}
		logs, err := h.store.ListByModel(c.Request.Context(), modelID, limit)
		if err != nil {
			httpbase.ServerError(c, fmt.Errorf("failed to list health logs: %w", err))
			return
		}
		httpbase.OK(c, logs)
		return
	}

	logs, err := h.store.ListRecent(c.Request.Context(), limit)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list health logs: %w", err))
		return
	}
	httpbase.OK(c, logs)
}

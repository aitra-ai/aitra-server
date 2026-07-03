package handler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
)

// GPUDeploymentHandler handles GPU SKU management and deployment billing APIs.
type GPUDeploymentHandler struct {
	skuStore         database.GPUSkuStore
	deployStore      database.DeploymentBillingStore
	deployTaskStore  database.DeployTaskStore
	creditStore      database.UserCreditStore
	userStore        database.UserStore
	config           *config.Config
}

func NewGPUDeploymentHandler(cfg *config.Config) (*GPUDeploymentHandler, error) {
	return &GPUDeploymentHandler{
		skuStore:        database.NewGPUSkuStore(cfg),
		deployStore:     database.NewDeploymentBillingStore(),
		deployTaskStore: database.NewDeployTaskStore(),
		creditStore:     database.NewUserCreditStore(),
		userStore:       database.NewUserStore(),
		config:          cfg,
	}, nil
}

// requireAdmin checks that the current user has admin role.
func (h *GPUDeploymentHandler) requireAdmin(c *gin.Context) bool {
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
		httpbase.ForbiddenError(c, fmt.Errorf("admin access required"))
		return false
	}
	return true
}

// DeploymentWithCost adds computed cost fields to DeploymentBilling for API responses.
type DeploymentWithCost struct {
	database.DeploymentBilling
	RunningHours         float64 `json:"running_hours"`
	EstimatedCurrentBill float64 `json:"estimated_current_bill"`
	Source               string  `json:"source,omitempty"`
}

func enrichDeployment(d database.DeploymentBilling) DeploymentWithCost {
	now := time.Now()
	var runningHours float64
	if d.Status == "running" {
		runningHours = now.Sub(d.StartedAt).Hours()
	} else if d.StoppedAt != nil {
		runningHours = d.StoppedAt.Sub(d.StartedAt).Hours()
	}
	estimated := d.PricePerHour * runningHours
	return DeploymentWithCost{
		DeploymentBilling:    d,
		RunningHours:         runningHours,
		EstimatedCurrentBill: estimated,
	}
}

// ListPublicGPUSkus lists enabled GPU SKUs for public display.
// GET /api/v1/public/gpu/skus
func (h *GPUDeploymentHandler) ListPublicGPUSkus(c *gin.Context) {
	skus, err := h.skuStore.List(c.Request.Context(), true)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list gpu skus: %w", err))
		return
	}
	httpbase.OK(c, skus)
}

// ListMyDeployments returns the current user's deployments with running cost.
// Merges deployment_billings (GPU billing) + deploys (serverless) into a unified list.
// GET /api/v1/user/gpu/deployments
func (h *GPUDeploymentHandler) ListMyDeployments(c *gin.Context) {
	username := httpbase.GetCurrentUser(c)
	if username == "" {
		httpbase.UnauthorizedError(c, fmt.Errorf("not logged in"))
		return
	}
	u, err := h.userStore.FindByUsername(c.Request.Context(), username)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find user: %w", err))
		return
	}
	ctx := c.Request.Context()

	// 1. GPU billing deployments
	deployments, err := h.deployStore.ListByUser(ctx, u.ID)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list deployments: %w", err))
		return
	}
	result := make([]DeploymentWithCost, 0, len(deployments)+4)
	for _, d := range deployments {
		result = append(result, enrichDeployment(d))
	}

	// 2. Serverless deploys from deploys table (K8s runner)
	serverlessDeploys, _, _ := h.deployTaskStore.ListDeployByUserID(ctx, u.ID, &types.DeployReq{
		DeployType: types.ServerlessType,
		PageOpts:   types.PageOpts{Page: 1, PageSize: 50},
	})
	for _, sd := range serverlessDeploys {
		statusStr := serverlessStatusToString(sd.Status)
		result = append(result, DeploymentWithCost{
			DeploymentBilling: database.DeploymentBilling{
				ID:         sd.ID,
				UserID:     sd.UserID,
				DeployName: sd.DeployName,
				Status:     statusStr,
				SkuName:    parseGPUFromHardware(sd.Hardware),
				StartedAt:  sd.CreatedAt,
				CreatedAt:  sd.CreatedAt,
			},
			Source: "serverless",
		})
	}

	httpbase.OK(c, result)
}

func serverlessStatusToString(status int) string {
	switch status {
	case 20: // Running
		return "running"
	case 30: // Stopped
		return "stopped"
	case 50: // DeployFailed
		return "failed"
	default:
		return "deploying"
	}
}

func parseGPUFromHardware(hw string) string {
	// hardware is JSON like {"gpu":{"type":"SXM4-80GB","num":"1",...},...}
	if hw == "" {
		return "N/A"
	}
	// Simple extraction — look for "type":"xxx"
	idx := 0
	for i := 0; i < len(hw)-6; i++ {
		if hw[i:i+6] == "\"type\"" {
			idx = i + 6
			break
		}
	}
	if idx == 0 {
		return "GPU"
	}
	// Find the value after :"
	start := 0
	for i := idx; i < len(hw); i++ {
		if hw[i] == '"' && start == 0 {
			start = i + 1
		} else if hw[i] == '"' && start > 0 {
			return hw[start:i]
		}
	}
	return "GPU"
}

// createDeploymentReq is the request body for CreateDeployment.
type createDeploymentReq struct {
	DeployName string `json:"deploy_name" binding:"required"`
	ModelPath  string `json:"model_path"`
	SkuName    string `json:"sku_name" binding:"required"`
}

// CreateDeployment creates a new GPU deployment billing record.
// POST /api/v1/user/gpu/deployments
func (h *GPUDeploymentHandler) CreateDeployment(c *gin.Context) {
	username := httpbase.GetCurrentUser(c)
	if username == "" {
		httpbase.UnauthorizedError(c, fmt.Errorf("not logged in"))
		return
	}
	u, err := h.userStore.FindByUsername(c.Request.Context(), username)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find user: %w", err))
		return
	}

	var req createDeploymentReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequestWithExt(c, fmt.Errorf("invalid request: %w", err))
		return
	}

	// Check balance
	balance, err := h.creditStore.Balance(c.Request.Context(), u.ID)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to check balance: %w", err))
		return
	}
	if balance <= 0 {
		httpbase.BadRequestWithExt(c, fmt.Errorf("insufficient balance: $%.4f remaining", balance))
		return
	}

	// Validate SKU
	sku, err := h.skuStore.FindByName(c.Request.Context(), req.SkuName)
	if err != nil {
		httpbase.BadRequestWithExt(c, fmt.Errorf("sku not found: %s", req.SkuName))
		return
	}
	if !sku.Enabled {
		httpbase.BadRequestWithExt(c, fmt.Errorf("sku is not available: %s", req.SkuName))
		return
	}

	now := time.Now()
	d := &database.DeploymentBilling{
		UserID:       u.ID,
		Username:     u.Username,
		DeployName:   req.DeployName,
		ModelPath:    req.ModelPath,
		SkuName:      sku.Name,
		PricePerHour: sku.PricePerHour,
		Status:       "running",
		StartedAt:    now,
		LastBilledAt: now,
		CreatedAt:    now,
	}
	if err := h.deployStore.Create(c.Request.Context(), d); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to create deployment: %w", err))
		return
	}
	httpbase.OK(c, enrichDeployment(*d))
}

// StopDeployment stops a running deployment (must belong to current user).
// PUT /api/v1/user/gpu/deployments/:id/stop
func (h *GPUDeploymentHandler) StopDeployment(c *gin.Context) {
	username := httpbase.GetCurrentUser(c)
	if username == "" {
		httpbase.UnauthorizedError(c, fmt.Errorf("not logged in"))
		return
	}
	u, err := h.userStore.FindByUsername(c.Request.Context(), username)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find user: %w", err))
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequestWithExt(c, fmt.Errorf("invalid deployment id"))
		return
	}

	d, err := h.deployStore.FindByID(c.Request.Context(), id)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("deployment not found: %w", err))
		return
	}
	if d.UserID != u.ID {
		httpbase.ForbiddenError(c, fmt.Errorf("access denied"))
		return
	}
	if d.Status == "stopped" {
		httpbase.BadRequestWithExt(c, fmt.Errorf("deployment is already stopped"))
		return
	}

	if err := h.deployStore.Stop(c.Request.Context(), id); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to stop deployment: %w", err))
		return
	}
	httpbase.OK(c, gin.H{"message": "deployment stopped"})
}

// DeleteDeployment deletes a stopped deployment.
// DELETE /api/v1/user/gpu/deployments/:id
func (h *GPUDeploymentHandler) DeleteDeployment(c *gin.Context) {
	username := httpbase.GetCurrentUser(c)
	if username == "" {
		httpbase.UnauthorizedError(c, fmt.Errorf("not logged in"))
		return
	}
	u, err := h.userStore.FindByUsername(c.Request.Context(), username)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find user: %w", err))
		return
	}

	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequestWithExt(c, fmt.Errorf("invalid deployment id"))
		return
	}

	ctx := c.Request.Context()

	// Try deployment_billings first
	d, billingErr := h.deployStore.FindByID(ctx, id)
	if billingErr == nil && d != nil {
		if d.UserID != u.ID {
			httpbase.ForbiddenError(c, fmt.Errorf("access denied"))
			return
		}
		if d.Status != "stopped" {
			httpbase.BadRequestWithExt(c, fmt.Errorf("can only delete stopped deployments"))
			return
		}
		if err := h.deployStore.Delete(ctx, id); err != nil {
			httpbase.ServerError(c, fmt.Errorf("failed to delete deployment: %w", err))
			return
		}
		httpbase.OK(c, gin.H{"message": "deployment deleted"})
		return
	}

	// Fallback: try deploys table (serverless)
	if err := h.deployTaskStore.DeleteDeployByID(ctx, u.ID, id); err != nil {
		httpbase.ServerError(c, fmt.Errorf("deployment not found or access denied: %w", err))
		return
	}
	httpbase.OK(c, gin.H{"message": "deployment deleted"})
}

// --- Admin handlers ---

// AdminListGPUSkus lists all GPU SKUs (admin only).
// GET /api/v1/admin/gpu/skus
func (h *GPUDeploymentHandler) AdminListGPUSkus(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	skus, err := h.skuStore.List(c.Request.Context(), false)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list gpu skus: %w", err))
		return
	}
	httpbase.OK(c, skus)
}

type createGPUSkuReq struct {
	Name         string  `json:"name" binding:"required"`
	DisplayName  string  `json:"display_name"`
	GPUModel     string  `json:"gpu_model"`
	VCPUs        int     `json:"vcpus"`
	MemoryGB     int     `json:"memory_gb"`
	GPUCount     int     `json:"gpu_count"`
	PricePerHour float64 `json:"price_per_hour"`
	Enabled      bool    `json:"enabled"`
}

// AdminCreateGPUSku creates a new GPU SKU (admin only).
// POST /api/v1/admin/gpu/skus
func (h *GPUDeploymentHandler) AdminCreateGPUSku(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var req createGPUSkuReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequestWithExt(c, fmt.Errorf("invalid request: %w", err))
		return
	}
	sku := &database.GPUSku{
		Name:         req.Name,
		DisplayName:  req.DisplayName,
		GPUModel:     req.GPUModel,
		VCPUs:        req.VCPUs,
		MemoryGB:     req.MemoryGB,
		GPUCount:     req.GPUCount,
		PricePerHour: req.PricePerHour,
		Enabled:      req.Enabled,
	}
	if err := h.skuStore.Create(c.Request.Context(), sku); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to create gpu sku: %w", err))
		return
	}
	httpbase.OK(c, sku)
}

type updateGPUSkuReq struct {
	DisplayName  string  `json:"display_name"`
	GPUModel     string  `json:"gpu_model"`
	VCPUs        int     `json:"vcpus"`
	MemoryGB     int     `json:"memory_gb"`
	GPUCount     int     `json:"gpu_count"`
	PricePerHour float64 `json:"price_per_hour"`
	Enabled      bool    `json:"enabled"`
}

// AdminUpdateGPUSku updates a GPU SKU (admin only).
// PUT /api/v1/admin/gpu/skus/:id
func (h *GPUDeploymentHandler) AdminUpdateGPUSku(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequestWithExt(c, fmt.Errorf("invalid sku id"))
		return
	}
	var req updateGPUSkuReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequestWithExt(c, fmt.Errorf("invalid request: %w", err))
		return
	}
	sku := &database.GPUSku{
		ID:           id,
		DisplayName:  req.DisplayName,
		GPUModel:     req.GPUModel,
		VCPUs:        req.VCPUs,
		MemoryGB:     req.MemoryGB,
		GPUCount:     req.GPUCount,
		PricePerHour: req.PricePerHour,
		Enabled:      req.Enabled,
	}
	if err := h.skuStore.Update(c.Request.Context(), sku); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to update gpu sku: %w", err))
		return
	}
	httpbase.OK(c, sku)
}

// AdminDeleteGPUSku deletes a GPU SKU (admin only).
// DELETE /api/v1/admin/gpu/skus/:id
func (h *GPUDeploymentHandler) AdminDeleteGPUSku(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequestWithExt(c, fmt.Errorf("invalid sku id"))
		return
	}
	if err := h.skuStore.Delete(c.Request.Context(), id); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to delete gpu sku: %w", err))
		return
	}
	httpbase.OK(c, gin.H{"message": "gpu sku deleted"})
}

// AdminListDeployments lists all user deployments (admin only).
// GET /api/v1/admin/gpu/deployments
func (h *GPUDeploymentHandler) AdminListDeployments(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	deployments, err := h.deployStore.ListAll(c.Request.Context())
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list deployments: %w", err))
		return
	}
	result := make([]DeploymentWithCost, 0, len(deployments))
	for _, d := range deployments {
		result = append(result, enrichDeployment(d))
	}
	httpbase.OK(c, result)
}

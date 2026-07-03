package handler

import (
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
)

type UsageHandler struct {
	usageStore   database.ModelUsageLogStore
	billingStore database.LLMBillingStore
	userStore    database.UserStore
	config       *config.Config
}

func NewUsageHandler(cfg *config.Config) (*UsageHandler, error) {
	return &UsageHandler{
		usageStore:   database.NewModelUsageLogStore(cfg),
		billingStore: database.NewLLMBillingStore(cfg),
		userStore:    database.NewUserStore(),
		config:       cfg,
	}, nil
}

// parseUsageFilter reads filter params from gin context query string.
func parseUsageFilter(c *gin.Context) database.UsageFilter {
	f := database.UsageFilter{
		Username: c.Query("username"),
		ModelID:  c.Query("model_id"),
		Provider: c.Query("provider"),
	}
	if sc := c.Query("status_code"); sc != "" {
		if code, err := strconv.Atoi(sc); err == nil {
			f.StatusCode = &code
		}
	}
	if s := c.Query("start_date"); s != "" {
		if t, err := time.Parse("2006-01-02", s); err == nil {
			f.StartDate = &t
		}
	}
	if e := c.Query("end_date"); e != "" {
		if t, err := time.Parse("2006-01-02", e); err == nil {
			end := t.Add(24*time.Hour - time.Second)
			f.EndDate = &end
		}
	}
	return f
}

// GetMyUsage returns the current user's paginated usage logs.
// GET /api/v1/user/usage
func (h *UsageHandler) GetMyUsage(c *gin.Context) {
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

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 20
	}
	filter := parseUsageFilter(c)

	logs, total, err := h.usageStore.ListByUser(c.Request.Context(), u.ID, filter, page, perPage)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list usage: %w", err))
		return
	}
	httpbase.OK(c, gin.H{
		"data":     logs,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// GetMyUsageSummary returns per-model aggregate stats for the current user.
// GET /api/v1/user/usage/summary
func (h *UsageHandler) GetMyUsageSummary(c *gin.Context) {
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

	filter := parseUsageFilter(c)
	stats, err := h.usageStore.SummaryByUser(c.Request.Context(), u.ID, filter)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to summarize usage: %w", err))
		return
	}
	httpbase.OK(c, stats)
}

// requireAdmin checks that the current user has admin role, returns false and writes error if not.
func (h *UsageHandler) requireAdmin(c *gin.Context) bool {
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

// GetAuditUsage returns all usage logs with optional filters (admin only).
// GET /api/v1/admin/audit/usage
func (h *UsageHandler) GetAuditUsage(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	perPage, _ := strconv.Atoi(c.DefaultQuery("per_page", "20"))
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 200 {
		perPage = 20
	}
	filter := parseUsageFilter(c)

	logs, total, err := h.usageStore.ListAll(c.Request.Context(), filter, page, perPage)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list audit usage: %w", err))
		return
	}
	httpbase.OK(c, gin.H{
		"data":     logs,
		"total":    total,
		"page":     page,
		"per_page": perPage,
	})
}

// GetAuditSummary returns platform-wide stats + top users + top models (admin only).
// GET /api/v1/admin/audit/summary
func (h *UsageHandler) GetAuditSummary(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	filter := parseUsageFilter(c)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	if limit < 1 || limit > 100 {
		limit = 10
	}

	summary, err := h.usageStore.Summary(c.Request.Context(), filter)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to get summary: %w", err))
		return
	}
	topUsers, err := h.usageStore.TopUsers(c.Request.Context(), filter, limit)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to get top users: %w", err))
		return
	}
	topModels, err := h.usageStore.TopModels(c.Request.Context(), filter, limit)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to get top models: %w", err))
		return
	}
	httpbase.OK(c, gin.H{
		"summary":    summary,
		"top_users":  topUsers,
		"top_models": topModels,
	})
}

// ListBilling returns all billing pricing configs (admin only).
// GET /api/v1/admin/billing
func (h *UsageHandler) ListBilling(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	configs, err := h.billingStore.List(c.Request.Context())
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list billing: %w", err))
		return
	}
	httpbase.OK(c, configs)
}

// CreateBilling creates a new billing pricing config (admin only).
// POST /api/v1/admin/billing
func (h *UsageHandler) CreateBilling(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var b database.LLMBilling
	if err := c.ShouldBindJSON(&b); err != nil {
		httpbase.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	if b.ModelID == "" || b.Provider == "" {
		httpbase.BadRequest(c, "model_id and provider are required")
		return
	}
	if err := h.billingStore.Create(c.Request.Context(), &b); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to create billing: %w", err))
		return
	}
	httpbase.OK(c, b)
}

// UpdateBilling updates an existing billing pricing config (admin only).
// PUT /api/v1/admin/billing/:id
func (h *UsageHandler) UpdateBilling(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid id: "+idStr)
		return
	}
	// Parse only price fields from request
	var req struct {
		PriceInput  *float64 `json:"price_input"`
		PriceOutput *float64 `json:"price_output"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequest(c, "invalid request body: "+err.Error())
		return
	}
	// Fetch existing record first
	existing, err := h.billingStore.FindByID(c.Request.Context(), id)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("billing config not found: %w", err))
		return
	}
	// Only update price fields
	if req.PriceInput != nil {
		existing.PriceInput = *req.PriceInput
	}
	if req.PriceOutput != nil {
		existing.PriceOutput = *req.PriceOutput
	}
	if err := h.billingStore.Update(c.Request.Context(), existing); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to update billing: %w", err))
		return
	}
	httpbase.OK(c, existing)
}

// DeleteBilling deletes a billing pricing config (admin only).
// DELETE /api/v1/admin/billing/:id
func (h *UsageHandler) DeleteBilling(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid id: "+idStr)
		return
	}
	if err := h.billingStore.Delete(c.Request.Context(), id); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to delete billing: %w", err))
		return
	}
	httpbase.OK(c, nil)
}

// GetMyBalance returns current user's credit balance.
// GET /api/v1/user/balance
func (h *UsageHandler) GetMyBalance(c *gin.Context) {
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
	creditStore := database.NewUserCreditStore()
	balance, err := creditStore.Balance(c.Request.Context(), u.ID)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to get balance: %w", err))
		return
	}
	grants, _ := creditStore.ListByUser(c.Request.Context(), u.ID)
	spent, _ := creditStore.TotalSpent(c.Request.Context(), u.ID)
	httpbase.OK(c, gin.H{
		"balance_usd": balance,
		"spent_usd":   spent,
		"grants":      grants,
	})
}

// ListUserBalances returns all user balances (admin only).
// GET /api/v1/admin/credits
func (h *UsageHandler) ListUserBalances(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	creditStore := database.NewUserCreditStore()
	balances, err := creditStore.ListUserBalances(c.Request.Context())
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to list balances: %w", err))
		return
	}
	httpbase.OK(c, balances)
}

// GrantCredit grants credits to a user (admin only).
// POST /api/v1/admin/credits/grant
// Body: { username: string, amount_usd: number, note: string }
func (h *UsageHandler) GrantCredit(c *gin.Context) {
	if !h.requireAdmin(c) {
		return
	}
	var body struct {
		Username  string  `json:"username"`
		AmountUSD float64 `json:"amount_usd"`
		Note      string  `json:"note"`
	}
	if err := c.ShouldBindJSON(&body); err != nil || body.Username == "" || body.AmountUSD <= 0 {
		httpbase.BadRequest(c, "invalid request: username and positive amount_usd required")
		return
	}
	u, err := h.userStore.FindByUsername(c.Request.Context(), body.Username)
	if err != nil {
		httpbase.NotFoundError(c, fmt.Errorf("user not found: %s", body.Username))
		return
	}
	adminUser := httpbase.GetCurrentUser(c)
	creditStore := database.NewUserCreditStore()
	err = creditStore.Create(c.Request.Context(), &database.UserCredit{
		UserID:    u.ID,
		Username:  body.Username,
		AmountUSD: body.AmountUSD,
		Note:      body.Note,
		GrantedBy: adminUser,
		CreatedAt: time.Now(),
	})
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to grant credit: %w", err))
		return
	}
	httpbase.OK(c, gin.H{"message": fmt.Sprintf("Granted $%.2f to %s", body.AmountUSD, body.Username)})
}

// GetPublicModelStats returns platform-level per-model call counts (no auth required).
// GET /api/v1/public/model_stats
func (h *UsageHandler) GetPublicModelStats(c *gin.Context) {
	stats, err := h.usageStore.TopModels(c.Request.Context(), database.UsageFilter{}, 100)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to get model stats: %w", err))
		return
	}
	// Only expose model_id and total_requests (no cost/token details)
	type PublicModelStat struct {
		ModelID       string `json:"model_id"`
		TotalRequests int64  `json:"total_requests"`
	}
	result := make([]PublicModelStat, 0, len(stats))
	for _, s := range stats {
		result = append(result, PublicModelStat{
			ModelID:       s.ModelID,
			TotalRequests: s.TotalReqs,
		})
	}
	httpbase.OK(c, result)
}

// GetMyBudget returns the user's monthly budget and current spend.
// GET /api/v1/user/budget
func (h *UsageHandler) GetMyBudget(c *gin.Context) {
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
	spend, _ := h.usageStore.MonthlySpend(c.Request.Context(), u.ID)
	httpbase.OK(c, gin.H{
		"monthly_budget_usd": u.MonthlyBudget,
		"current_spend_usd":  spend,
		"percentage":         budgetPercentage(u.MonthlyBudget, spend),
	})
}

// SetMyBudget sets the user's monthly budget.
// PUT /api/v1/user/budget
func (h *UsageHandler) SetMyBudget(c *gin.Context) {
	username := httpbase.GetCurrentUser(c)
	if username == "" {
		httpbase.UnauthorizedError(c, fmt.Errorf("not logged in"))
		return
	}
	var req struct {
		MonthlyBudgetUSD float64 `json:"monthly_budget_usd"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequest(c, "invalid request body")
		return
	}
	if req.MonthlyBudgetUSD < 0 {
		httpbase.BadRequest(c, "budget must be >= 0")
		return
	}
	u, err := h.userStore.FindByUsername(c.Request.Context(), username)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find user: %w", err))
		return
	}
	u.MonthlyBudget = req.MonthlyBudgetUSD
	err = h.userStore.Update(c.Request.Context(), &u, "")
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to update budget: %w", err))
		return
	}
	httpbase.OK(c, gin.H{"monthly_budget_usd": req.MonthlyBudgetUSD})
}

func budgetPercentage(budget, spend float64) float64 {
	if budget <= 0 {
		return 0
	}
	pct := (spend / budget) * 100
	if pct > 100 {
		return 100
	}
	return pct
}

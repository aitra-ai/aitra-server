package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"opencsg.com/csghub-server/common/config"
)

type ModelUsageLog struct {
	bun.BaseModel  `bun:"table:model_usage_logs"`
	ID             int64     `bun:",pk,autoincrement" json:"id"`
	UserID         int64     `bun:",notnull" json:"user_id"`
	Username       string    `bun:",notnull" json:"username"`
	ModelID        string    `bun:",notnull" json:"model_id"`
	Provider       string    `bun:",notnull" json:"provider"`
	InputTokens    int       `bun:",notnull,default:0" json:"input_tokens"`
	OutputTokens   int       `bun:",notnull,default:0" json:"output_tokens"`
	CostUSD        float64   `bun:",notnull,default:0" json:"cost_usd"`
	StatusCode     int       `bun:",notnull,default:200" json:"status_code"`
	LatencyMs      int64     `bun:",notnull,default:0" json:"latency_ms"`
	RequestSummary string    `bun:",nullzero" json:"request_summary,omitempty"`
	CreatedAt      time.Time `bun:",notnull,default:current_timestamp" json:"created_at"`
}

// UsageFilter holds optional filter parameters for usage queries.
type UsageFilter struct {
	Username   string
	ModelID    string
	Provider   string
	StatusCode *int
	StartDate  *time.Time
	EndDate    *time.Time
}

// UsageSummary is the aggregate summary for admin.
type UsageSummary struct {
	TotalRequests int64   `json:"total_requests"`
	TotalInput    int64   `json:"total_input_tokens"`
	TotalOutput   int64   `json:"total_output_tokens"`
	TotalCost     float64 `json:"total_cost_usd"`
}

// UserUsageStat is per-user aggregate stats.
type UserUsageStat struct {
	Username    string  `bun:"username" json:"username"`
	TotalReqs   int64   `bun:"total_requests" json:"total_requests"`
	TotalInput  int64   `bun:"total_input" json:"total_input_tokens"`
	TotalOutput int64   `bun:"total_output" json:"total_output_tokens"`
	TotalCost   float64 `bun:"total_cost" json:"total_cost_usd"`
}

// ModelUsageStat is per-model aggregate stats.
type ModelUsageStat struct {
	ModelID     string  `bun:"model_id" json:"model_id"`
	Provider    string  `bun:"provider" json:"provider"`
	TotalReqs   int64   `bun:"total_requests" json:"total_requests"`
	TotalInput  int64   `bun:"total_input" json:"total_input_tokens"`
	TotalOutput int64   `bun:"total_output" json:"total_output_tokens"`
	TotalCost   float64 `bun:"total_cost" json:"total_cost_usd"`
}

type ModelUsageLogStore interface {
	Create(ctx context.Context, log *ModelUsageLog) error
	// User: list own logs with pagination
	ListByUser(ctx context.Context, userID int64, filter UsageFilter, page, perPage int) ([]ModelUsageLog, int64, error)
	// User: summary per model
	SummaryByUser(ctx context.Context, userID int64, filter UsageFilter) ([]ModelUsageStat, error)
	// Admin: all logs with filter + pagination
	ListAll(ctx context.Context, filter UsageFilter, page, perPage int) ([]ModelUsageLog, int64, error)
	// Admin: top users ranked by usage
	TopUsers(ctx context.Context, filter UsageFilter, limit int) ([]UserUsageStat, error)
	// Admin: usage by model
	TopModels(ctx context.Context, filter UsageFilter, limit int) ([]ModelUsageStat, error)
	// Admin: overall summary
	Summary(ctx context.Context, filter UsageFilter) (*UsageSummary, error)
	// MonthlySpend returns total cost_usd for a user in the current month
	MonthlySpend(ctx context.Context, userID int64) (float64, error)
}

type modelUsageLogStore struct {
	db *DB
}

func NewModelUsageLogStore(cfg *config.Config) ModelUsageLogStore {
	return &modelUsageLogStore{db: defaultDB}
}

func applyModelUsageFilter(q *bun.SelectQuery, f UsageFilter) *bun.SelectQuery {
	if f.Username != "" {
		q = q.Where("username ILIKE ?", "%"+f.Username+"%")
	}
	if f.ModelID != "" {
		q = q.Where("model_id = ?", f.ModelID)
	}
	if f.Provider != "" {
		q = q.Where("provider = ?", f.Provider)
	}
	if f.StatusCode != nil {
		q = q.Where("status_code = ?", *f.StatusCode)
	}
	if f.StartDate != nil {
		q = q.Where("created_at >= ?", f.StartDate)
	}
	if f.EndDate != nil {
		q = q.Where("created_at <= ?", f.EndDate)
	}
	return q
}

func (s *modelUsageLogStore) Create(ctx context.Context, log *ModelUsageLog) error {
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}
	res, err := s.db.Core.NewInsert().Model(log).Exec(ctx, log)
	if err := assertAffectedOneRow(res, err); err != nil {
		return fmt.Errorf("create model usage log: %w", err)
	}
	return nil
}

func (s *modelUsageLogStore) ListByUser(ctx context.Context, userID int64, filter UsageFilter, page, perPage int) ([]ModelUsageLog, int64, error) {
	var logs []ModelUsageLog
	q := s.db.Operator.Core.NewSelect().Model(&logs).Where("user_id = ?", userID)
	q = applyModelUsageFilter(q, filter)
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count usage logs by user: %w", err)
	}
	err = q.Order("created_at DESC").Limit(perPage).Offset((page - 1) * perPage).Scan(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list usage logs by user: %w", err)
	}
	return logs, int64(total), nil
}

func (s *modelUsageLogStore) SummaryByUser(ctx context.Context, userID int64, filter UsageFilter) ([]ModelUsageStat, error) {
	var stats []ModelUsageStat
	q := s.db.Operator.Core.NewSelect().
		TableExpr("model_usage_logs").
		ColumnExpr("model_id, provider, COUNT(*) AS total_requests, SUM(input_tokens) AS total_input, SUM(output_tokens) AS total_output, SUM(cost_usd) AS total_cost").
		Where("user_id = ?", userID).
		GroupExpr("model_id, provider").
		Order("total_cost DESC")
	q = applyModelUsageFilter(q, filter)
	err := q.Scan(ctx, &stats)
	if err != nil {
		return nil, fmt.Errorf("summary usage by user: %w", err)
	}
	return stats, nil
}

func (s *modelUsageLogStore) ListAll(ctx context.Context, filter UsageFilter, page, perPage int) ([]ModelUsageLog, int64, error) {
	var logs []ModelUsageLog
	q := s.db.Operator.Core.NewSelect().Model(&logs)
	q = applyModelUsageFilter(q, filter)
	total, err := q.Count(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("count all usage logs: %w", err)
	}
	err = q.Order("created_at DESC").Limit(perPage).Offset((page - 1) * perPage).Scan(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("list all usage logs: %w", err)
	}
	return logs, int64(total), nil
}

func (s *modelUsageLogStore) TopUsers(ctx context.Context, filter UsageFilter, limit int) ([]UserUsageStat, error) {
	var stats []UserUsageStat
	q := s.db.Operator.Core.NewSelect().
		TableExpr("model_usage_logs").
		ColumnExpr("username, COUNT(*) AS total_requests, SUM(input_tokens) AS total_input, SUM(output_tokens) AS total_output, SUM(cost_usd) AS total_cost").
		GroupExpr("username").
		Order("total_cost DESC").
		Limit(limit)
	q = applyModelUsageFilter(q, filter)
	err := q.Scan(ctx, &stats)
	if err != nil {
		return nil, fmt.Errorf("top users usage: %w", err)
	}
	return stats, nil
}

func (s *modelUsageLogStore) TopModels(ctx context.Context, filter UsageFilter, limit int) ([]ModelUsageStat, error) {
	var stats []ModelUsageStat
	q := s.db.Operator.Core.NewSelect().
		TableExpr("model_usage_logs").
		ColumnExpr("model_id, provider, COUNT(*) AS total_requests, SUM(input_tokens) AS total_input, SUM(output_tokens) AS total_output, SUM(cost_usd) AS total_cost").
		GroupExpr("model_id, provider").
		Order("total_cost DESC").
		Limit(limit)
	q = applyModelUsageFilter(q, filter)
	err := q.Scan(ctx, &stats)
	if err != nil {
		return nil, fmt.Errorf("top models usage: %w", err)
	}
	return stats, nil
}

func (s *modelUsageLogStore) Summary(ctx context.Context, filter UsageFilter) (*UsageSummary, error) {
	type row struct {
		TotalRequests int64   `bun:"total_requests"`
		TotalInput    int64   `bun:"total_input"`
		TotalOutput   int64   `bun:"total_output"`
		TotalCost     float64 `bun:"total_cost"`
	}
	var r row
	q := s.db.Operator.Core.NewSelect().
		TableExpr("model_usage_logs").
		ColumnExpr("COUNT(*) AS total_requests, SUM(input_tokens) AS total_input, SUM(output_tokens) AS total_output, SUM(cost_usd) AS total_cost")
	q = applyModelUsageFilter(q, filter)
	err := q.Scan(ctx, &r)
	if err != nil {
		return nil, fmt.Errorf("summary usage: %w", err)
	}
	return &UsageSummary{
		TotalRequests: r.TotalRequests,
		TotalInput:    r.TotalInput,
		TotalOutput:   r.TotalOutput,
		TotalCost:     r.TotalCost,
	}, nil
}

func (s *modelUsageLogStore) MonthlySpend(ctx context.Context, userID int64) (float64, error) {
	now := time.Now()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	var spend float64
	err := s.db.Operator.Core.NewSelect().
		TableExpr("model_usage_logs").
		ColumnExpr("COALESCE(SUM(cost_usd), 0)").
		Where("user_id = ? AND created_at >= ?", userID, monthStart).
		Scan(ctx, &spend)
	if err != nil {
		return 0, fmt.Errorf("monthly spend: %w", err)
	}
	return spend, nil
}

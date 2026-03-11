package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

type UserCredit struct {
	bun.BaseModel `bun:"table:user_credits"`
	ID        int64     `bun:",pk,autoincrement" json:"id"`
	UserID    int64     `bun:",notnull" json:"user_id"`
	Username  string    `bun:",notnull" json:"username"`
	AmountUSD float64   `bun:",notnull,default:0" json:"amount_usd"`
	Note      string    `bun:",notnull,default:''" json:"note"`
	GrantedBy string    `bun:",notnull,default:'system'" json:"granted_by"`
	CreatedAt time.Time `bun:",notnull,default:current_timestamp" json:"created_at"`
}

type UserBalanceSummary struct {
	UserID      int64   `bun:"user_id" json:"user_id"`
	Username    string  `bun:"username" json:"username"`
	TotalGrants float64 `bun:"total_grants" json:"total_grants"`
	TotalSpent  float64 `bun:"total_spent" json:"total_spent"`
	Balance     float64 `bun:"balance" json:"balance"`
}

type UserCreditStore interface {
	Create(ctx context.Context, c *UserCredit) error
	TotalGranted(ctx context.Context, userID int64) (float64, error)
	TotalSpent(ctx context.Context, userID int64) (float64, error)
	Balance(ctx context.Context, userID int64) (float64, error)
	ListByUser(ctx context.Context, userID int64) ([]UserCredit, error)
	ListUserBalances(ctx context.Context) ([]UserBalanceSummary, error)
}

type userCreditStore struct {
	db *DB
}

func NewUserCreditStore() UserCreditStore {
	return &userCreditStore{db: defaultDB}
}

func (s *userCreditStore) Create(ctx context.Context, c *UserCredit) error {
	if c.CreatedAt.IsZero() {
		c.CreatedAt = time.Now()
	}
	res, err := s.db.Core.NewInsert().Model(c).Exec(ctx, c)
	if err := assertAffectedOneRow(res, err); err != nil {
		return fmt.Errorf("create user credit: %w", err)
	}
	return nil
}

func (s *userCreditStore) TotalGranted(ctx context.Context, userID int64) (float64, error) {
	var total float64
	err := s.db.Operator.Core.NewSelect().
		TableExpr("user_credits").
		ColumnExpr("COALESCE(SUM(amount_usd), 0)").
		Where("user_id = ?", userID).
		Scan(ctx, &total)
	if err != nil {
		return 0, fmt.Errorf("total granted: %w", err)
	}
	return total, nil
}

func (s *userCreditStore) TotalSpent(ctx context.Context, userID int64) (float64, error) {
	var total float64
	err := s.db.Operator.Core.NewSelect().
		TableExpr("model_usage_logs").
		ColumnExpr("COALESCE(SUM(cost_usd), 0)").
		Where("user_id = ?", userID).
		Scan(ctx, &total)
	if err != nil {
		return 0, fmt.Errorf("total spent: %w", err)
	}
	return total, nil
}

func (s *userCreditStore) Balance(ctx context.Context, userID int64) (float64, error) {
	granted, err := s.TotalGranted(ctx, userID)
	if err != nil {
		return 0, err
	}
	spent, err := s.TotalSpent(ctx, userID)
	if err != nil {
		return 0, err
	}
	return granted - spent, nil
}

func (s *userCreditStore) ListByUser(ctx context.Context, userID int64) ([]UserCredit, error) {
	var result []UserCredit
	err := s.db.Operator.Core.NewSelect().
		Model(&result).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list user credits: %w", err)
	}
	return result, nil
}

func (s *userCreditStore) ListUserBalances(ctx context.Context) ([]UserBalanceSummary, error) {
	var result []UserBalanceSummary
	err := s.db.Operator.Core.NewSelect().
		TableExpr("users u").
		ColumnExpr("u.id AS user_id").
		ColumnExpr("u.username").
		ColumnExpr("COALESCE(uc.total_grants, 0) AS total_grants").
		ColumnExpr("COALESCE(mul.total_spent, 0) AS total_spent").
		ColumnExpr("COALESCE(uc.total_grants, 0) - COALESCE(mul.total_spent, 0) AS balance").
		Join("LEFT JOIN (SELECT user_id, SUM(amount_usd) AS total_grants FROM user_credits GROUP BY user_id) uc ON uc.user_id = u.id").
		Join("LEFT JOIN (SELECT user_id, SUM(cost_usd) AS total_spent FROM model_usage_logs GROUP BY user_id) mul ON mul.user_id = u.id").
		Where("uc.user_id IS NOT NULL").
		Order("balance DESC").
		Scan(ctx, &result)
	if err != nil {
		return nil, fmt.Errorf("list user balances: %w", err)
	}
	return result, nil
}

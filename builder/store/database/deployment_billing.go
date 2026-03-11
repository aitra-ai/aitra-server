package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

type DeploymentBilling struct {
	bun.BaseModel  `bun:"table:deployment_billing"`
	ID             int64      `bun:",pk,autoincrement" json:"id"`
	UserID         int64      `bun:",notnull" json:"user_id"`
	Username       string     `bun:",notnull" json:"username"`
	DeployName     string     `bun:",notnull,default:''" json:"deploy_name"`
	ModelPath      string     `bun:",notnull,default:''" json:"model_path"`
	SkuName        string     `bun:",notnull,default:''" json:"sku_name"`
	PricePerHour   float64    `bun:",notnull,default:0" json:"price_per_hour"`
	Status         string     `bun:",notnull,default:'running'" json:"status"` // running/stopped
	StartedAt      time.Time  `bun:",notnull,default:current_timestamp" json:"started_at"`
	StoppedAt      *time.Time `bun:",nullzero" json:"stopped_at,omitempty"`
	LastBilledAt   time.Time  `bun:",notnull,default:current_timestamp" json:"last_billed_at"`
	TotalBilledUSD float64    `bun:",notnull,default:0" json:"total_billed_usd"`
	CreatedAt      time.Time  `bun:",notnull,default:current_timestamp" json:"created_at"`
}

type DeploymentBillingStore interface {
	Create(ctx context.Context, d *DeploymentBilling) error
	ListByUser(ctx context.Context, userID int64) ([]DeploymentBilling, error)
	ListRunning(ctx context.Context) ([]DeploymentBilling, error)
	Stop(ctx context.Context, id int64) error
	UpdateBilling(ctx context.Context, id int64, billedAmount float64, lastBilledAt time.Time) error
	FindByID(ctx context.Context, id int64) (*DeploymentBilling, error)
	ListAll(ctx context.Context) ([]DeploymentBilling, error)
	Delete(ctx context.Context, id int64) error
}

type deploymentBillingStore struct {
	db *DB
}

func NewDeploymentBillingStore() DeploymentBillingStore {
	return &deploymentBillingStore{db: defaultDB}
}

func NewDeploymentBillingStoreFromDB(db *DB) DeploymentBillingStore {
	return &deploymentBillingStore{db: db}
}

func (s *deploymentBillingStore) Create(ctx context.Context, d *DeploymentBilling) error {
	now := time.Now()
	if d.StartedAt.IsZero() {
		d.StartedAt = now
	}
	if d.LastBilledAt.IsZero() {
		d.LastBilledAt = now
	}
	d.CreatedAt = now
	res, err := s.db.Core.NewInsert().Model(d).Exec(ctx, d)
	if err := assertAffectedOneRow(res, err); err != nil {
		return fmt.Errorf("create deployment billing: %w", err)
	}
	return nil
}

func (s *deploymentBillingStore) ListByUser(ctx context.Context, userID int64) ([]DeploymentBilling, error) {
	var result []DeploymentBilling
	err := s.db.Operator.Core.NewSelect().Model(&result).
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list deployment billing by user: %w", err)
	}
	return result, nil
}

func (s *deploymentBillingStore) ListRunning(ctx context.Context) ([]DeploymentBilling, error) {
	var result []DeploymentBilling
	err := s.db.Operator.Core.NewSelect().Model(&result).
		Where("status = 'running'").
		Order("id ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list running deployments: %w", err)
	}
	return result, nil
}

func (s *deploymentBillingStore) Stop(ctx context.Context, id int64) error {
	now := time.Now()
	_, err := s.db.Core.NewUpdate().
		TableExpr("deployment_billing").
		Set("status = 'stopped'").
		Set("stopped_at = ?", now).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("stop deployment billing: %w", err)
	}
	return nil
}

func (s *deploymentBillingStore) UpdateBilling(ctx context.Context, id int64, billedAmount float64, lastBilledAt time.Time) error {
	_, err := s.db.Core.NewUpdate().
		TableExpr("deployment_billing").
		Set("total_billed_usd = total_billed_usd + ?", billedAmount).
		Set("last_billed_at = ?", lastBilledAt).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update deployment billing: %w", err)
	}
	return nil
}

func (s *deploymentBillingStore) FindByID(ctx context.Context, id int64) (*DeploymentBilling, error) {
	var d DeploymentBilling
	err := s.db.Operator.Core.NewSelect().Model(&d).
		Where("id = ?", id).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find deployment billing by id: %w", err)
	}
	return &d, nil
}

func (s *deploymentBillingStore) ListAll(ctx context.Context) ([]DeploymentBilling, error) {
	var result []DeploymentBilling
	err := s.db.Operator.Core.NewSelect().Model(&result).
		Order("created_at DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list all deployment billing: %w", err)
	}
	return result, nil
}

func (s *deploymentBillingStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Operator.Core.NewDelete().Model((*DeploymentBilling)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete deployment billing: %w", err)
	}
	return nil
}

package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

type ModelHealthLog struct {
	bun.BaseModel `bun:"table:model_health_logs"`
	ID            int64     `bun:",pk,autoincrement" json:"id"`
	ModelID       int64     `bun:",notnull" json:"model_id"`
	ModelName     string    `bun:",notnull" json:"model_name"`
	Provider      string    `bun:",notnull" json:"provider"`
	Status        string    `bun:",notnull" json:"status"` // "online" or "offline"
	LatencyMs     int64     `bun:",notnull,default:0" json:"latency_ms"`
	ErrorMsg      string    `bun:",nullzero" json:"error_msg,omitempty"`
	CheckedAt     time.Time `bun:",notnull" json:"checked_at"`
}

type ModelHealthLogStore interface {
	Create(ctx context.Context, log *ModelHealthLog) error
	ListByModel(ctx context.Context, modelID int64, limit int) ([]ModelHealthLog, error)
	ListRecent(ctx context.Context, limit int) ([]ModelHealthLog, error)
	Cleanup(ctx context.Context, olderThan time.Time) (int64, error)
}

type modelHealthLogStoreImpl struct {
	db *DB
}

func NewModelHealthLogStore() ModelHealthLogStore {
	return &modelHealthLogStoreImpl{db: defaultDB}
}

func (s *modelHealthLogStoreImpl) Create(ctx context.Context, log *ModelHealthLog) error {
	_, err := s.db.Core.NewInsert().Model(log).Exec(ctx)
	if err != nil {
		return fmt.Errorf("create health log: %w", err)
	}
	return nil
}

func (s *modelHealthLogStoreImpl) ListByModel(ctx context.Context, modelID int64, limit int) ([]ModelHealthLog, error) {
	var logs []ModelHealthLog
	err := s.db.Operator.Core.NewSelect().Model(&logs).
		Where("model_id = ?", modelID).
		Order("checked_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list health logs by model: %w", err)
	}
	return logs, nil
}

func (s *modelHealthLogStoreImpl) ListRecent(ctx context.Context, limit int) ([]ModelHealthLog, error) {
	var logs []ModelHealthLog
	err := s.db.Operator.Core.NewSelect().Model(&logs).
		Order("checked_at DESC").
		Limit(limit).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list recent health logs: %w", err)
	}
	return logs, nil
}

func (s *modelHealthLogStoreImpl) Cleanup(ctx context.Context, olderThan time.Time) (int64, error) {
	res, err := s.db.Operator.Core.NewDelete().Model((*ModelHealthLog)(nil)).
		Where("checked_at < ?", olderThan).
		Exec(ctx)
	if err != nil {
		return 0, fmt.Errorf("cleanup health logs: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

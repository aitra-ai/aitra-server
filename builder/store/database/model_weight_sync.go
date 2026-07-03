package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"opencsg.com/csghub-server/common/config"
)

type ModelWeightSync struct {
	bun.BaseModel `bun:"table:model_weight_syncs"`
	ID            int64     `bun:",pk,autoincrement" json:"id"`
	RepoID        int64     `bun:"repo_id,notnull" json:"repo_id"`
	RepoPath      string    `bun:"repo_path,notnull,default:''" json:"repo_path"`
	HFModelID     string    `bun:"hf_model_id,notnull,default:''" json:"hf_model_id"`
	Status        string    `bun:",notnull,default:'pending'" json:"status"`
	TotalFiles    int       `bun:"total_files,notnull,default:0" json:"total_files"`
	SyncedFiles   int       `bun:"synced_files,notnull,default:0" json:"synced_files"`
	TotalSize     int64     `bun:"total_size,notnull,default:0" json:"total_size"`
	SyncedSize    int64     `bun:"synced_size,notnull,default:0" json:"synced_size"`
	ErrorMsg      string    `bun:"error_msg,notnull,default:''" json:"error_msg"`
	CreatedAt     time.Time `bun:",notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time `bun:",notnull,default:current_timestamp" json:"updated_at"`
}

type ModelWeightSyncStore interface {
	Create(ctx context.Context, s *ModelWeightSync) error
	FindByRepoID(ctx context.Context, repoID int64) (*ModelWeightSync, error)
	FindByStatus(ctx context.Context, status string) ([]ModelWeightSync, error)
	UpdateStatus(ctx context.Context, id int64, status, errorMsg string) error
	UpdateProgress(ctx context.Context, id int64, syncedFiles int, syncedSize int64) error
	List(ctx context.Context) ([]ModelWeightSync, error)
	Delete(ctx context.Context, id int64) error
}

type modelWeightSyncStore struct {
	db *DB
}

func NewModelWeightSyncStore(cfg *config.Config) ModelWeightSyncStore {
	return &modelWeightSyncStore{db: defaultDB}
}

func NewModelWeightSyncStoreFromDB(db *DB) ModelWeightSyncStore {
	return &modelWeightSyncStore{db: db}
}

func (s *modelWeightSyncStore) Create(ctx context.Context, sync *ModelWeightSync) error {
	now := time.Now()
	sync.CreatedAt = now
	sync.UpdatedAt = now
	res, err := s.db.Core.NewInsert().Model(sync).Exec(ctx, sync)
	if err := assertAffectedOneRow(res, err); err != nil {
		return fmt.Errorf("create model weight sync: %w", err)
	}
	return nil
}

func (s *modelWeightSyncStore) FindByRepoID(ctx context.Context, repoID int64) (*ModelWeightSync, error) {
	var sync ModelWeightSync
	err := s.db.Operator.Core.NewSelect().Model(&sync).
		Where("repo_id = ?", repoID).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find model weight sync by repo_id: %w", err)
	}
	return &sync, nil
}

func (s *modelWeightSyncStore) FindByStatus(ctx context.Context, status string) ([]ModelWeightSync, error) {
	var result []ModelWeightSync
	err := s.db.Operator.Core.NewSelect().Model(&result).
		Where("status = ?", status).
		Order("id ASC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find model weight syncs by status: %w", err)
	}
	return result, nil
}

func (s *modelWeightSyncStore) UpdateStatus(ctx context.Context, id int64, status, errorMsg string) error {
	now := time.Now()
	_, err := s.db.Core.NewUpdate().
		Model((*ModelWeightSync)(nil)).
		Set("status = ?, error_msg = ?, updated_at = ?", status, errorMsg, now).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update model weight sync status: %w", err)
	}
	return nil
}

func (s *modelWeightSyncStore) UpdateProgress(ctx context.Context, id int64, syncedFiles int, syncedSize int64) error {
	now := time.Now()
	_, err := s.db.Core.NewUpdate().
		Model((*ModelWeightSync)(nil)).
		Set("synced_files = ?, synced_size = ?, updated_at = ?", syncedFiles, syncedSize, now).
		Where("id = ?", id).
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("update model weight sync progress: %w", err)
	}
	return nil
}

func (s *modelWeightSyncStore) List(ctx context.Context) ([]ModelWeightSync, error) {
	var result []ModelWeightSync
	err := s.db.Operator.Core.NewSelect().Model(&result).
		Order("id DESC").
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list model weight syncs: %w", err)
	}
	return result, nil
}

func (s *modelWeightSyncStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Operator.Core.NewDelete().Model((*ModelWeightSync)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete model weight sync: %w", err)
	}
	return nil
}
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
)

type SandboxInstance struct {
	bun.BaseModel `bun:"table:space_sandbox_instances"`
	ID           int64      `bun:",pk,autoincrement" json:"id"`
	SpacePath    string     `bun:",notnull" json:"space_path"`
	Template     string     `bun:",notnull" json:"template"`
	UserID       int64      `bun:",notnull,default:0" json:"user_id"`
	Username     string     `bun:",notnull,default:''" json:"username"`
	Status       string     `bun:",notnull,default:'pending'" json:"status"`
	ContainerID  string     `bun:",nullzero" json:"container_id,omitempty"`
	Port         int        `bun:",nullzero" json:"port,omitempty"`
	AccessURL    string     `bun:",nullzero" json:"access_url,omitempty"`
	ErrorMsg     string     `bun:",nullzero" json:"error_msg,omitempty"`
	IsHotPool    bool       `bun:",notnull,default:false" json:"is_hot_pool"`
	StartedAt    *time.Time `bun:",nullzero" json:"started_at,omitempty"`
	LastActiveAt *time.Time `bun:",nullzero" json:"last_active_at,omitempty"`
	ExpiresAt    *time.Time `bun:",nullzero" json:"expires_at,omitempty"`
	CreatedAt    time.Time  `bun:",notnull,default:current_timestamp" json:"created_at"`
}

type FeaturedSpace struct {
	bun.BaseModel `bun:"table:featured_spaces"`
	ID          int64     `bun:",pk,autoincrement" json:"id"`
	SpacePath   string    `bun:",notnull,unique" json:"space_path"`
	Template    string    `bun:",notnull" json:"template"`
	DisplayName string    `bun:",notnull,default:''" json:"display_name"`
	Description string    `bun:",notnull,default:''" json:"description"`
	CoverURL    string    `bun:",nullzero" json:"cover_url,omitempty"`
	HotPool     int       `bun:",notnull,default:1" json:"hot_pool"`
	TTLSeconds  int       `bun:",notnull,default:1800" json:"ttl_seconds"`
	EnvVars     string    `bun:",nullzero" json:"env_vars,omitempty"`
	Enabled     bool      `bun:",notnull,default:true" json:"enabled"`
	SortOrder   int       `bun:",notnull,default:0" json:"sort_order"`
	CreatedAt   time.Time `bun:",notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt   time.Time `bun:",notnull,default:current_timestamp" json:"updated_at"`
}

// SandboxInstanceStore defines persistence operations for sandbox instances.
type SandboxInstanceStore interface {
	Create(ctx context.Context, inst *SandboxInstance) error
	FindByID(ctx context.Context, id int64) (*SandboxInstance, error)
	UpdateStatus(ctx context.Context, id int64, status, containerID, accessURL, errMsg string, port int) error
	UpdateLastActive(ctx context.Context, id int64) error
	CountHotPool(ctx context.Context, spacePath string) (int, error)
	ListHotPoolReady(ctx context.Context, spacePath string) ([]SandboxInstance, error)
	Delete(ctx context.Context, id int64) error
	ListExpired(ctx context.Context) ([]SandboxInstance, error)
	ListAll(ctx context.Context) ([]SandboxInstance, error)
}

type sandboxInstanceStore struct{ db *DB }

func NewSandboxInstanceStore() SandboxInstanceStore {
	return &sandboxInstanceStore{db: defaultDB}
}

func (s *sandboxInstanceStore) Create(ctx context.Context, inst *SandboxInstance) error {
	inst.CreatedAt = time.Now()
	res, err := s.db.Core.NewInsert().Model(inst).Exec(ctx, inst)
	if err := assertAffectedOneRow(res, err); err != nil {
		return fmt.Errorf("create sandbox instance: %w", err)
	}
	return nil
}

func (s *sandboxInstanceStore) FindByID(ctx context.Context, id int64) (*SandboxInstance, error) {
	var inst SandboxInstance
	err := s.db.Operator.Core.NewSelect().Model(&inst).Where("id = ?", id).Limit(1).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find sandbox instance: %w", err)
	}
	return &inst, nil
}

func (s *sandboxInstanceStore) UpdateStatus(ctx context.Context, id int64, status, containerID, accessURL, errMsg string, port int) error {
	now := time.Now()
	q := s.db.Core.NewUpdate().TableExpr("space_sandbox_instances").
		Set("status = ?", status).
		Set("container_id = ?", containerID).
		Set("access_url = ?", accessURL).
		Set("error_msg = ?", errMsg).
		Set("port = ?", port).
		Where("id = ?", id)
	if status == "running" {
		q = q.Set("started_at = ?", now)
	}
	_, err := q.Exec(ctx)
	return err
}

func (s *sandboxInstanceStore) UpdateLastActive(ctx context.Context, id int64) error {
	now := time.Now()
	_, err := s.db.Core.NewUpdate().TableExpr("space_sandbox_instances").
		Set("last_active_at = ?", now).Where("id = ?", id).Exec(ctx)
	return err
}

func (s *sandboxInstanceStore) CountHotPool(ctx context.Context, spacePath string) (int, error) {
	return s.db.Operator.Core.NewSelect().Model((*SandboxInstance)(nil)).
		Where("space_path = ? AND is_hot_pool = true AND status IN ('starting','running')", spacePath).
		Count(ctx)
}

func (s *sandboxInstanceStore) ListHotPoolReady(ctx context.Context, spacePath string) ([]SandboxInstance, error) {
	var result []SandboxInstance
	err := s.db.Operator.Core.NewSelect().Model(&result).
		Where("space_path = ? AND is_hot_pool = true AND status = 'running'", spacePath).
		Order("created_at ASC").Scan(ctx)
	return result, err
}

func (s *sandboxInstanceStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Operator.Core.NewDelete().Model((*SandboxInstance)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

func (s *sandboxInstanceStore) ListExpired(ctx context.Context) ([]SandboxInstance, error) {
	var result []SandboxInstance
	now := time.Now()
	err := s.db.Operator.Core.NewSelect().Model(&result).
		Where("status IN ('running','starting') AND expires_at IS NOT NULL AND expires_at < ?", now).
		Scan(ctx)
	return result, err
}

func (s *sandboxInstanceStore) ListAll(ctx context.Context) ([]SandboxInstance, error) {
	var result []SandboxInstance
	err := s.db.Operator.Core.NewSelect().Model(&result).Order("created_at DESC").Scan(ctx)
	return result, err
}

// FeaturedSpaceStore defines persistence operations for featured spaces.
type FeaturedSpaceStore interface {
	List(ctx context.Context) ([]FeaturedSpace, error)
	FindByPath(ctx context.Context, spacePath string) (*FeaturedSpace, error)
	Create(ctx context.Context, fs *FeaturedSpace) error
	Update(ctx context.Context, fs *FeaturedSpace) error
	Delete(ctx context.Context, id int64) error
}

type featuredSpaceStore struct{ db *DB }

func NewFeaturedSpaceStore() FeaturedSpaceStore {
	return &featuredSpaceStore{db: defaultDB}
}

func (s *featuredSpaceStore) List(ctx context.Context) ([]FeaturedSpace, error) {
	var result []FeaturedSpace
	err := s.db.Operator.Core.NewSelect().Model(&result).
		Where("enabled = true").Order("sort_order ASC, id ASC").Scan(ctx)
	return result, err
}

func (s *featuredSpaceStore) FindByPath(ctx context.Context, spacePath string) (*FeaturedSpace, error) {
	var fs FeaturedSpace
	err := s.db.Operator.Core.NewSelect().Model(&fs).Where("space_path = ?", spacePath).Limit(1).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &fs, nil
}

func (s *featuredSpaceStore) Create(ctx context.Context, fs *FeaturedSpace) error {
	now := time.Now()
	fs.CreatedAt = now
	fs.UpdatedAt = now
	res, err := s.db.Core.NewInsert().Model(fs).Exec(ctx, fs)
	if err := assertAffectedOneRow(res, err); err != nil {
		return fmt.Errorf("create featured space: %w", err)
	}
	return nil
}

func (s *featuredSpaceStore) Update(ctx context.Context, fs *FeaturedSpace) error {
	fs.UpdatedAt = time.Now()
	_, err := s.db.Core.NewUpdate().Model(fs).Where("id = ?", fs.ID).Exec(ctx)
	return err
}

func (s *featuredSpaceStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Operator.Core.NewDelete().Model((*FeaturedSpace)(nil)).Where("id = ?", id).Exec(ctx)
	return err
}

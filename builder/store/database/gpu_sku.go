package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"opencsg.com/csghub-server/common/config"
)

type GPUSku struct {
	bun.BaseModel `bun:"table:gpu_skus"`
	ID            int64     `bun:",pk,autoincrement" json:"id"`
	Name          string    `bun:",notnull,unique" json:"name"`
	DisplayName   string    `bun:",notnull,default:''" json:"display_name"`
	GPUModel      string    `bun:"gpu_model,notnull,default:''" json:"gpu_model"`
	VCPUs         int       `bun:"vcpus,notnull,default:0" json:"vcpus"`
	MemoryGB      int       `bun:"memory_gb,notnull,default:0" json:"memory_gb"`
	GPUCount      int       `bun:"gpu_count,notnull,default:1" json:"gpu_count"`
	PricePerHour  float64   `bun:",notnull,default:0" json:"price_per_hour"`
	Enabled       bool      `bun:",notnull,default:true" json:"enabled"`
	CreatedAt     time.Time `bun:",notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time `bun:",notnull,default:current_timestamp" json:"updated_at"`
}

type GPUSkuStore interface {
	List(ctx context.Context, enabledOnly bool) ([]GPUSku, error)
	FindByName(ctx context.Context, name string) (*GPUSku, error)
	Create(ctx context.Context, s *GPUSku) error
	Update(ctx context.Context, s *GPUSku) error
	Delete(ctx context.Context, id int64) error
}

type gpuSkuStore struct {
	db *DB
}

func NewGPUSkuStore(cfg *config.Config) GPUSkuStore {
	return &gpuSkuStore{db: defaultDB}
}

func NewGPUSkuStoreFromDB(db *DB) GPUSkuStore {
	return &gpuSkuStore{db: db}
}

func (s *gpuSkuStore) List(ctx context.Context, enabledOnly bool) ([]GPUSku, error) {
	var result []GPUSku
	q := s.db.Operator.Core.NewSelect().Model(&result).Order("id ASC")
	if enabledOnly {
		q = q.Where("enabled = true")
	}
	if err := q.Scan(ctx); err != nil {
		return nil, fmt.Errorf("list gpu skus: %w", err)
	}
	return result, nil
}

func (s *gpuSkuStore) FindByName(ctx context.Context, name string) (*GPUSku, error) {
	var sku GPUSku
	err := s.db.Operator.Core.NewSelect().Model(&sku).
		Where("name = ?", name).
		Limit(1).
		Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find gpu sku by name: %w", err)
	}
	return &sku, nil
}

func (s *gpuSkuStore) Create(ctx context.Context, sku *GPUSku) error {
	now := time.Now()
	sku.CreatedAt = now
	sku.UpdatedAt = now
	res, err := s.db.Core.NewInsert().Model(sku).Exec(ctx, sku)
	if err := assertAffectedOneRow(res, err); err != nil {
		return fmt.Errorf("create gpu sku: %w", err)
	}
	return nil
}

func (s *gpuSkuStore) Update(ctx context.Context, sku *GPUSku) error {
	sku.UpdatedAt = time.Now()
	_, err := s.db.Core.NewUpdate().Model(sku).Where("id = ?", sku.ID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update gpu sku: %w", err)
	}
	return nil
}

func (s *gpuSkuStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Operator.Core.NewDelete().Model((*GPUSku)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete gpu sku: %w", err)
	}
	return nil
}

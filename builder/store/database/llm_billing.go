package database

import (
	"context"
	"fmt"
	"time"

	"github.com/uptrace/bun"
	"opencsg.com/csghub-server/common/config"
)

type LLMBilling struct {
	bun.BaseModel `bun:"table:llm_billing"`
	ID            int64     `bun:",pk,autoincrement" json:"id"`
	ModelID       string    `bun:",notnull" json:"model_id"`
	Provider      string    `bun:",notnull" json:"provider"`
	PriceInput    float64   `bun:",notnull,default:0" json:"price_input"`  // USD per 1M tokens
	PriceOutput   float64   `bun:",notnull,default:0" json:"price_output"` // USD per 1M tokens
	CreatedAt     time.Time `bun:",notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt     time.Time `bun:",notnull,default:current_timestamp" json:"updated_at"`
}

type LLMBillingStore interface {
	List(ctx context.Context) ([]LLMBilling, error)
	Create(ctx context.Context, b *LLMBilling) error
	Update(ctx context.Context, b *LLMBilling) error
	FindByID(ctx context.Context, id int64) (*LLMBilling, error)
	Delete(ctx context.Context, id int64) error
	FindByModel(ctx context.Context, provider, modelID string) (*LLMBilling, error)
}

type llmBillingStore struct {
	db *DB
}

func NewLLMBillingStore(cfg *config.Config) LLMBillingStore {
	return &llmBillingStore{db: defaultDB}
}

func (s *llmBillingStore) List(ctx context.Context) ([]LLMBilling, error) {
	var result []LLMBilling
	err := s.db.Operator.Core.NewSelect().Model(&result).Order("provider ASC", "model_id ASC").Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list llm billing: %w", err)
	}
	return result, nil
}

func (s *llmBillingStore) Create(ctx context.Context, b *LLMBilling) error {
	b.CreatedAt = time.Now()
	b.UpdatedAt = time.Now()
	res, err := s.db.Core.NewInsert().Model(b).Exec(ctx, b)
	if err := assertAffectedOneRow(res, err); err != nil {
		return fmt.Errorf("create llm billing: %w", err)
	}
	return nil
}

func (s *llmBillingStore) Update(ctx context.Context, b *LLMBilling) error {
	b.UpdatedAt = time.Now()
	_, err := s.db.Core.NewUpdate().Model(b).Where("id = ?", b.ID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("update llm billing: %w", err)
	}
	return nil
}

func (s *llmBillingStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Operator.Core.NewDelete().Model((*LLMBilling)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete llm billing: %w", err)
	}
	return nil
}

func (s *llmBillingStore) FindByID(ctx context.Context, id int64) (*LLMBilling, error) {
	var b LLMBilling
	err := s.db.Operator.Core.NewSelect().Model(&b).
		Where("id = ?", id).
		Limit(1).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find llm billing by id: %w", err)
	}
	return &b, nil
}

func (s *llmBillingStore) FindByModel(ctx context.Context, provider, modelID string) (*LLMBilling, error) {
	var b LLMBilling
	err := s.db.Operator.Core.NewSelect().Model(&b).
		Where("provider = ? AND model_id = ?", provider, modelID).
		Limit(1).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find llm billing by model: %w", err)
	}
	return &b, nil
}

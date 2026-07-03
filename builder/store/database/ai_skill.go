package database

import (
	"context"
	"fmt"
	"time"
)

// AISkill represents a reusable skill template for the Playground.
// Skills bundle a system prompt + preferred model + metadata.
type AISkill struct {
	ID                   int64          `bun:"id,pk,autoincrement" json:"id"`
	Name                 string         `bun:"name,notnull" json:"name"`
	Description          string         `bun:"description,notnull,default:''" json:"description"`
	SystemPrompt         string         `bun:"system_prompt,notnull" json:"system_prompt"`
	PreferredModel       string         `bun:"preferred_model,notnull,default:''" json:"preferred_model"`
	Icon                 string         `bun:"icon,notnull,default:'🤖'" json:"icon"`
	IsBuiltin            bool           `bun:"is_builtin,notnull,default:false" json:"is_builtin"`
	Enabled              bool           `bun:"enabled,notnull,default:true" json:"enabled"`
	Tools                []any          `bun:"tools,type:jsonb,default:'[]'" json:"tools"`
	MCPServices          []any          `bun:"mcp_services,type:jsonb,default:'[]'" json:"mcp_services"`
	ConversationStarters []string       `bun:"conversation_starters,type:jsonb,default:'[]'" json:"conversation_starters"`
	WelcomeMessage       string         `bun:"welcome_message,default:''" json:"welcome_message"`
	Category             string         `bun:"category,default:'custom'" json:"category"`
	UsageCount           int            `bun:"usage_count,default:0" json:"usage_count"`
	Author               string         `bun:"author,default:''" json:"author"`
	IsPublic             bool           `bun:"is_public,default:true" json:"is_public"`
	CreatedAt            time.Time      `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt            time.Time      `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

type AISkillStore interface {
	List(ctx context.Context, enabledOnly bool) ([]AISkill, error)
	FindByID(ctx context.Context, id int64) (*AISkill, error)
	FindByName(ctx context.Context, name string) (*AISkill, error)
	Create(ctx context.Context, skill *AISkill) error
	Update(ctx context.Context, skill *AISkill) error
	Delete(ctx context.Context, id int64) error
	IncrementUsageCount(ctx context.Context, id int64) error
}

type aiSkillStore struct {
	db *DB
}

func NewAISkillStore() AISkillStore {
	return &aiSkillStore{db: defaultDB}
}

func (s *aiSkillStore) List(ctx context.Context, enabledOnly bool) ([]AISkill, error) {
	var skills []AISkill
	q := s.db.Operator.Core.NewSelect().Model(&skills).Order("is_builtin DESC", "name ASC")
	if enabledOnly {
		q = q.Where("enabled = true")
	}
	err := q.Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list ai skills: %w", err)
	}
	return skills, nil
}

func (s *aiSkillStore) FindByID(ctx context.Context, id int64) (*AISkill, error) {
	var skill AISkill
	err := s.db.Operator.Core.NewSelect().Model(&skill).Where("id = ?", id).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find ai skill: %w", err)
	}
	return &skill, nil
}

func (s *aiSkillStore) Create(ctx context.Context, skill *AISkill) error {
	skill.CreatedAt = time.Now()
	skill.UpdatedAt = time.Now()
	_, err := s.db.Operator.Core.NewInsert().Model(skill).Exec(ctx)
	if err != nil {
		return fmt.Errorf("create ai skill: %w", err)
	}
	return nil
}

func (s *aiSkillStore) Update(ctx context.Context, skill *AISkill) error {
	skill.UpdatedAt = time.Now()
	_, err := s.db.Operator.Core.NewUpdate().Model(skill).WherePK().Exec(ctx)
	if err != nil {
		return fmt.Errorf("update ai skill: %w", err)
	}
	return nil
}

func (s *aiSkillStore) Delete(ctx context.Context, id int64) error {
	_, err := s.db.Operator.Core.NewDelete().Model((*AISkill)(nil)).Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete ai skill: %w", err)
	}
	return nil
}

func (s *aiSkillStore) FindByName(ctx context.Context, name string) (*AISkill, error) {
	var skill AISkill
	err := s.db.Operator.Core.NewSelect().Model(&skill).Where("name = ?", name).Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("find ai skill by name: %w", err)
	}
	return &skill, nil
}

func (s *aiSkillStore) IncrementUsageCount(ctx context.Context, id int64) error {
	_, err := s.db.Operator.Core.NewUpdate().Model((*AISkill)(nil)).
		Set("usage_count = usage_count + 1").
		Where("id = ?", id).Exec(ctx)
	if err != nil {
		return fmt.Errorf("increment skill usage: %w", err)
	}
	return nil
}

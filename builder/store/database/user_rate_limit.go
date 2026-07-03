package database

import (
	"context"
	"fmt"
	"time"
)

// UserRateLimit stores per-user RPM/TPM overrides set by admin.
// If no record exists for a user, the global defaults from config are used.
type UserRateLimit struct {
	ID        int64     `bun:"id,pk,autoincrement" json:"id"`
	UserID    int64     `bun:"user_id,notnull,unique" json:"user_id"`
	Username  string    `bun:"username,notnull" json:"username"`
	RPM       int       `bun:"rpm,notnull" json:"rpm"`        // requests per minute, 0 = unlimited
	TPM       int       `bun:"tpm,notnull" json:"tpm"`        // tokens per minute, 0 = unlimited
	CreatedAt time.Time `bun:"created_at,nullzero,notnull,default:current_timestamp" json:"created_at"`
	UpdatedAt time.Time `bun:"updated_at,nullzero,notnull,default:current_timestamp" json:"updated_at"`
}

type UserRateLimitStore interface {
	FindByUserID(ctx context.Context, userID int64) (*UserRateLimit, error)
	Upsert(ctx context.Context, rl *UserRateLimit) error
	Delete(ctx context.Context, userID int64) error
	List(ctx context.Context) ([]UserRateLimit, error)
}

type userRateLimitStore struct {
	db *DB
}

func NewUserRateLimitStore() UserRateLimitStore {
	return &userRateLimitStore{db: defaultDB}
}

func (s *userRateLimitStore) FindByUserID(ctx context.Context, userID int64) (*UserRateLimit, error) {
	var rl UserRateLimit
	err := s.db.Operator.Core.NewSelect().Model(&rl).Where("user_id = ?", userID).Scan(ctx)
	if err != nil {
		return nil, err
	}
	return &rl, nil
}

func (s *userRateLimitStore) Upsert(ctx context.Context, rl *UserRateLimit) error {
	rl.UpdatedAt = time.Now()
	if rl.CreatedAt.IsZero() {
		rl.CreatedAt = time.Now()
	}
	_, err := s.db.Operator.Core.NewInsert().Model(rl).
		On("CONFLICT (user_id) DO UPDATE").
		Set("rpm = EXCLUDED.rpm").
		Set("tpm = EXCLUDED.tpm").
		Set("username = EXCLUDED.username").
		Set("updated_at = EXCLUDED.updated_at").
		Exec(ctx)
	if err != nil {
		return fmt.Errorf("upsert user rate limit: %w", err)
	}
	return nil
}

func (s *userRateLimitStore) Delete(ctx context.Context, userID int64) error {
	_, err := s.db.Operator.Core.NewDelete().Model((*UserRateLimit)(nil)).Where("user_id = ?", userID).Exec(ctx)
	if err != nil {
		return fmt.Errorf("delete user rate limit: %w", err)
	}
	return nil
}

func (s *userRateLimitStore) List(ctx context.Context) ([]UserRateLimit, error) {
	var rls []UserRateLimit
	err := s.db.Operator.Core.NewSelect().Model(&rls).Order("username ASC").Scan(ctx)
	if err != nil {
		return nil, fmt.Errorf("list user rate limits: %w", err)
	}
	return rls, nil
}

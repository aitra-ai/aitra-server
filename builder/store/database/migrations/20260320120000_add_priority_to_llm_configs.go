package migrations

import (
	"context"

	"github.com/uptrace/bun"
)

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.ExecContext(ctx, `ALTER TABLE llm_configs ADD COLUMN IF NOT EXISTS priority INT NOT NULL DEFAULT 0`)
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		_, err := db.ExecContext(ctx, `ALTER TABLE llm_configs DROP COLUMN IF EXISTS priority`)
		return err
	})
}

package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type userRateLimit struct {
	bun.BaseModel `bun:"table:user_rate_limits"`
	ID            int64     `bun:",pk,autoincrement"`
	UserID        int64     `bun:",notnull,unique"`
	Username      string    `bun:",notnull"`
	RPM           int       `bun:",notnull,default:0"`
	TPM           int       `bun:",notnull,default:0"`
	CreatedAt     time.Time `bun:",notnull,default:current_timestamp"`
	UpdatedAt     time.Time `bun:",notnull,default:current_timestamp"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewCreateTable().Model((*userRateLimit)(nil)).IfNotExists().Exec(ctx)
		return err
	}, func(ctx context.Context, db *bun.DB) error {
		_, err := db.NewDropTable().Model((*userRateLimit)(nil)).IfExists().Exec(ctx)
		return err
	})
}

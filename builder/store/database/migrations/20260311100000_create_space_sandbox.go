package migrations

import (
	"context"
	"time"

	"github.com/uptrace/bun"
)

type spaceSandboxInstance struct {
	bun.BaseModel `bun:"table:space_sandbox_instances"`
	ID           int64      `bun:",pk,autoincrement"`
	SpacePath    string     `bun:",notnull"`
	Template     string     `bun:",notnull"`
	UserID       int64      `bun:",notnull,default:0"`
	Username     string     `bun:",notnull,default:''"`
	Status       string     `bun:",notnull,default:'pending'"`
	ContainerID  string     `bun:",nullzero"`
	Port         int        `bun:",nullzero"`
	AccessURL    string     `bun:",nullzero"`
	ErrorMsg     string     `bun:",nullzero"`
	IsHotPool    bool       `bun:",notnull,default:false"`
	StartedAt    *time.Time `bun:",nullzero"`
	LastActiveAt *time.Time `bun:",nullzero"`
	ExpiresAt    *time.Time `bun:",nullzero"`
	CreatedAt    time.Time  `bun:",notnull,default:current_timestamp"`
}

type featuredSpace struct {
	bun.BaseModel `bun:"table:featured_spaces"`
	ID          int64     `bun:",pk,autoincrement"`
	SpacePath   string    `bun:",notnull,unique"`
	Template    string    `bun:",notnull"`
	DisplayName string    `bun:",notnull,default:''"`
	Description string    `bun:",notnull,default:''"`
	CoverURL    string    `bun:",nullzero"`
	HotPool     int       `bun:",notnull,default:1"`
	TTLSeconds  int       `bun:",notnull,default:1800"`
	EnvVars     string    `bun:",nullzero"`
	Enabled     bool      `bun:",notnull,default:true"`
	SortOrder   int       `bun:",notnull,default:0"`
	CreatedAt   time.Time `bun:",notnull,default:current_timestamp"`
	UpdatedAt   time.Time `bun:",notnull,default:current_timestamp"`
}

func init() {
	Migrations.MustRegister(func(ctx context.Context, db *bun.DB) error {
		return createTables(ctx, db, spaceSandboxInstance{}, featuredSpace{})
	}, func(ctx context.Context, db *bun.DB) error {
		return dropTables(ctx, db, spaceSandboxInstance{}, featuredSpace{})
	})
}

package component

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
)

type LfsComponent interface {
	DispatchLfsXnetProgress() error
	DispatchLfsXnetResult() error
	PublishLfsMigrationMessage(ctx context.Context, repo *database.Repository, oid string) error
}

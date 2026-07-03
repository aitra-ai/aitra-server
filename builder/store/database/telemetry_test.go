package database_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/tests"
)

func TestTelemetryStore_Save(t *testing.T) {
	db := tests.InitTestDB()
	defer db.Close()
	ctx := context.TODO()

	store := database.NewTelemetryStoreWithDB(db)
	err := store.Save(ctx, &database.Telemetry{
		UUID: "foo",
	})
	require.Nil(t, err)

}

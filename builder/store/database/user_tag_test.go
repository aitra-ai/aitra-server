package database_test

import (
	"context"
	"testing"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/tests"
)

func TestUserTagStoreImpl_ResetUserTags(t *testing.T) {
	ctx := context.Background()
	testDB := tests.InitTestDB()
	userTagStore := database.NewUserTagStoreWithDB(testDB)

	userId := int64(1)
	tagIDs := []int64{1, 2, 3}

	t.Run("ResetUserTags", func(t *testing.T) {
		err := userTagStore.ResetUserTags(ctx, userId, tagIDs)
		if err != nil {
			t.Errorf("ResetUserTags failed, err: %v", err)
		}
	})

}

package component

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

func TestEventComponent_NewEvent(t *testing.T) {
	ctx := context.TODO()
	ec := initializeTestEventComponent(ctx, t)

	ec.mocks.stores.EventMock().EXPECT().BatchSave(ctx, []database.Event{
		{EventID: "e1"},
	}).Return(nil)

	err := ec.NewEvents(ctx, []types.Event{{ID: "e1"}})
	require.Nil(t, err)
}

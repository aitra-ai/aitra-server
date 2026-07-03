package component

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	mockdb "github.com/aitra-ai/aitra-server/_mocks/github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

func NewTestMCPResourceComponent(mcpResStore database.MCPResourceStore) MCPResourceComponent {
	mrc := &mcpResourceComponentImpl{
		mcpResStore: mcpResStore,
	}
	return mrc
}

func TestMCPResourceComponent_List(t *testing.T) {
	ctx := context.TODO()

	filter := &types.MCPFilter{
		Per:  10,
		Page: 1,
	}

	resStore := mockdb.NewMockMCPResourceStore(t)
	resStore.EXPECT().List(ctx, filter).Return([]database.MCPResource{
		{
			ID:   1,
			Name: "test-name",
		},
	}, 1, nil)

	testComp := NewTestMCPResourceComponent(resStore)

	resList, total, err := testComp.List(ctx, filter)
	require.Nil(t, err)
	require.Equal(t, 1, total)
	require.Len(t, resList, 1)
}

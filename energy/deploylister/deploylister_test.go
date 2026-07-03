package deploylister

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"opencsg.com/csghub-server/builder/store/database"
)

type fakeReader struct {
	deploys []database.Deploy
	err     error
}

func (f *fakeReader) ListRunningInference(context.Context) ([]database.Deploy, error) {
	return f.deploys, f.err
}

func TestToDeploymentMapsFields(t *testing.T) {
	d := database.Deploy{
		ID: 7, ClusterID: "cl-1", OwnerNamespace: "team-a", ClusterNode: "node-3",
		GitPath: "org/qwen", SKU: "h100", SvcName: "qwen-svc",
	}
	got := toDeployment(d)
	require.Equal(t, int64(7), got.ID)
	require.Equal(t, "cl-1", got.Cluster)
	require.Equal(t, "team-a", got.Namespace)
	require.Equal(t, "node-3", got.Node)
	require.Equal(t, "org/qwen", got.Model)
	require.Equal(t, "h100", got.Hardware)
	require.Equal(t, "qwen-svc", got.MeteringKey, "token join key = SvcName")
	require.Equal(t, "^qwen-svc-", got.Scope.PodSelector, "pod regex isolates the deployment's pods")
	require.Empty(t, got.Scope.Node, "no node filter, so multi-node replicas are not undercounted")
}

func TestListRunningMapsAll(t *testing.T) {
	fr := &fakeReader{deploys: []database.Deploy{
		{ID: 1, SvcName: "a", GitPath: "org/a"},
		{ID: 2, SvcName: "b", GitPath: "org/b"},
	}}
	l := &Lister{reader: fr}

	got, err := l.ListRunning(context.Background())
	require.NoError(t, err)
	require.Len(t, got, 2)
	require.Equal(t, "^a-", got[0].Scope.PodSelector)
	require.Equal(t, "b", got[1].MeteringKey)
}

func TestPodSelectorEmptySvc(t *testing.T) {
	require.Empty(t, podSelector(""), "no svc name -> no selector")
}

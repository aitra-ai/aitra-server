package component

import (
	"context"
	"net/url"
	"time"

	"github.com/aitra-ai/aitra-server/builder/rpc"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

type XnetComponent interface {
	XnetToken(ctx context.Context, req *types.XnetTokenReq) (*types.XnetTokenResp, error)
	PresignedGetObject(ctx context.Context, objectKey string, expireTime time.Duration, params url.Values) (*url.URL, error)
}

type XnetComponentImpl struct {
	repoStore      database.RepoStore
	xnetClient     rpc.XnetSvcClient
	userStore      database.UserStore
	namespaceStore database.NamespaceStore
	repoComp       RepoComponent
}

package rpc

import (
	"context"
	"net/url"
	"time"

	"github.com/aitra-ai/aitra-server/common/types"
)

type XnetSvcClient interface {
	GenerateWriteToken(ctx context.Context, req *types.XnetTokenReq) (*types.XnetTokenResp, error)
	PresignedGetObject(ctx context.Context, objectKey string, expireTime time.Duration, params url.Values) (*url.URL, error)
	FileExists(ctx context.Context, req *types.XetFileExistsReq) (bool, error)
	GetMigrationStats(ctx context.Context) (*types.MigrationStatsResp, error)
}

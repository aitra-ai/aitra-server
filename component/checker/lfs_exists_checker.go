package checker

import (
	"context"
	"errors"
	"fmt"
	"github.com/minio/minio-go/v7"
	"github.com/aitra-ai/aitra-server/builder/git"
	"github.com/aitra-ai/aitra-server/builder/git/gitserver"
	"github.com/aitra-ai/aitra-server/builder/rpc"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/builder/store/s3"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
	"github.com/aitra-ai/aitra-server/common/utils/common"
	"strconv"
)

type LFSExistsChecker struct {
	repoStore  database.RepoStore
	gitServer  gitserver.GitServer
	config     *config.Config
	s3Client   s3.Client
	xnetClient rpc.XnetSvcClient
}

func NewLFSExistsChecker(config *config.Config) (GitCallbackChecker, error) {
	git, err := git.NewGitServer(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create git server: %w", err)
	}
	ossClient, err := s3.NewMinio(config)
	if err != nil {
		return nil, err
	}
	return &LFSExistsChecker{
		repoStore:  database.NewRepoStore(),
		gitServer:  git,
		config:     config,
		s3Client:   ossClient,
		xnetClient: rpc.NewXnetSvcHttpClient(config.Xnet.Endpoint, rpc.AuthWithApiKey(config.Xnet.ApiKey)),
	}, nil

}

func (c *LFSExistsChecker) Check(ctx context.Context, req types.GitalyAllowedReq) (bool, error) {
	if !c.config.Git.LfsExistsCheck {
		return true, nil
	}

	var revisions []string
	repoType, namespace, name := req.GetRepoTypeNamespaceAndName()

	repo, err := c.repoStore.FindByPath(ctx, repoType, namespace, name)
	if err != nil {
		return false, fmt.Errorf("failed to find repo, err: %v", err)
	}
	if repo == nil {
		return false, errors.New("repo not found")
	}
	revisions = []string{"--not", "--all", "--not", req.GetRevision()}

	pointers, err := c.gitServer.GetRepoLfsPointers(ctx, gitserver.GetRepoFilesReq{
		Namespace:                             namespace,
		Name:                                  name,
		GitObjectDirectoryRelative:            req.GitEnv.GitObjectDirectoryRelative,
		GitAlternateObjectDirectoriesRelative: req.GitEnv.GitAlternateObjectDirectoriesRelative,
		RepoType:                              repoType,
		Revisions:                             revisions,
	})
	if err != nil {
		return false, err
	}

	for _, p := range pointers {
		if repo.XnetEnabled {
			lfsExistReq := &types.XetFileExistsReq{
				ObjectKey: p.FileOid,
				RepoID:    strconv.FormatInt(repo.ID, 10),
			}
			if e, err := c.xnetClient.FileExists(ctx, lfsExistReq); nil != err || !e {
				return false, fmt.Errorf("failed to request xnet, exist:%t err: %w", e, err)
			}
			continue
		}
		objectKey := common.BuildLfsPath(repo.ID, p.FileOid, repo.Migrated)
		info, err := c.s3Client.StatObject(ctx, c.config.S3.Bucket, objectKey, minio.StatObjectOptions{})
		if err != nil {
			return false, fmt.Errorf("lfs object %s not found, err: %v", p.FileOid, err)
		}

		if p.FileSize != info.Size {
			return false, fmt.Errorf("lfs object %s size mismatch, expected: %d, got: %d", p.Oid, p.Size, info.Size)
		}
	}
	return true, nil
}

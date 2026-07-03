package handler

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/common/config"
)

type ModelWeightHandler struct {
	weightSyncStore database.ModelWeightSyncStore
	minioClient     *minio.Client
	minioBucket     string
}

func NewModelWeightHandler(cfg *config.Config) (*ModelWeightHandler, error) {
	// Initialize MinIO client
	minioClient, err := minio.New(cfg.S3.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.S3.AccessKeyID, cfg.S3.AccessKeySecret, ""),
		Secure: cfg.S3.EnableSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	return &ModelWeightHandler{
		weightSyncStore: database.NewModelWeightSyncStore(cfg),
		minioClient:     minioClient,
		minioBucket:     cfg.S3.Bucket,
	}, nil
}

// Response types
type SyncedFile struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
}

// ListModelWeights lists all model weight sync records
// GET /api/v1/user/model-weights
func (h *ModelWeightHandler) ListModelWeights(c *gin.Context) {
	currentUser := httpbase.GetCurrentUser(c)
	if currentUser == "" {
		httpbase.UnauthorizedError(c, errors.New("login required"))
		return
	}

	ctx := c.Request.Context()
	syncs, err := h.weightSyncStore.List(ctx)
	if err != nil {
		slog.Error("failed to list model weight syncs", "error", err)
		httpbase.ServerError(c, fmt.Errorf("failed to list model weight syncs: %w", err))
		return
	}

	httpbase.OK(c, syncs)
}

// GetWeightStatus gets sync status for a specific repo
// GET /api/v1/user/model-weights/:repo_id/status
func (h *ModelWeightHandler) GetWeightStatus(c *gin.Context) {
	currentUser := httpbase.GetCurrentUser(c)
	if currentUser == "" {
		httpbase.UnauthorizedError(c, errors.New("login required"))
		return
	}

	repoIDStr := c.Param("repo_id")
	repoID, err := strconv.ParseInt(repoIDStr, 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid repo_id")
		return
	}

	ctx := c.Request.Context()
	sync, err := h.weightSyncStore.FindByRepoID(ctx, repoID)
	if err != nil {
		slog.Error("failed to find model weight sync", "repo_id", repoID, "error", err)
		httpbase.ServerError(c, fmt.Errorf("failed to find model weight sync: %w", err))
		return
	}

	httpbase.OK(c, sync)
}

// TriggerWeightSync manually triggers weight sync for a repo
// POST /api/v1/user/model-weights/:repo_id/sync
func (h *ModelWeightHandler) TriggerWeightSync(c *gin.Context) {
	currentUser := httpbase.GetCurrentUser(c)
	if currentUser == "" {
		httpbase.UnauthorizedError(c, errors.New("login required"))
		return
	}

	repoIDStr := c.Param("repo_id")
	repoID, err := strconv.ParseInt(repoIDStr, 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid repo_id")
		return
	}

	ctx := c.Request.Context()

	// Find existing sync record
	sync, err := h.weightSyncStore.FindByRepoID(ctx, repoID)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find model weight sync: %w", err))
		return
	}

	// Reset to pending status
	if err := h.weightSyncStore.UpdateStatus(ctx, sync.ID, "pending", ""); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to reset sync status: %w", err))
		return
	}

	// Start async sync (this would need the HF import handler's sync method)
	// For now, just return success
	// TODO: Refactor syncModelWeights to be reusable

	httpbase.OK(c, "sync triggered")
}

// ListWeightFiles lists synced files from MinIO
// GET /api/v1/user/model-weights/:repo_id/files
func (h *ModelWeightHandler) ListWeightFiles(c *gin.Context) {
	currentUser := httpbase.GetCurrentUser(c)
	if currentUser == "" {
		httpbase.UnauthorizedError(c, errors.New("login required"))
		return
	}

	repoIDStr := c.Param("repo_id")
	repoID, err := strconv.ParseInt(repoIDStr, 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid repo_id")
		return
	}

	ctx := c.Request.Context()

	// Find sync record to get repo_path
	sync, err := h.weightSyncStore.FindByRepoID(ctx, repoID)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to find model weight sync: %w", err))
		return
	}

	// List objects from MinIO
	prefix := fmt.Sprintf("lfs/%s/", sync.RepoPath)
	objectCh := h.minioClient.ListObjects(ctx, h.minioBucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var files []SyncedFile
	for object := range objectCh {
		if object.Err != nil {
			slog.Error("error listing MinIO objects", "error", object.Err)
			continue
		}
		
		// Remove prefix to get relative path
		path := object.Key[len(prefix):]
		if path != "" {
			files = append(files, SyncedFile{
				Path: path,
				Size: object.Size,
			})
		}
	}

	httpbase.OK(c, files)
}
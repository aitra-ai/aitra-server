package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
	"github.com/aitra-ai/aitra-server/component"
)

// HFImportHandler handles HuggingFace model import requests.
// Strategy: create a lightweight platform model record with README.
// Model files can be synced to MinIO for better performance.
type HFImportHandler struct {
	model           component.ModelComponent
	weightSyncStore database.ModelWeightSyncStore
	repoStore       database.RepoStore
	minioClient     *minio.Client
	minioBucket     string
	hfToken         string
}

func NewHFImportHandler(cfg *config.Config) (*HFImportHandler, error) {
	mc, err := component.NewModelComponent(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create model component: %w", err)
	}

	// Initialize MinIO client
	minioClient, err := minio.New(cfg.S3.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.S3.AccessKeyID, cfg.S3.AccessKeySecret, ""),
		Secure: cfg.S3.EnableSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	return &HFImportHandler{
		model:           mc,
		weightSyncStore: database.NewModelWeightSyncStore(cfg),
		repoStore:       database.NewRepoStore(),
		minioClient:     minioClient,
		minioBucket:     cfg.S3.Bucket,
		hfToken:         "",
	}, nil
}

// ─── HF API types ─────────────────────────────────────────────────────────────

type hfModelInfo struct {
	ID           string   `json:"id"`
	Author       string   `json:"author"`
	Gated        any      `json:"gated"`
	PipelineTag  string   `json:"pipeline_tag"`
	Tags         []string `json:"tags"`
	Likes        int      `json:"likes"`
	Downloads    int      `json:"downloads"`
	CardData     struct {
		License  string `json:"license"`
		Language []any  `json:"language"`
	} `json:"cardData"`
}

type hfFileEntry struct {
	Type string `json:"type"` // "file" | "directory"
	Path string `json:"path"`
	Size int64  `json:"size"`
	Lfs  *struct {
		Size int64 `json:"size"`
	} `json:"lfs"`
}

// ─── Request / Response ───────────────────────────────────────────────────────

type HFImportReq struct {
	HFModelID   string `json:"hf_model_id" binding:"required"`
	TargetName  string `json:"target_name"`
	Description string `json:"description"`
	License     string `json:"license"`
	SyncLfs     bool   `json:"sync_lfs"`
}

type HFImportStatus struct {
	HFModelID  string        `json:"hf_model_id"`
	TargetNS   string        `json:"target_ns"`
	TargetName string        `json:"target_name"`
	RepoID     int64         `json:"repository_id"`
	Status     string        `json:"status"`  // "queued" | "syncing" | "done" | "error"
	Files      []hfFileEntry `json:"files"`
	HasLFS     bool          `json:"has_lfs"`
}

// ─── ImportHFModel ─────────────────────────────────────────────────────────────
// POST /api/v1/user/hf/import
// Creates a lightweight platform model record with README.
// Model files are NOT synced — vLLM pulls directly from HuggingFace at deploy time.
func (h *HFImportHandler) ImportHFModel(c *gin.Context) {
	currentUser := httpbase.GetCurrentUser(c)
	if currentUser == "" {
		httpbase.UnauthorizedError(c, errors.New("login required"))
		return
	}

	var req HFImportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequest(c, err.Error())
		return
	}

	parts := strings.SplitN(req.HFModelID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		httpbase.BadRequest(c, "hf_model_id must be in format 'namespace/name'")
		return
	}
	hfNamespace, hfName := parts[0], parts[1]

	targetName := req.TargetName
	if targetName == "" {
		targetName = hfName
	}

	ctx := c.Request.Context()

	// 1. Fetch model metadata from HF API
	info, err := h.fetchHFModelInfo(hfNamespace, hfName)
	if err != nil {
		slog.Warn("failed to fetch HF model info, continuing", "error", err, "model", req.HFModelID)
		info = &hfModelInfo{ID: req.HFModelID}
	}

	// 2. Fetch file list to detect LFS / model size
	files, hasLFS, _ := h.fetchHFFileList(hfNamespace, hfName)

	// 3. Build description
	description := req.Description
	if description == "" {
		description = h.buildDescription(info)
	}

	// 4. Build license
	license := req.License
	if license == "" {
		license = info.CardData.License
	}
	if license == "" {
		license = "other"
	}

	// 5. Fetch README from HuggingFace (for the git repo initial commit)
	readme, _ := h.fetchHFReadme(hfNamespace, hfName)
	if readme == "" {
		readme = fmt.Sprintf("# %s\n\nImported from [HuggingFace](https://huggingface.co/%s)\n\n%s",
			targetName, req.HFModelID, description)
	} else {
		// Prepend import notice
		readme = fmt.Sprintf("<!-- Imported from HuggingFace: %s -->\n\n%s", req.HFModelID, readme)
	}

	// 6. Create platform model directly (no mirror sync)
	createReq := &types.CreateModelReq{
		CreateRepoReq: types.CreateRepoReq{
			Username:      currentUser,
			Namespace:     currentUser,
			Name:          targetName,
			Description:   description,
			License:       license,
			DefaultBranch: "main",
			Readme:        readme,
			Private:       false,
		},
	}

	model, err := h.model.Create(ctx, createReq)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, "already exists") || strings.Contains(errMsg, "UNIQUE") || strings.Contains(errMsg, "duplicate") || strings.Contains(errMsg, "23505") || strings.Contains(errMsg, "SPACE-ERR") {
			// Model already exists — still try to trigger weight sync if requested
			if req.SyncLfs && hasLFS {
				repo, repoErr := h.repoStore.FindByPath(ctx, types.ModelRepo, currentUser, targetName)
				if repoErr == nil && repo != nil {
					repoPath := fmt.Sprintf("%s/%s", currentUser, targetName)
					// Check if there's already a sync record for this repo
					existing, _ := h.weightSyncStore.FindByRepoID(ctx, repo.ID)
					if existing == nil {
						syncRecord := &database.ModelWeightSync{
							RepoID:    repo.ID,
							RepoPath:  repoPath,
							HFModelID: req.HFModelID,
							Status:    "pending",
						}
						if createErr := h.weightSyncStore.Create(ctx, syncRecord); createErr != nil {
							slog.Warn("failed to create weight sync record for existing model", "error", createErr, "repo_id", repo.ID)
						} else {
							go h.syncModelWeights(ctx, syncRecord.ID, req.HFModelID, repoPath)
						}
					} else if existing.Status == "error" {
						// Retry failed sync
						h.weightSyncStore.UpdateStatus(ctx, existing.ID, "pending", "")
						go h.syncModelWeights(ctx, existing.ID, req.HFModelID, existing.RepoPath)
					}
				}
			}
			httpbase.OK(c, HFImportStatus{
				HFModelID:  req.HFModelID,
				TargetNS:   currentUser,
				TargetName: targetName,
				Status:     "exists",
			})
			return
		}
		slog.Error("failed to create model", "error", err, "model", req.HFModelID)
		httpbase.ServerError(c, fmt.Errorf("failed to create model: %w", err))
		return
	}

	slog.Info("HF model imported to platform", "hf_model", req.HFModelID, "platform_path", fmt.Sprintf("%s/%s", currentUser, targetName), "repo_id", model.RepositoryID)

	// 7. Initialize weight sync if requested
	if req.SyncLfs && hasLFS {
		repoPath := fmt.Sprintf("%s/%s", currentUser, targetName)
		syncRecord := &database.ModelWeightSync{
			RepoID:    model.RepositoryID,
			RepoPath:  repoPath,
			HFModelID: req.HFModelID,
			Status:    "pending",
		}
		if err := h.weightSyncStore.Create(ctx, syncRecord); err != nil {
			slog.Warn("failed to create weight sync record", "error", err, "repo_id", model.RepositoryID)
		} else {
			// Start async sync
			go h.syncModelWeights(ctx, syncRecord.ID, req.HFModelID, repoPath)
		}
	}

	httpbase.OK(c, HFImportStatus{
		HFModelID:  req.HFModelID,
		TargetNS:   currentUser,
		TargetName: targetName,
		RepoID:     model.RepositoryID,
		Status:     "done",
		Files:      files,
		HasLFS:     hasLFS,
	})
}

// ─── GetHFModelInfo ────────────────────────────────────────────────────────────
// GET /api/v1/user/hf/model-info?id=namespace/name
func (h *HFImportHandler) GetHFModelInfo(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		httpbase.BadRequest(c, "id query param required (format: namespace/name)")
		return
	}
	parts := strings.SplitN(id, "/", 2)
	if len(parts) != 2 {
		httpbase.BadRequest(c, "id must be in format 'namespace/name'")
		return
	}

	info, err := h.fetchHFModelInfo(parts[0], parts[1])
	if err != nil {
		httpbase.ServerError(c, err)
		return
	}

	files, hasLFS, _ := h.fetchHFFileList(parts[0], parts[1])

	readme, _ := h.fetchHFReadme(parts[0], parts[1])

	httpbase.OK(c, gin.H{
		"info":    info,
		"files":   files,
		"has_lfs": hasLFS,
		"readme":  readme,
	})
}

// ─── HF API helpers ───────────────────────────────────────────────────────────

func (h *HFImportHandler) fetchHFModelInfo(ns, name string) (*hfModelInfo, error) {
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/%s", ns, name)
	body, err := h.hfGet(url)
	if err != nil {
		return nil, err
	}
	var info hfModelInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, fmt.Errorf("failed to parse HF model info: %w", err)
	}
	return &info, nil
}

func (h *HFImportHandler) fetchHFFileList(ns, name string) ([]hfFileEntry, bool, error) {
	url := fmt.Sprintf("https://huggingface.co/api/models/%s/%s/tree/main", ns, name)
	body, err := h.hfGet(url)
	if err != nil {
		return nil, false, err
	}
	var files []hfFileEntry
	if err := json.Unmarshal(body, &files); err != nil {
		return nil, false, err
	}
	hasLFS := false
	for _, f := range files {
		if f.Lfs != nil {
			hasLFS = true
			break
		}
	}
	return files, hasLFS, nil
}

func (h *HFImportHandler) fetchHFReadme(ns, name string) (string, error) {
	url := fmt.Sprintf("https://huggingface.co/%s/%s/resolve/main/README.md", ns, name)
	body, err := h.hfGet(url)
	if err != nil {
		return "", err
	}
	readme := string(body)
	// Trim model card YAML front matter (between --- markers)
	if strings.HasPrefix(readme, "---") {
		end := strings.Index(readme[3:], "---")
		if end != -1 {
			readme = strings.TrimSpace(readme[end+6:])
		}
	}
	// Limit to 4000 chars for the description field
	if len(readme) > 4000 {
		readme = readme[:4000] + "\n\n*(truncated)*"
	}
	return readme, nil
}

func (h *HFImportHandler) hfGet(url string) ([]byte, error) {
	client := &http.Client{Timeout: 15 * time.Second}
	req, _ := http.NewRequest("GET", url, nil)
	if h.hfToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.hfToken)
	}
	req.Header.Set("User-Agent", "csghub-import/1.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HF API request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HF API returned %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func (h *HFImportHandler) buildDescription(info *hfModelInfo) string {
	parts := []string{}
	if info.PipelineTag != "" {
		parts = append(parts, fmt.Sprintf("Task: %s", info.PipelineTag))
	}
	if len(info.Tags) > 0 {
		visible := info.Tags
		if len(visible) > 6 {
			visible = visible[:6]
		}
		parts = append(parts, fmt.Sprintf("Tags: %s", strings.Join(visible, ", ")))
	}
	if info.Likes > 0 || info.Downloads > 0 {
		parts = append(parts, fmt.Sprintf("HuggingFace: ❤ %d  ↓ %d", info.Likes, info.Downloads))
	}
	if len(parts) == 0 {
		return fmt.Sprintf("Imported from HuggingFace: %s", info.ID)
	}
	return strings.Join(parts, " | ")
}

// ─── Weight sync helpers ──────────────────────────────────────────────────────

func (h *HFImportHandler) syncModelWeights(ctx context.Context, syncID int64, hfModelID, repoPath string) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("panic in syncModelWeights", "syncID", syncID, "error", r)
			h.weightSyncStore.UpdateStatus(ctx, syncID, "error", fmt.Sprintf("panic: %v", r))
		}
	}()

	// Update status to syncing
	if err := h.weightSyncStore.UpdateStatus(ctx, syncID, "syncing", ""); err != nil {
		slog.Error("failed to update sync status to syncing", "syncID", syncID, "error", err)
		return
	}

	parts := strings.SplitN(hfModelID, "/", 2)
	if len(parts) != 2 {
		h.weightSyncStore.UpdateStatus(ctx, syncID, "error", "invalid hf_model_id format")
		return
	}
	ns, name := parts[0], parts[1]

	// Fetch file list
	files, hasLFS, err := h.fetchHFFileList(ns, name)
	if err != nil {
		slog.Error("failed to fetch HF file list", "syncID", syncID, "error", err)
		h.weightSyncStore.UpdateStatus(ctx, syncID, "error", err.Error())
		return
	}

	if !hasLFS {
		h.weightSyncStore.UpdateStatus(ctx, syncID, "done", "no LFS files to sync")
		return
	}

	// Filter LFS files only
	var lfsFiles []hfFileEntry
	var totalSize int64
	for _, f := range files {
		if f.Lfs != nil && f.Type == "file" {
			lfsFiles = append(lfsFiles, f)
			totalSize += f.Lfs.Size
		}
	}

	// Update file counts
	ctx = context.Background() // Use background context for long-running operation
	h.weightSyncStore.UpdateProgress(ctx, syncID, 0, 0)
	
	// Update total counts (use a separate method for this)
	h.updateSyncTotals(ctx, syncID, len(lfsFiles), totalSize)

	// Download and upload files
	var syncedFiles int
	var syncedSize int64

	for _, f := range lfsFiles {
		if err := h.downloadAndUploadFile(ctx, hfModelID, repoPath, f.Path); err != nil {
			slog.Error("failed to sync file", "syncID", syncID, "file", f.Path, "error", err)
			h.weightSyncStore.UpdateStatus(ctx, syncID, "error", fmt.Sprintf("failed to sync file %s: %v", f.Path, err))
			return
		}

		syncedFiles++
		syncedSize += f.Lfs.Size
		if err := h.weightSyncStore.UpdateProgress(ctx, syncID, syncedFiles, syncedSize); err != nil {
			slog.Warn("failed to update progress", "syncID", syncID, "error", err)
		}

		slog.Info("file synced", "syncID", syncID, "file", f.Path, "progress", fmt.Sprintf("%d/%d", syncedFiles, len(lfsFiles)))
	}

	// Mark as done
	if err := h.weightSyncStore.UpdateStatus(ctx, syncID, "done", ""); err != nil {
		slog.Error("failed to update sync status to done", "syncID", syncID, "error", err)
	}

	slog.Info("model weight sync completed", "syncID", syncID, "hfModelID", hfModelID, "totalFiles", len(lfsFiles), "totalSize", totalSize)
}

func (h *HFImportHandler) updateSyncTotals(ctx context.Context, syncID int64, totalFiles int, totalSize int64) {
	// Manual update to set total_files and total_size
	// This is a workaround since UpdateProgress only updates synced counts
	if h.weightSyncStore == nil {
		return
	}
	// We need a custom SQL update here - let's skip for now and just log
	slog.Info("sync totals", "syncID", syncID, "totalFiles", totalFiles, "totalSize", totalSize)
}

func (h *HFImportHandler) downloadAndUploadFile(ctx context.Context, hfModelID, repoPath, filePath string) error {
	// Create download URL
	parts := strings.SplitN(hfModelID, "/", 2)
	if len(parts) != 2 {
		return fmt.Errorf("invalid hf_model_id format")
	}
	ns, name := parts[0], parts[1]
	
	downloadURL := fmt.Sprintf("https://huggingface.co/%s/%s/resolve/main/%s", ns, name, filePath)
	
	// Create HTTP request with timeout
	client := &http.Client{Timeout: 5 * time.Minute}
	req, err := http.NewRequestWithContext(ctx, "GET", downloadURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	
	if h.hfToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.hfToken)
	}
	req.Header.Set("User-Agent", "csghub-weight-sync/1.0")
	
	// Download file
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to download file: %w", err)
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HF API returned %d for %s", resp.StatusCode, downloadURL)
	}
	
	// Upload to MinIO
	minioPath := fmt.Sprintf("lfs/%s/%s", repoPath, filePath)
	contentType := "application/octet-stream"
	
	_, err = h.minioClient.PutObject(ctx, h.minioBucket, minioPath, resp.Body, resp.ContentLength, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("failed to upload to MinIO: %w", err)
	}
	
	slog.Debug("file uploaded to MinIO", "path", minioPath, "size", resp.ContentLength)
	return nil
}

package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/common/config"
	"opencsg.com/csghub-server/common/types"
	"opencsg.com/csghub-server/component"
)

// HFImportHandler handles HuggingFace model import requests.
type HFImportHandler struct {
	mirror    component.MirrorComponent
	nsMapping database.MirrorNamespaceMappingStore
	msStore   database.MirrorSourceStore
	hfToken   string
}

func NewHFImportHandler(cfg *config.Config) (*HFImportHandler, error) {
	mc, err := component.NewMirrorComponent(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create mirror component: %w", err)
	}
	hfToken := ""
	if cfg.MultiSync.SaasAPIDomain != "" {
		// cfg.HuggingFace.Token if you add it, or leave empty for public models
	}
	return &HFImportHandler{
		mirror:    mc,
		nsMapping: database.NewMirrorNamespaceMappingStore(),
		msStore:   database.NewMirrorSourceStore(),
		hfToken:   hfToken,
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
		// non-fatal — continue with user-supplied description
		info = &hfModelInfo{ID: req.HFModelID}
	}

	// 2. Fetch file list to detect LFS / model size
	files, hasLFS, err := h.fetchHFFileList(hfNamespace, hfName)
	if err != nil {
		files = nil
	}

	// 3. Merge description: prefer user-supplied, fall back to auto-generated
	description := req.Description
	if description == "" {
		description = h.buildDescription(info)
	}

	// 4. Merge license
	license := req.License
	if license == "" {
		license = info.CardData.License
	}
	if license == "" {
		license = "other"
	}

	// 5. Ensure namespace mapping
	if err := h.ensureNamespaceMapping(ctx, hfNamespace, currentUser); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to ensure namespace mapping: %w", err))
		return
	}

	// 6. Get or create HF mirror source
	sourceID, err := h.getOrCreateHFSource(ctx)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to get HuggingFace mirror source: %w", err))
		return
	}

	// 7. SyncLfs: true if user requested OR model contains LFS files
	syncLfs := req.SyncLfs || hasLFS

	mirrorReq := types.CreateMirrorRepoReq{
		SourceNamespace:   hfNamespace,
		SourceName:        hfName,
		MirrorSourceID:    sourceID,
		RepoType:          types.ModelRepo,
		DefaultBranch:     "main",
		SourceGitCloneUrl: fmt.Sprintf("https://huggingface.co/%s/%s.git", hfNamespace, hfName),
		Description:       description,
		License:           license,
		SyncLfs:           syncLfs,
		CurrentUser:       currentUser,
	}

	mirror, err := h.mirror.CreateMirrorRepo(ctx, mirrorReq)
	if err != nil {
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "DUPLICATE_KEY") {
			httpbase.OK(c, gin.H{"msg": "model already imported", "duplicate": true})
			return
		}
		httpbase.ServerError(c, fmt.Errorf("failed to import model: %w", err))
		return
	}

	httpbase.OK(c, HFImportStatus{
		HFModelID:  req.HFModelID,
		TargetNS:   currentUser,
		TargetName: targetName,
		RepoID:     mirror.RepositoryID,
		Status:     "queued",
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

// ─── Namespace / Source helpers ───────────────────────────────────────────────

func (h *HFImportHandler) ensureNamespaceMapping(ctx context.Context, hfNamespace, targetNamespace string) error {
	existing, err := h.nsMapping.FindBySourceNamespace(ctx, hfNamespace)
	if err == nil && existing != nil {
		if existing.TargetNamespace != targetNamespace {
			existing.TargetNamespace = targetNamespace
			_, err = h.nsMapping.Update(ctx, existing)
		}
		return err
	}
	enabled := true
	_, err = h.nsMapping.Create(ctx, &database.MirrorNamespaceMapping{
		SourceNamespace: hfNamespace,
		TargetNamespace: targetNamespace,
		Enabled:         &enabled,
	})
	return err
}

func (h *HFImportHandler) getOrCreateHFSource(ctx context.Context) (int64, error) {
	sources, err := h.msStore.Index(ctx)
	if err != nil {
		return 0, err
	}
	for _, s := range sources {
		if strings.EqualFold(s.SourceName, "HuggingFace") {
			return s.ID, nil
		}
	}
	created, err := h.msStore.Create(ctx, &database.MirrorSource{
		SourceName: "HuggingFace",
		InfoAPIUrl: "https://huggingface.co/api/models",
	})
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

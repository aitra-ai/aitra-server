package handler

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
}

func NewHFImportHandler(cfg *config.Config) (*HFImportHandler, error) {
	mc, err := component.NewMirrorComponent(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create mirror component: %w", err)
	}
	return &HFImportHandler{
		mirror:    mc,
		nsMapping: database.NewMirrorNamespaceMappingStore(),
		msStore:   database.NewMirrorSourceStore(),
	}, nil
}

type HFImportReq struct {
	// HF model ID in the form "namespace/name", e.g. "facebook/opt-125m"
	HFModelID   string `json:"hf_model_id" binding:"required"`
	TargetName  string `json:"target_name"`
	Description string `json:"description"`
	License     string `json:"license"`
	SyncLfs     bool   `json:"sync_lfs"`
}

// ImportHFModel imports a HuggingFace model into the platform as a mirrored repo.
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

	// Parse "namespace/name" from HF model ID
	parts := strings.SplitN(req.HFModelID, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		httpbase.BadRequest(c, "hf_model_id must be in format 'namespace/name'")
		return
	}
	hfNamespace := parts[0]
	hfName := parts[1]

	targetName := req.TargetName
	if targetName == "" {
		targetName = hfName
	}

	ctx := c.Request.Context()

	// Ensure a namespace mapping exists: hfNamespace → currentUser
	if err := h.ensureNamespaceMapping(ctx, hfNamespace, currentUser); err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to ensure namespace mapping: %w", err))
		return
	}

	// Get the HuggingFace mirror source ID
	sourceID, err := h.getOrCreateHFSource(ctx)
	if err != nil {
		httpbase.ServerError(c, fmt.Errorf("failed to get HuggingFace mirror source: %w", err))
		return
	}

	license := req.License
	if license == "" {
		license = "other"
	}

	mirrorReq := types.CreateMirrorRepoReq{
		SourceNamespace:   hfNamespace,
		SourceName:        hfName,
		MirrorSourceID:    sourceID,
		RepoType:          types.ModelRepo,
		DefaultBranch:     "main",
		SourceGitCloneUrl: fmt.Sprintf("https://huggingface.co/%s/%s.git", hfNamespace, hfName),
		Description:       req.Description,
		License:           license,
		SyncLfs:           req.SyncLfs,
		CurrentUser:       currentUser,
	}

	mirror, err := h.mirror.CreateMirrorRepo(ctx, mirrorReq)
	if err != nil {
		// Check for duplicate key — not a fatal error
		if strings.Contains(err.Error(), "already exists") || strings.Contains(err.Error(), "DUPLICATE_KEY") {
			httpbase.OK(c, gin.H{"msg": "model already imported", "duplicate": true})
			return
		}
		httpbase.ServerError(c, fmt.Errorf("failed to import model: %w", err))
		return
	}

	httpbase.OK(c, gin.H{
		"repository_id": mirror.RepositoryID,
		"hf_model_id":   req.HFModelID,
		"target_name":   targetName,
		"target_ns":     currentUser,
	})
}

// ensureNamespaceMapping upserts a mapping: hfNamespace → targetNamespace.
func (h *HFImportHandler) ensureNamespaceMapping(ctx context.Context, hfNamespace, targetNamespace string) error {
	existing, err := h.nsMapping.FindBySourceNamespace(ctx, hfNamespace)
	if err == nil && existing != nil {
		// Mapping already exists — update target if different
		if existing.TargetNamespace != targetNamespace {
			existing.TargetNamespace = targetNamespace
			_, err = h.nsMapping.Update(ctx, existing)
		}
		return err
	}

	// Create new mapping
	enabled := true
	_, err = h.nsMapping.Create(ctx, &database.MirrorNamespaceMapping{
		SourceNamespace: hfNamespace,
		TargetNamespace: targetNamespace,
		Enabled:         &enabled,
	})
	return err
}

// getOrCreateHFSource returns the ID of the HuggingFace mirror source, creating it if absent.
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

	// Create HuggingFace source
	created, err := h.msStore.Create(ctx, &database.MirrorSource{
		SourceName: "HuggingFace",
		InfoAPIUrl: "https://huggingface.co/api/models",
	})
	if err != nil {
		return 0, err
	}
	return created.ID, nil
}

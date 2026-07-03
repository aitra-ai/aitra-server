package handler

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
)

type SandboxHandler struct {
	instanceStore database.SandboxInstanceStore
	featuredStore database.FeaturedSpaceStore
	docker        *SandboxDockerManager
}

func NewSandboxHandler(cfg *config.Config) *SandboxHandler {
	sandboxHost := "http://localhost"
	if cfg != nil && cfg.User.SandboxHost != "" {
		sandboxHost = cfg.User.SandboxHost
	}
	return &SandboxHandler{
		instanceStore: database.NewSandboxInstanceStore(),
		featuredStore: database.NewFeaturedSpaceStore(),
		docker:        NewSandboxDockerManager(sandboxHost),
	}
}

func (h *SandboxHandler) ListFeaturedSpaces(c *gin.Context) {
	spaces, err := h.featuredStore.List(c.Request.Context())
	if err != nil {
		httpbase.ServerError(c, err)
		return
	}
	type SpaceWithStats struct {
		database.FeaturedSpace
		RunningInstances int `json:"running_instances"`
	}
	result := make([]SpaceWithStats, 0, len(spaces))
	for _, sp := range spaces {
		count, _ := h.instanceStore.CountHotPool(c.Request.Context(), sp.SpacePath)
		result = append(result, SpaceWithStats{FeaturedSpace: sp, RunningInstances: count})
	}
	httpbase.OK(c, result)
}

func (h *SandboxHandler) LaunchSandbox(c *gin.Context) {
	namespace := c.Param("namespace")
	name := c.Param("name")
	spacePath := namespace + "/" + name

	userIDRaw, _ := c.Get("currentUser")
	username, _ := userIDRaw.(string)
	if username == "" {
		username = "anonymous"
	}

	fs, err := h.featuredStore.FindByPath(c.Request.Context(), spacePath)
	if err != nil {
		httpbase.BadRequestWithExt(c, err)
		return
	}

	hotInstances, _ := h.instanceStore.ListHotPoolReady(c.Request.Context(), spacePath)
	if len(hotInstances) > 0 {
		inst := hotInstances[0]
		expiry := time.Now().Add(time.Duration(fs.TTLSeconds) * time.Second)
		inst.IsHotPool = false
		inst.Username = username
		inst.ExpiresAt = &expiry
		_ = h.instanceStore.UpdateStatus(c.Request.Context(), inst.ID,
			"running", inst.ContainerID, inst.AccessURL, "", inst.Port)
		httpbase.OK(c, inst)
		go h.replenishHotPool(spacePath, fs)
		return
	}

	expiry := time.Now().Add(time.Duration(fs.TTLSeconds) * time.Second)
	inst := &database.SandboxInstance{
		SpacePath: spacePath,
		Template:  fs.Template,
		Username:  username,
		Status:    "starting",
		ExpiresAt: &expiry,
		IsHotPool: false,
	}
	if err := h.instanceStore.Create(c.Request.Context(), inst); err != nil {
		httpbase.ServerError(c, err)
		return
	}
	go h.startContainerAsync(inst.ID, fs)
	httpbase.OK(c, gin.H{"id": inst.ID, "status": "starting"})
}

func (h *SandboxHandler) GetSandboxStatus(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid id")
		return
	}
	inst, err := h.instanceStore.FindByID(c.Request.Context(), id)
	if err != nil {
		httpbase.NotFoundError(c, errors.New("sandbox instance not found"))
		return
	}
	_ = h.instanceStore.UpdateLastActive(c.Request.Context(), id)
	httpbase.OK(c, inst)
}

func (h *SandboxHandler) StopSandbox(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpbase.BadRequest(c, "invalid id")
		return
	}
	inst, err := h.instanceStore.FindByID(c.Request.Context(), id)
	if err != nil {
		httpbase.NotFoundError(c, errors.New("sandbox instance not found"))
		return
	}
	if inst.ContainerID != "" {
		_ = h.docker.StopContainer(c.Request.Context(), inst.ContainerID)
	}
	_ = h.instanceStore.UpdateStatus(c.Request.Context(), id, "stopped", inst.ContainerID, "", "", inst.Port)
	httpbase.OK(c, gin.H{"message": "sandbox stopped"})
}

func (h *SandboxHandler) ListMySandboxes(c *gin.Context) {
	all, err := h.instanceStore.ListAll(c.Request.Context())
	if err != nil {
		httpbase.ServerError(c, err)
		return
	}
	httpbase.OK(c, all)
}

func (h *SandboxHandler) AdminListFeaturedSpaces(c *gin.Context) {
	spaces, err := h.featuredStore.List(c.Request.Context())
	if err != nil {
		httpbase.ServerError(c, err)
		return
	}
	httpbase.OK(c, spaces)
}

func (h *SandboxHandler) AdminCreateFeaturedSpace(c *gin.Context) {
	var req database.FeaturedSpace
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequest(c, err.Error())
		return
	}
	if req.Template == "" {
		req.Template = "openclaw-local"
	}
	if req.TTLSeconds == 0 {
		req.TTLSeconds = 1800
	}
	if err := h.featuredStore.Create(c.Request.Context(), &req); err != nil {
		httpbase.ServerError(c, err)
		return
	}
	go h.warmHotPool(&req)
	httpbase.OK(c, req)
}

func (h *SandboxHandler) AdminUpdateFeaturedSpace(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req database.FeaturedSpace
	if err := c.ShouldBindJSON(&req); err != nil {
		httpbase.BadRequest(c, err.Error())
		return
	}
	req.ID = id
	if err := h.featuredStore.Update(c.Request.Context(), &req); err != nil {
		httpbase.ServerError(c, err)
		return
	}
	httpbase.OK(c, req)
}

func (h *SandboxHandler) AdminDeleteFeaturedSpace(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.featuredStore.Delete(c.Request.Context(), id); err != nil {
		httpbase.ServerError(c, err)
		return
	}
	httpbase.OK(c, gin.H{"message": "deleted"})
}

func (h *SandboxHandler) AdminListInstances(c *gin.Context) {
	all, err := h.instanceStore.ListAll(c.Request.Context())
	if err != nil {
		httpbase.ServerError(c, err)
		return
	}
	httpbase.OK(c, all)
}

func (h *SandboxHandler) startContainerAsync(instanceID int64, fs *database.FeaturedSpace) {
	ctx := context.Background()
	result, err := h.docker.StartContainer(ctx, instanceID, fs.Template, nil, fs.TTLSeconds)
	if err != nil {
		_ = h.instanceStore.UpdateStatus(ctx, instanceID, "error", "", "", err.Error(), 0)
		return
	}
	ready := h.docker.WaitReady(ctx, result.Port, 60*time.Second)
	status := "running"
	if !ready {
		status = "error"
	}
	_ = h.instanceStore.UpdateStatus(ctx, instanceID, status, result.ContainerID, result.AccessURL, "", result.Port)
}

func (h *SandboxHandler) replenishHotPool(spacePath string, fs *database.FeaturedSpace) {
	if fs.HotPool <= 0 {
		return
	}
	ctx := context.Background()
	current, _ := h.instanceStore.CountHotPool(ctx, spacePath)
	for i := current; i < fs.HotPool; i++ {
		expiry := time.Now().Add(time.Duration(fs.TTLSeconds) * time.Second * 2)
		inst := &database.SandboxInstance{
			SpacePath: spacePath,
			Template:  fs.Template,
			Status:    "starting",
			IsHotPool: true,
			ExpiresAt: &expiry,
		}
		_ = h.instanceStore.Create(ctx, inst)
		go h.startContainerAsync(inst.ID, fs)
	}
}

func (h *SandboxHandler) warmHotPool(fs *database.FeaturedSpace) {
	h.replenishHotPool(fs.SpacePath, fs)
}

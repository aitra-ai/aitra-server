package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/openai/openai-go/v3"
	"opencsg.com/csghub-server/aigateway/component"
	"opencsg.com/csghub-server/aigateway/token"
	"opencsg.com/csghub-server/aigateway/types"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/builder/proxy"
	"opencsg.com/csghub-server/builder/rpc"
	"opencsg.com/csghub-server/builder/store/cache"
	"opencsg.com/csghub-server/builder/store/database"
	"opencsg.com/csghub-server/common/config"
	"opencsg.com/csghub-server/common/errorx"
	commonType "opencsg.com/csghub-server/common/types"
	apicomp "opencsg.com/csghub-server/component"
)

// OpenAIHandler defines the interface for handling OpenAI compatible APIs
type OpenAIHandler interface {
	// List available models
	ListModels(c *gin.Context)
	// Get model details
	GetModel(c *gin.Context)
	// Chat with backend model
	Chat(c *gin.Context)
	Embedding(c *gin.Context)
}

func NewOpenAIHandlerFromConfig(config *config.Config) (OpenAIHandler, error) {
	modelService, err := component.NewOpenAIComponentFromConfig(config)
	if err != nil {
		return nil, err
	}
	repoComp, err := apicomp.NewRepoComponent(config)
	if err != nil {
		return nil, err
	}
	var modSvcClient rpc.ModerationSvcClient
	var cacheClient cache.RedisClient
	if config.SensitiveCheck.Enable {
		modSvcClient = rpc.NewModerationSvcHttpClient(fmt.Sprintf("%s:%d", config.Moderation.Host, config.Moderation.Port))
		cacheClient, err = cache.NewCache(context.Background(), cache.RedisConfig{
			Addr:     config.Redis.Endpoint,
			Username: config.Redis.User,
			Password: config.Redis.Password,
		})
		if err != nil {
			return nil, err
		}
	}
	modComponent := component.NewModerationImplWithClient(config, modSvcClient, cacheClient)
	clusterComp, err := apicomp.NewClusterComponent(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create cluster component, error: %w", err)
	}
	return newOpenAIHandler(modelService, repoComp, modComponent, clusterComp, token.NewCounterFactory(), config), nil
}

func newOpenAIHandler(
	modelService component.OpenAIComponent,
	repoComp apicomp.RepoComponent,
	modComponent component.Moderation,
	clusterComp apicomp.ClusterComponent,
	tokenCounterFactory token.CounterFactory,
	config *config.Config,
) *OpenAIHandlerImpl {
	return &OpenAIHandlerImpl{
		openaiComponent:     modelService,
		repoComp:            repoComp,
		modComponent:        modComponent,
		clusterComp:         clusterComp,
		tokenCounterFactory: tokenCounterFactory,
		config:              config,
	}
}

// handleInsufficientBalance handles the insufficient balance error response
// for both stream and non-stream requests
func (h *OpenAIHandlerImpl) handleInsufficientBalance(c *gin.Context, isStream bool, username, modelID string, err error) {
	// Handle budget exceeded (429) separately from insufficient balance (403)
	if errors.Is(err, errorx.ErrBudgetExceeded) {
		slog.WarnContext(c.Request.Context(), "monthly budget exceeded",
			"user", username, "model", modelID)
		c.Header("Retry-After", "3600")
		c.JSON(http.StatusTooManyRequests, gin.H{
			"error": map[string]string{
				"type":    "budget_exceeded",
				"message": "Monthly budget exceeded. Please increase your budget or wait until next month.",
			},
		})
		c.Abort()
		return
	}

	// Check if the error is the standard insufficient balance error
	if !errors.Is(err, errorx.ErrInsufficientBalance) {
		// If it's a different error, log and return generic error
		slog.ErrorContext(c.Request.Context(), "balance check failed",
			"user", username, "model", modelID, "error", err)
		httpbase.ServerError(c, err)
		return
	}

	slog.WarnContext(c.Request.Context(), "insufficient balance for request",
		"user", username, "model", modelID)

	if isStream {
		// For stream requests, write error chunk
		errorChunk := generateInsufficientBalanceResp(h.config.Frontend.URL)
		errorChunkJson, _ := json.Marshal(errorChunk)
		_, writeErr := c.Writer.Write([]byte("data: " + string(errorChunkJson) + "\n\ndata: [DONE]\n\n"))
		if writeErr != nil {
			slog.Error("failed to write insufficient balance error to stream", "error", writeErr)
		}
		c.Writer.Flush()
	} else {
		httpbase.ForbiddenError(c, err)
	}
}

// OpenAIHandlerImpl implements the OpenAIHandler interface
type OpenAIHandlerImpl struct {
	openaiComponent     component.OpenAIComponent
	repoComp            apicomp.RepoComponent
	modComponent        component.Moderation
	clusterComp         apicomp.ClusterComponent
	tokenCounterFactory token.CounterFactory
	config              *config.Config
}

// ListModels godoc
// @Security     ApiKey
// @Summary      List available models
// @Description  Returns a list of available models, supports fuzzy search by model_id query parameter and filtering by public status
// @Tags         AIGateway
// @Accept       json
// @Produce      json
// @Param        model_id query string false "Model ID for fuzzy search"
// @Param        public query bool false "Filter by public status (true for public models, false for private models)"
// @Param        per query int false "Models per page (default 20, max 100)"
// @Param        page query int false "Page number (1-based, default 1)"
// @Success      200  {object}  types.ModelList "OK"
// @Failure      500  {object}  error "Internal server error"
// @Router       /v1/models [get]
func (h *OpenAIHandlerImpl) ListModels(c *gin.Context) {
	currentUser := httpbase.GetCurrentUser(c)
	resp, err := h.openaiComponent.ListModels(c.Request.Context(), currentUser, types.ListModelsReq{
		ModelID: c.Query("model_id"),
		Public:  c.Query("public"),
		Per:     c.Query("per"),
		Page:    c.Query("page"),
	})
	if err != nil {
		slog.ErrorContext(c.Request.Context(), "failed to get available models", "error", err.Error(), "current_user", currentUser)
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": types.Error{
				Code:    "internal_server_error",
				Message: "Failed to retrieve models",
				Type:    "server_error",
			}})
		return
	}
	c.PureJSON(http.StatusOK, resp)
}

// GetModel godoc
// @Security     ApiKey
// @Summary      Get model details
// @Description  Returns information about a specific model
// @Tags         AIGateway
// @Accept       json
// @Produce      json
// @Param        model path string true "Model ID"
// @Success      200  {object}  types.Model "OK"
// @Failure      404  {object}  error "Model not found"
// @Failure      500  {object}  error "Internal server error"
// @Router       /v1/models/{model} [get]
func (h *OpenAIHandlerImpl) GetModel(c *gin.Context) {
	username := httpbase.GetCurrentUser(c)
	modelID := c.Param("model")
	if modelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": types.Error{
				Code:    "model_not_found",
				Message: "model id can not be empty",
				Type:    "invalid_request_error",
			}})
		return
	}

	model, err := h.openaiComponent.GetModelByID(c.Request.Context(), username, modelID)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Errorf("failed to get model by id '%s',error:%w", modelID, err).Error())
		return
	}
	if model == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": types.Error{
				Code:    "model_not_found",
				Message: fmt.Sprintf("model '%s' not found", modelID),
				Type:    "invalid_request_error",
			}})
		return
	}

	c.PureJSON(http.StatusOK, model)
}

var _ openai.ChatCompletion
var _ openai.ChatCompletionChunk

// Chat godoc
// @Security     ApiKey
// @Summary      Chat with backend model
// @Description  Sends a chat completion request to the backend model and returns the response
// @Tags         AIGateway
// @Accept       json
// @Produce      json
// @Param        request body ChatCompletionRequest true "Chat completion request"
// @Success      200  {object}  openai.ChatCompletion "OK"
// @Success      200  {object}  openai.ChatCompletionChunk "OK"
// @Failure      400  {object}  error "Bad request"
// @Failure      404  {object}  error "Model not found"
// @Failure      500  {object}  error "Internal server error"
// @Router       /v1/chat/completions [post]
// buildAnthropicBody converts OpenAI-format request to Anthropic Messages API format.
// Extracts system messages from messages array into top-level "system" parameter.
func buildAnthropicBody(chatReq *ChatCompletionRequest) []byte {
	// Extract system messages
	var systemParts []string
	var nonSystemMessages []openai.ChatCompletionMessageParamUnion
	for _, msg := range chatReq.Messages {
		// Check if it's a system message by marshaling and inspecting
		msgBytes, _ := json.Marshal(msg)
		var raw map[string]any
		json.Unmarshal(msgBytes, &raw)
		if role, ok := raw["role"].(string); ok && role == "system" {
			if content, ok := raw["content"].(string); ok {
				systemParts = append(systemParts, content)
			}
		} else {
			nonSystemMessages = append(nonSystemMessages, msg)
		}
	}

	// Build Anthropic body with top-level system param
	body := map[string]any{
		"model":      chatReq.Model,
		"messages":   nonSystemMessages,
		"max_tokens": chatReq.MaxTokens,
		"stream":     chatReq.Stream,
	}
	if len(systemParts) > 0 {
		body["system"] = strings.Join(systemParts, "\n\n")
	}
	if chatReq.Temperature > 0 {
		body["temperature"] = chatReq.Temperature
	}
	result, _ := json.Marshal(body)
	return result
}

func (h *OpenAIHandlerImpl) Chat(c *gin.Context) {
	/*
		1.parse request body of ChatCompletionRequest
		2.get model id from request body
		3.find running model endpoint by model id
		4.proxy request to running model endpoint
	*/
	requestStart := time.Now()
	username := httpbase.GetCurrentUser(c)
	userUUID := httpbase.GetCurrentUserUUID(c)
	chatReq := &ChatCompletionRequest{}
	if err := c.BindJSON(chatReq); err != nil {
		slog.Error("invalid chat compoletion request body", "error", err.Error())
		c.String(http.StatusBadRequest, fmt.Errorf("invalid chat compoletion request body:%w", err).Error())
		return
	}
	// Skill injection: if skill parameter provided, inject system_prompt + tools
	if chatReq.Skill != "" {
		skillStore := database.NewAISkillStore()
		// Try by name first, then by ID
		var skill *database.AISkill
		skill, _ = skillStore.FindByName(c.Request.Context(), chatReq.Skill)
		if skill == nil {
			if sid, err := strconv.ParseInt(chatReq.Skill, 10, 64); err == nil {
				skill, _ = skillStore.FindByID(c.Request.Context(), sid)
			}
		}
		if skill != nil && skill.Enabled {
			// Inject system prompt as first message
			if skill.SystemPrompt != "" {
				sysMsg := openai.SystemMessage(skill.SystemPrompt)
				chatReq.Messages = append([]openai.ChatCompletionMessageParamUnion{sysMsg}, chatReq.Messages...)
			}
			// Override model if skill has preferred model and request didn't specify
			if skill.PreferredModel != "" && chatReq.Model == "" {
				chatReq.Model = skill.PreferredModel
			}
			// Merge skill tools into request
			if len(skill.Tools) > 0 {
				toolsJSON, err := json.Marshal(skill.Tools)
				if err == nil {
					var tools []openai.ChatCompletionToolUnionParam
					if json.Unmarshal(toolsJSON, &tools) == nil {
						chatReq.Tools = append(chatReq.Tools, tools...)
					}
				}
			}
			// Increment usage count async
			go func(id int64) {
				ctx2, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				skillStore.IncrementUsageCount(ctx2, id)
			}(skill.ID)
			slog.Info("skill injected", "skill", skill.Name, "system_prompt_len", len(skill.SystemPrompt))
		}
	}

	modelID := chatReq.Model
	model, err := h.openaiComponent.GetModelByID(c.Request.Context(), username, modelID)
	if err != nil {
		slog.Error("failed to get model by id", "model_id", modelID, "error", err.Error())
		c.String(http.StatusInternalServerError, fmt.Errorf("failed to get model by id '%s',error:%w", modelID, err).Error())
		return
	}
	if model == nil {
		slog.Error("model not found", "model_id", modelID)
		c.JSON(http.StatusBadRequest, gin.H{
			"error": types.Error{
				Code:    "model_not_found",
				Message: fmt.Sprintf("model '%s' not found", modelID),
				Type:    "invalid_request_error",
			}})
		return
	}

	targetReq := commonType.EndpointReq{
		ClusterID: model.ClusterID,
		Target:    model.Endpoint,
		Host:      "",
		Endpoint:  model.Endpoint,
		SvcName:   model.SvcName,
	}
	target := ""
	host := ""
	if len(model.SvcName) > 0 {
		target, host, err = apicomp.ExtractDeployTargetAndHost(c.Request.Context(), h.clusterComp, targetReq)
	} else {
		slog.Debug("external model", slog.Any("model", model))
		target = model.Endpoint
	}
	if err != nil || len(target) < 1 {
		slog.Error("failed to get model target address", slog.Any("error", err),
			slog.Any("model", model), slog.Any("targetReq", targetReq), slog.Any("model_id", modelID),
			slog.Any("target", target), slog.Any("host", host))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": types.Error{
				Code:    "model_not_running",
				Message: fmt.Sprintf("model '%s' not running", modelID),
				Type:    "invalid_request_error",
			}})
		return
	}

	modelName, _, err := (component.ModelIDBuilder{}).From(modelID)
	if err != nil {
		slog.Error("failed to process chat request", "error", err, "model_id", modelID)
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	chatReq.Model = modelName
	if chatReq.Stream {
		c.Writer.Header().Set("Content-Type", "text/event-stream")
		// stream_options is OpenAI-specific; skip for external providers (Anthropic etc.)
		if len(model.SvcName) > 0 && !strings.Contains(model.ImageID, "vllm-cpu") {
			chatReq.StreamOptions = &StreamOptions{
				IncludeUsage: true,
			}
		}
	}

	// Check API Key model permission
	accessToken := httpbase.GetAccessToken(c)
	if accessToken != "" {
		tokenStore := database.NewAccessTokenStore()
		if at, err := tokenStore.FindByToken(c.Request.Context(), accessToken, string(commonType.AccessTokenAppAIGateway)); err == nil && at != nil {
			if len(at.AllowedModels) > 0 {
				allowed := false
				for _, m := range at.AllowedModels {
					if m == modelName {
						allowed = true
						break
					}
				}
				if !allowed {
					c.JSON(http.StatusForbidden, gin.H{
						"error": map[string]string{
							"type":    "model_not_allowed",
							"message": fmt.Sprintf("API key does not have access to model '%s'", modelName),
						},
					})
					c.Abort()
					return
				}
			}
		}
	}

	sceneValue := c.Request.Header.Get(commonType.SceneHeaderKey)
	// Check balance before processing request
	if err := h.openaiComponent.CheckBalance(c.Request.Context(), username, model, sceneValue); err != nil {
		h.handleInsufficientBalance(c, chatReq.Stream, username, modelID, err)
		return
	}

	// Anthropic requires max_tokens; inject a default if not provided.
	if len(model.SvcName) == 0 && model.Provider == "anthropic" && chatReq.MaxTokens == 0 {
		chatReq.MaxTokens = 4096
	}

	// For Anthropic provider: extract system messages from messages array into top-level system param
	var updatedBodyBytes []byte
	if len(model.SvcName) == 0 && model.Provider == "anthropic" {
		updatedBodyBytes = buildAnthropicBody(chatReq)
	} else {
		updatedBodyBytes, _ = json.Marshal(chatReq)
	}
	slog.Info("outgoing request body to upstream", slog.String("body", string(updatedBodyBytes)))
	c.Request.Body = io.NopCloser(bytes.NewReader(updatedBodyBytes))
	c.Request.ContentLength = int64(len(updatedBodyBytes))
	slog.Info("proxy chat request to model target", slog.Any("target", target), slog.Any("host", host),
		slog.Any("user", username), slog.Any("model_name", modelName))
	// Create a combined key using userUUID and modelID for caching and tracking
	key := fmt.Sprintf("%s:%s", userUUID, modelID)
	result, err := h.modComponent.CheckChatPrompts(c.Request.Context(), chatReq.Messages, key)
	if err != nil {
		c.String(http.StatusInternalServerError, fmt.Errorf("failed to call moderation error:%w", err).Error())
		return
	}
	if result.IsSensitive {
		slog.Debug("sensitive content", slog.String("reason", result.Reason))
		errorChunk := generateSensitiveRespForPrompt()
		errorChunkJson, _ := json.Marshal(errorChunk)
		_, err := c.Writer.Write([]byte("data: " + string(errorChunkJson) + "\n\n" + "[DONE]"))
		if err != nil {
			slog.Error("write into resp error:", slog.String("err", err.Error()))
		}
		c.Writer.Flush()
		return
	}
	tokenCounter := h.tokenCounterFactory.NewChat(token.CreateParam{
		Endpoint: target,
		Host:     host,
		Model:    modelName,
		ImageID:  model.ImageID,
	})
	// External Anthropic models return Anthropic Messages format; convert to OpenAI format.
	convertAnthropic := len(model.SvcName) == 0 && model.Provider == "anthropic"
	w := NewResponseWriterWrapperWithOptions(c.Writer, chatReq.Stream, h.modComponent, tokenCounter, convertAnthropic)
	defer w.ClearBuffer()

	tokenCounter.AppendPrompts(chatReq.Messages)

	// External models with fallbacks: use fallback-aware proxy
	if len(model.SvcName) == 0 && len(model.Fallbacks) > 0 {
		actualProvider := proxyWithFallback(c, w, chatReq, model, h.config, 2)
		c.Header("X-Provider", actualProvider)
	} else {
		// Original path: platform models or external without fallbacks
		rp, _ := proxy.NewReverseProxy(target)
		proxyToApi := ""
		if model.Endpoint != "" {
			uri, err := url.ParseRequestURI(model.Endpoint)
			if err != nil {
				slog.Warn("endpoint has wrong struct ", slog.String("model", modelName))
			} else {
				proxyToApi = uri.Path
			}
		}

		// Inject auth headers for external models.
		if len(model.SvcName) == 0 {
			authJSON := buildProviderAuthHead(h.config, model.Provider, model.Endpoint, model.AuthHead)
			slog.Info("external model auth",
				slog.String("model", modelName),
				slog.String("provider", model.Provider),
				slog.Bool("authHead_set", model.AuthHead != ""),
				slog.Bool("authJSON_set", authJSON != ""),
			)
			if authJSON != "" {
				var authMap map[string]string
				if err := json.Unmarshal([]byte(authJSON), &authMap); err != nil {
					slog.Warn("invalid auth head", slog.String("model", modelName))
				} else {
					for authKey, authVal := range authMap {
						c.Request.Header.Set(authKey, authVal)
					}
				}
			}
		}
		c.Header("X-Provider", model.Provider)
		rp.ServeHTTP(w, c.Request, proxyToApi, host)
	}

	go func() {
		usageCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 3*time.Second)
		defer cancel()

		err := h.openaiComponent.RecordUsage(usageCtx, userUUID, model, tokenCounter, sceneValue)
		if err != nil {
			slog.Error("failed to record token usage", "error", err)
		}
	}()

	// Record billing usage log asynchronously
	go func() {
		bctx, bcancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer bcancel()

		// Prefer raw captured tokens (works for both OpenAI and Anthropic formats).
		// Fall back to tokenCounter for platform models (SvcName set).
		var inputTokens, outputTokens int
		if capturer, ok := w.(interface{ GetCapturedTokens() (int, int) }); ok {
			inputTokens, outputTokens = capturer.GetCapturedTokens()
		}
		if inputTokens == 0 && outputTokens == 0 {
			// Try tokenCounter as fallback
			if usage, uerr := tokenCounter.Usage(bctx); uerr == nil && usage != nil {
				inputTokens = int(usage.PromptTokens)
				outputTokens = int(usage.CompletionTokens)
			}
		}
		if inputTokens == 0 && outputTokens == 0 {
			return
		}

		// Look up user's numeric ID
		userStore := database.NewUserStore()
		u, err := userStore.FindByUsername(bctx, username)
		if err != nil {
			slog.Warn("billing: failed to find user", "username", username, "error", err)
			return
		}

		// Look up pricing
		var costUSD float64
		billingStore := database.NewLLMBillingStore(h.config)
		billing, berr := billingStore.FindByModel(bctx, model.Provider, modelName)
		if berr == nil && billing != nil {
			inputCost := float64(inputTokens) / 1_000_000.0 * billing.PriceInput
			outputCost := float64(outputTokens) / 1_000_000.0 * billing.PriceOutput
			costUSD = inputCost + outputCost
		}

		// Build request summary: model + message count
		reqSummary := fmt.Sprintf("model=%s, messages=%d", modelName, len(chatReq.Messages))

		latencyMs := time.Since(requestStart).Milliseconds()
		statusCode := c.Writer.Status()

		usageLogStore := database.NewModelUsageLogStore(h.config)
		logErr := usageLogStore.Create(bctx, &database.ModelUsageLog{
			UserID:         u.ID,
			Username:       username,
			ModelID:        modelName,
			Provider:       model.Provider,
			InputTokens:    inputTokens,
			OutputTokens:   outputTokens,
			CostUSD:        costUSD,
			StatusCode:     statusCode,
			LatencyMs:      latencyMs,
			RequestSummary: reqSummary,
			CreatedAt:      time.Now(),
		})
		if logErr != nil {
			slog.Error("billing: failed to create usage log", "error", logErr)
		}
	}()
}

// Embedding godoc
// @Security     ApiKey
// @Summary      Get embedding for a text
// @Description  Sends a text to the backend model and returns the embedding
// @Tags         AIGateway
// @Accept       json
// @Produce      json
// @Param        request body  EmbeddingRequest true "Embedding request"
// @Success      200  {object}  types.Response{} "OK"
// @Failure      400  {object}  error "Bad request or sensitive input"
// @Failure      404  {object}  error "Model not found"
// @Failure      500  {object}  error "Internal server error"
// @Router       /v1/embeddings [post]
func (h *OpenAIHandlerImpl) Embedding(c *gin.Context) {
	var req EmbeddingRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.Model == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Model cannot be empty"})
		return
	}
	if req.Input.OfString.String() == "" &&
		len(req.Input.OfArrayOfStrings) == 0 &&
		len(req.Input.OfArrayOfTokenArrays) == 0 &&
		len(req.Input.OfArrayOfTokens) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Input cannot be empty"})
		return
	}
	modelID := req.Model
	username := httpbase.GetCurrentUser(c)
	userUUID := httpbase.GetCurrentUserUUID(c)
	model, err := h.openaiComponent.GetModelByID(c.Request.Context(), username, modelID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if model == nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": types.Error{
				Code:    "model_not_found",
				Message: fmt.Sprintf("model '%s' not found", modelID),
				Type:    "invalid_request_error",
			}})
		return
	}

	targetReq := commonType.EndpointReq{
		ClusterID: model.ClusterID,
		Target:    model.Endpoint,
		Host:      "",
		Endpoint:  model.Endpoint,
		SvcName:   model.SvcName,
	}
	target := ""
	host := ""
	if len(model.SvcName) > 0 {
		target, host, err = apicomp.ExtractDeployTargetAndHost(c.Request.Context(), h.clusterComp, targetReq)
	} else {
		target = model.Endpoint
	}
	if err != nil || len(target) < 1 {
		slog.ErrorContext(c, "failed to get embedding target address", slog.Any("error", err),
			slog.Any("model", model), slog.Any("targetReq", targetReq), slog.Any("model_id", modelID),
			slog.Any("target", target), slog.Any("host", host))
		c.JSON(http.StatusBadRequest, gin.H{
			"error": types.Error{
				Code:    "model_not_running",
				Message: fmt.Sprintf("model '%s' not running", modelID),
				Type:    "invalid_request_error",
			}})
		return
	}

	sceneValue := c.Request.Header.Get(commonType.SceneHeaderKey)
	// Check balance before processing request
	if err := h.openaiComponent.CheckBalance(c.Request.Context(), username, model, sceneValue); err != nil {
		h.handleInsufficientBalance(c, false, username, modelID, err)
		return
	}

	modelName, _, err := (component.ModelIDBuilder{}).From(modelID)
	if err != nil {
		slog.ErrorContext(c, "failed to process chat request", "error", err, "model_id", modelID)
		c.String(http.StatusBadRequest, err.Error())
		return
	}
	req.Model = modelName
	data, _ := json.Marshal(req)
	c.Request.Body = io.NopCloser(bytes.NewReader(data))
	c.Request.ContentLength = int64(len(data))
	slog.InfoContext(c, "proxy embedding request to model endpoint", slog.Any("target", target), slog.Any("host", host),
		slog.Any("user", username), slog.Any("model_id", modelID))
	rp, _ := proxy.NewReverseProxy(target)

	tokenCounter := h.tokenCounterFactory.NewEmbedding(token.CreateParam{
		Endpoint: target,
		Host:     host,
		Model:    modelName,
		ImageID:  model.ImageID,
	})
	// Inject auth headers for external embedding models
	if len(model.SvcName) == 0 {
		if authJSON := buildProviderAuthHead(h.config, model.Provider, model.Endpoint, model.AuthHead); authJSON != "" {
			var authMap map[string]string
			if err := json.Unmarshal([]byte(authJSON), &authMap); err == nil {
				for k, v := range authMap {
					c.Request.Header.Set(k, v)
				}
			}
		}
	}

	w := NewResponseWriterWrapperEmbedding(c.Writer, tokenCounter)
	if req.Input.OfString.String() != "" {
		tokenCounter.Input(req.Input.OfString.Value)
	}

	rp.ServeHTTP(w, c.Request, "", host)
	go func() {
		usageCtx, cancel := context.WithTimeout(context.WithoutCancel(c.Request.Context()), 3*time.Second)
		defer cancel()

		err := h.openaiComponent.RecordUsage(usageCtx, userUUID, model, tokenCounter, sceneValue)
		if err != nil {
			slog.ErrorContext(c, "failed to record embedding token usage", "error", err)
		}
	}()
}

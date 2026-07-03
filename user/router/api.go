package router

import (
	"fmt"
	"log/slog"

	"github.com/aitra-ai/aitra-server/builder/instrumentation"

	"github.com/gin-contrib/pprof"
	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	"github.com/aitra-ai/aitra-server/api/middleware"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/errorx"
	"github.com/aitra-ai/aitra-server/user/handler"
)

func NewRouter(config *config.Config) (*gin.Engine, error) {
	r := gin.New()
	middleware.SetInfraMiddleware(r, config, instrumentation.User)
	needAPIKey := middleware.NeedAPIKey(config)
	//add router for golang pprof
	debugGroup := r.Group("/debug", needAPIKey)
	pprof.RouteRegister(debugGroup, "pprof")

	r.Use(middleware.Authenticator(config))

	userHandler, err := handler.NewUserHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating user handler:%w", err)
	}
	acHandler, err := handler.NewAccessTokenHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating token handler:%w", err)
	}
	orgHandler, err := handler.NewOrganizationHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating user controller:%w", err)
	}
	// Member
	memberCtrl, err := handler.NewMemberHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating user controller:%w", err)
	}
	//namespace
	nsCtrl, err := handler.NewNamespaceHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating namespace controller:%w", err)
	}

	apiV1Group := r.Group("/api/v1")
	jwtGroup := apiV1Group.Group("/jwt")
	userGroup := apiV1Group.Group("/user")
	tokenGroup := apiV1Group.Group("/token")
	internalGroup := apiV1Group.Group("/internal", needAPIKey)
	internalUserGroup := internalGroup.Group("/user")

	jwtHandler, err := handler.NewJWTHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating jwt handler:%w", err)
	}
	loginHandler, err := handler.NewLoginHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating login handler:%w", err)
	}

	//don't need login
	{
		//casdoor
		apiV1Group.GET("/callback/casdoor", userHandler.Casdoor)
		// password login
		apiV1Group.POST("/login", loginHandler.PasswordLogin)
		//user
		userGroup.GET("/:username", userHandler.Get)
		userGroup.DELETE("/:username", userHandler.Delete)
		// find user by uuids
		apiV1Group.GET("/users/by-uuids", userHandler.FindByUUIDs)
		userGroup.DELETE("/:username/close_account", userHandler.CloseAccount)
		// org and members
		apiV1Group.GET("/organizations", orgHandler.Index)
		apiV1Group.GET("/organization/:namespace", orgHandler.Get)
		apiV1Group.GET("/organization/:namespace/members", memberCtrl.OrgMembers)
	}

	//internal only
	{
		//organization
		//namespace
		apiV1Group.GET("/namespace/:path", needAPIKey, nsCtrl.GetInfo)
		//jwt
		jwtGroup.POST("/token", needAPIKey, jwtHandler.Create)
		jwtGroup.GET("/:token", needAPIKey, jwtHandler.Verify)
		// check token info
		tokenGroup.GET("/:token_value", acHandler.Get)
		userGroup.GET("/user_uuids", needAPIKey, userHandler.GetUserUUIDs)

		internalUserGroup.GET("/emails", userHandler.GetEmailsInternal)
	}

	apiV1Group.Use(mustLogin())
	userMatch := userMatch()

	// routers for users
	{
		// userGroup.POST("", userHandler.Create)
		// user self or admin
		userGroup.PUT("/:id", mustLogin(), userHandler.Update)
		//TODO:
		// userGroup.DELETE("/:username", userMatch, userHandler.Delete)
		// get user's all tokens
		userGroup.GET("/:username/tokens", userMatch, acHandler.GetUserTokens)
		userGroup.GET("/:username/tokens/first", userMatch, acHandler.GetOrCreateFirstAvaiTokens)
		// get user list
		apiV1Group.GET("/users", mustLogin(), userHandler.Index)
		// export user info
		apiV1Group.GET("/users/stream-export", mustLogin(), userHandler.ExportUserInfo)

		// user labels
		userGroup.PUT("/labels", mustLogin(), userHandler.UpdateUserLabels)
		// get user's email addresses
		userGroup.GET("/emails", mustLogin(), userHandler.GetEmails)
	}
	// routers for user verify
	{
		userGroup.POST("/verify", mustLogin(), userHandler.CreateVerify)
		userGroup.PUT("/verify/:id", mustLogin(), userHandler.UpdateVerify)
		userGroup.GET("/verify/:id", mustLogin(), userHandler.GetVerify)
	}
	// routers for organizations
	{
		apiV1Group.POST("/organizations", orgHandler.Create)
		apiV1Group.PUT("/organization/:namespace", orgHandler.Update)
		apiV1Group.DELETE("/organization/:namespace", orgHandler.Delete)
	}
	// routers for members
	{
		apiV1Group.GET("/organization/:namespace/members/:username", userMatch, memberCtrl.GetMemberRole)
		apiV1Group.POST("/organization/:namespace/members", memberCtrl.Create)
		apiV1Group.PUT("/organization/:namespace/members/:username", memberCtrl.Update)
		apiV1Group.DELETE("/organization/:namespace/members/:username", memberCtrl.Delete)
	}
	// routers for organization verify
	{
		apiV1Group.POST("/organization/verify", mustLogin(), orgHandler.CreateVerify)
		apiV1Group.PUT("/organization/verify/:id", mustLogin(), orgHandler.UpdateVerify)
		apiV1Group.GET("/organization/verify/:namespace", orgHandler.GetVerify)
	}
	// routers for access tokens
	{
		tokenGroup.POST("/:app/:token_name", acHandler.CreateAppToken)
		tokenGroup.PUT("/:app/:token_name", acHandler.Refresh)
		tokenGroup.DELETE("/:app/:token_name", acHandler.DeleteAppToken)
	}

	{
		userGroup.POST("/email-verification-code/:email", mustLogin(), userHandler.GenerateVerificationCodeAndSendEmail)
	}

	{
		userGroup.POST("/tags", mustLogin(), userHandler.ResetUserTags)
	}

	// Admin routes for LLM config management
	llmConfigHandler, err := handler.NewLLMConfigHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating llm config handler:%w", err)
	}

	// Usage & Billing handlers
	usageHandler, err := handler.NewUsageHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating usage handler:%w", err)
	}

	// GPU Deployment handler
	gpuDeployHandler, err := handler.NewGPUDeploymentHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating gpu deployment handler:%w", err)
	}

	// HuggingFace import handler
	hfImportHandler, err := handler.NewHFImportHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating hf import handler:%w", err)
	}

	// Model weight handler
	modelWeightHandler, err := handler.NewModelWeightHandler(config)
	if err != nil {
		return nil, fmt.Errorf("error creating model weight handler:%w", err)
	}
	{
		adminGroup := apiV1Group.Group("/admin", mustLogin())
		adminGroup.GET("/llm_configs", llmConfigHandler.ListExternalLLMConfigs)
		adminGroup.POST("/llm_configs", llmConfigHandler.CreateExternalLLMConfig)
		adminGroup.PUT("/llm_configs/:id", llmConfigHandler.UpdateExternalLLMConfig)
		adminGroup.DELETE("/llm_configs/:id", llmConfigHandler.DeleteExternalLLMConfig)

		// Billing config CRUD (admin only)
		adminGroup.GET("/billing", usageHandler.ListBilling)
		adminGroup.POST("/billing", usageHandler.CreateBilling)
		adminGroup.PUT("/billing/:id", usageHandler.UpdateBilling)
		adminGroup.DELETE("/billing/:id", usageHandler.DeleteBilling)

		// Audit usage logs (admin only)
		adminGroup.GET("/audit/usage", usageHandler.GetAuditUsage)
		adminGroup.GET("/audit/summary", usageHandler.GetAuditSummary)

		// Credit management (admin only)
		adminGroup.GET("/credits", usageHandler.ListUserBalances)
		adminGroup.POST("/credits/grant", usageHandler.GrantCredit)

		// GPU SKU management (admin only)
		adminGroup.GET("/gpu/skus", gpuDeployHandler.AdminListGPUSkus)
		adminGroup.POST("/gpu/skus", gpuDeployHandler.AdminCreateGPUSku)
		adminGroup.PUT("/gpu/skus/:id", gpuDeployHandler.AdminUpdateGPUSku)
		adminGroup.DELETE("/gpu/skus/:id", gpuDeployHandler.AdminDeleteGPUSku)
		adminGroup.GET("/gpu/deployments", gpuDeployHandler.AdminListDeployments)

		// Health check logs (admin only)
		healthHandler := handler.NewHealthHandler()
		adminGroup.GET("/health_logs", healthHandler.ListHealthLogs)

		// MCP resource management (admin only)
		mcpHandler := handler.NewMCPHandler()
		adminGroup.GET("/mcp", mcpHandler.ListMCPResources)
		adminGroup.POST("/mcp", mcpHandler.CreateMCPResource)
		adminGroup.PUT("/mcp/:id", mcpHandler.UpdateMCPResource)
		adminGroup.DELETE("/mcp/:id", mcpHandler.DeleteMCPResource)

		// Rate limit management (admin only)
		rateLimitHandler := handler.NewRateLimitHandler(config)
		adminGroup.GET("/rate_limits", rateLimitHandler.ListRateLimits)
		adminGroup.POST("/rate_limits", rateLimitHandler.UpsertRateLimit)
		adminGroup.DELETE("/rate_limits/:id", rateLimitHandler.DeleteRateLimit)

		// AI Skills management (admin only)
		aiSkillHandler := handler.NewAISkillHandler()
		adminGroup.GET("/skills", aiSkillHandler.AdminListSkills)
		adminGroup.POST("/skills", aiSkillHandler.AdminCreateSkill)
		adminGroup.PUT("/skills/:id", aiSkillHandler.AdminUpdateSkill)
		adminGroup.DELETE("/skills/:id", aiSkillHandler.AdminDeleteSkill)
	}

	// User usage routes (require login)
	rateLimitHandler := handler.NewRateLimitHandler(config)
	{
		userGroup.GET("/usage", mustLogin(), usageHandler.GetMyUsage)
		userGroup.GET("/usage/summary", mustLogin(), usageHandler.GetMyUsageSummary)
		userGroup.GET("/settings/budget", mustLogin(), usageHandler.GetMyBudget)
		userGroup.PUT("/settings/budget", mustLogin(), usageHandler.SetMyBudget)
		userGroup.GET("/balance", mustLogin(), usageHandler.GetMyBalance)
		userGroup.GET("/rate_limit", mustLogin(), rateLimitHandler.GetMyRateLimit)

		// GPU deployment routes (user)
		userGroup.GET("/gpu/deployments", mustLogin(), gpuDeployHandler.ListMyDeployments)
		userGroup.POST("/gpu/deployments", mustLogin(), gpuDeployHandler.CreateDeployment)
		userGroup.PUT("/gpu/deployments/:id/stop", mustLogin(), gpuDeployHandler.StopDeployment)
		userGroup.DELETE("/gpu/deployments/:id", mustLogin(), gpuDeployHandler.DeleteDeployment)

		// HuggingFace import (user)
		userGroup.POST("/hf/import", mustLogin(), hfImportHandler.ImportHFModel)
		userGroup.GET("/hf/model-info", hfImportHandler.GetHFModelInfo) // public: no auth needed

		// Stripe recharge — AITRA-040 暂缓，路由未注册，stripe.go 保留备用
		// TODO: 启用时取消注释
		// if config.Payment.StripeSecretKey != "" {
		// 	stripeHandler, stripeErr := handler.NewStripeHandler(config)
		// 	if stripeErr == nil {
		// 		userGroup.POST("/recharge/checkout", mustLogin(), stripeHandler.CreateCheckoutSession)
		// 		apiV1Group.POST("/webhook/stripe", stripeHandler.HandleWebhook)
		// 	}
		// }

		// Model weights management (user)
		userGroup.GET("/model-weights", mustLogin(), modelWeightHandler.ListModelWeights)
		userGroup.GET("/model-weights/:repo_id/status", mustLogin(), modelWeightHandler.GetWeightStatus)
		userGroup.POST("/model-weights/:repo_id/sync", mustLogin(), modelWeightHandler.TriggerWeightSync)
		userGroup.GET("/model-weights/:repo_id/files", mustLogin(), modelWeightHandler.ListWeightFiles)
	}

	// Public routes — no auth required
	{
		publicGroup := r.Group("/api/v1/public")
		publicGroup.GET("/llm_configs", llmConfigHandler.ListPublicLLMConfigs)
		publicGroup.GET("/gpu/skus", gpuDeployHandler.ListPublicGPUSkus)
		publicGroup.GET("/model_stats", usageHandler.GetPublicModelStats)
		aiSkillHandler := handler.NewAISkillHandler()
		publicGroup.GET("/skills", aiSkillHandler.ListPublicSkills)

		// Sandbox public endpoints (SpaceHub page, no login needed)
		sandboxHandler := handler.NewSandboxHandler(config)
		publicGroup.GET("/sandbox/featured", sandboxHandler.ListFeaturedSpaces)
		publicGroup.GET("/sandbox/instances/:id/status", sandboxHandler.GetSandboxStatus)
	}

	middlewareCollection := middleware.MiddlewareCollection{}
	middlewareCollection.Auth.NeedLogin = mustLogin()
	middlewareCollection.Auth.NeedAdmin = mustLogin() // admin role check is done in handlers

	if err := extendRoutes(apiV1Group, middlewareCollection, config); err != nil {
		return nil, fmt.Errorf("error extending routes:%w", err)
	}

	return r, nil
}

func userMatch() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		currentUser := httpbase.GetCurrentUser(ctx)
		if currentUser == "" {
			httpbase.UnauthorizedError(ctx, errorx.ErrUserNotFound)
			ctx.Abort()
			return
		}

		userName := ctx.Param("username")
		if userName != currentUser {
			httpbase.UnauthorizedError(ctx, errorx.ErrUserNotMatch)
			slog.ErrorContext(ctx.Request.Context(), "user not match, try to query user account not owned", "currentUser", currentUser, "userName", userName)
			ctx.Abort()
			return
		}
	}
}

func mustLogin() gin.HandlerFunc {
	return func(ctx *gin.Context) {
		currentUser := httpbase.GetCurrentUser(ctx)
		if currentUser == "" {
			httpbase.UnauthorizedError(ctx, errorx.ErrUserNotFound)
			ctx.Abort()
			return
		}
	}
}

//go:build !ee && !saas

package component

import (
	"context"

	"github.com/aitra-ai/aitra-server/aigateway/types"
	"github.com/aitra-ai/aitra-server/builder/event"
	"github.com/aitra-ai/aitra-server/builder/store/cache"
	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/errorx"
	common_types "github.com/aitra-ai/aitra-server/common/types"
)

type extendOpenai struct {
	chargingEnabled bool
}

func NewOpenAIComponentFromConfig(config *config.Config) (OpenAIComponent, error) {
	cacheClient, err := cache.NewCache(context.Background(), cache.RedisConfig{
		Addr:     config.Redis.Endpoint,
		Username: config.Redis.User,
		Password: config.Redis.Password,
	})
	if err != nil {
		return nil, err
	}
	return &openaiComponentImpl{
		userStore:      database.NewUserStore(),
		organStore:     database.NewOrgStore(),
		deployStore:    database.NewDeployTaskStore(),
		eventPub:       &event.DefaultEventPublisher,
		extllmStore:    database.NewLLMConfigStore(config),
		modelListCache: cacheClient,
		extendOpenai:   extendOpenai{chargingEnabled: config.Accounting.ChargingEnable},
	}, nil
}

func (e *openaiComponentImpl) userPreference(ctx context.Context, req *types.UserPreferenceRequest) ([]types.Model, error) {
	return req.Models, nil
}

// parseScene parses the scene value from the HTTP header
// return SceneModelServerless
func parseScene(sceneValue string) common_types.SceneType {
	return common_types.SceneModelServerless
}

func (e *extendOpenai) CheckBalance(ctx context.Context, username string, model *types.Model, sceneValue string) error {
	userStore := database.NewUserStore()
	u, err := userStore.FindByUsername(ctx, username)
	if err != nil {
		return nil // Don't block on user lookup failure
	}

	// Check monthly budget (works regardless of charging_enable)
	if u.MonthlyBudget > 0 {
		usageStore := database.NewModelUsageLogStore(nil)
		spend, err := usageStore.MonthlySpend(ctx, u.ID)
		if err == nil && spend >= u.MonthlyBudget {
			return errorx.ErrBudgetExceeded
		}
	}

	// Skip credit balance check when charging is disabled
	if !e.chargingEnabled {
		return nil
	}
	// Only check balance for external models (which cost real money)
	if model.Provider == "" {
		return nil // Platform models: no credit check
	}
	creditStore := database.NewUserCreditStore()
	balance, err := creditStore.Balance(ctx, u.ID)
	if err != nil {
		return nil // Don't block on DB error
	}
	if balance <= 0 {
		return errorx.ErrInsufficientBalance
	}
	return nil
}

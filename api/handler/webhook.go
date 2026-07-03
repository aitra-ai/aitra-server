package handler

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/aitra-ai/aitra-server/api/httpbase"
	bldmq "github.com/aitra-ai/aitra-server/builder/mq"
	"github.com/aitra-ai/aitra-server/common/config"
	"github.com/aitra-ai/aitra-server/common/types"
	"github.com/aitra-ai/aitra-server/component"
)

type WebHookHandler struct {
	webhookComp component.WebHookComponent
}

func NewWebHookHandler(config *config.Config, mqFactory bldmq.MessageQueueFactory) (*WebHookHandler, error) {
	whcom, err := component.NewWebHookComponent(config, mqFactory)
	if err != nil {
		return nil, fmt.Errorf("failed to create webhook component error: %w", err)
	}
	err = whcom.DispatchWebHookEvent()
	if err != nil {
		return nil, fmt.Errorf("failed to dispatch webhook event error: %w", err)
	}
	return &WebHookHandler{
		webhookComp: whcom,
	}, nil
}

func (h *WebHookHandler) ReceiveRunnerWebHook(ctx *gin.Context) {
	var reqEvent types.WebHookRecvEvent

	if err := ctx.ShouldBindJSON(&reqEvent); err != nil {
		slog.ErrorContext(ctx.Request.Context(), "Bad request format for webhook event", slog.Any("error", err))
		httpbase.BadRequest(ctx, err.Error())
		return
	}

	slog.Debug("Received webhook event", slog.Any("event", reqEvent))

	err := h.webhookComp.HandleWebHook(ctx.Request.Context(), &reqEvent)
	if err != nil {
		slog.ErrorContext(ctx.Request.Context(), "Failed to handle webhook event", slog.Any("error", err))
		httpbase.ServerError(ctx, err)
		return
	}

	httpbase.OK(ctx, nil)
}

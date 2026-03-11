package handler

import (
	"fmt"
	"log/slog"

	"github.com/gin-gonic/gin"
	"opencsg.com/csghub-server/api/httpbase"
	"opencsg.com/csghub-server/common/config"
	"opencsg.com/csghub-server/user/component"
)

type LoginHandler struct {
	uc component.UserComponent
}

func NewLoginHandler(config *config.Config) (*LoginHandler, error) {
	uc, err := component.NewUserComponent(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create user component: %w", err)
	}
	return &LoginHandler{uc: uc}, nil
}

type PasswordLoginReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// PasswordLogin godoc
// @Summary      Login with username and password via Casdoor
// @Tags         Login
// @Accept       json
// @Produce      json
// @Param        body body PasswordLoginReq true "login credentials"
// @Success      200  {object} types.CreateJWTResp "OK"
// @Failure      400  {object} types.APIBadRequest "Bad request"
// @Failure      401  {object} types.APIBadRequest "Unauthorized"
// @Failure      500  {object} types.APIInternalServerError "Internal server error"
// @Router       /login [post]
func (h *LoginHandler) PasswordLogin(ctx *gin.Context) {
	var req PasswordLoginReq
	if err := ctx.ShouldBindJSON(&req); err != nil {
		slog.ErrorContext(ctx.Request.Context(), "Invalid login request", "error", err)
		httpbase.BadRequestWithExt(ctx, err)
		return
	}

	claims, signed, err := h.uc.SigninWithPassword(ctx.Request.Context(), req.Username, req.Password)
	if err != nil {
		slog.ErrorContext(ctx.Request.Context(), "Password login failed",
			"username", req.Username, "error", err)
		httpbase.UnauthorizedError(ctx, err)
		return
	}

	httpbase.OK(ctx, gin.H{
		"token":     signed,
		"expire_at": claims.ExpiresAt.Time,
		"username":  claims.CurrentUser,
	})
}

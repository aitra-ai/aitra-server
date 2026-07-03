package component

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/store/database"
	"github.com/aitra-ai/aitra-server/common/types"
)

type UserPhoneComponent interface {
	NeedPhoneChange() bool
	CanChangePhone(ctx context.Context, user *database.User, newPhone string) (bool, error)
	UpdatePhone(ctx context.Context, uid string, req types.UpdateUserPhoneRequest) error
	SendSMSCode(ctx context.Context, uid string, req types.SendSMSCodeRequest) (*types.SendSMSCodeResponse, error)
	SendPublicSMSCode(ctx context.Context, req types.SendPublicSMSCodeRequest) (*types.SendSMSCodeResponse, error)
	VerifyPublicSMSCode(ctx context.Context, req types.VerifyPublicSMSCodeRequest) error
}

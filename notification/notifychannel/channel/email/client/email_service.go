package client

import (
	"github.com/aitra-ai/aitra-server/common/types"
)

type EmailService interface {
	Send(req types.EmailReq) error
}

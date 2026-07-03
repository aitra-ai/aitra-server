package hook

import (
	"context"

	"github.com/aitra-ai/aitra-server/builder/rpc"
	"github.com/aitra-ai/aitra-server/common/types"
)

type Response struct {
	Status string `json:"status"`
}

type MessageSender interface {
	Send(ctx context.Context, message types.MessageRequest) (Response, error)
}
type MessageSenderImpl struct {
	client *rpc.HttpClient
}

func NewMessageSender(endpoint string, opts ...rpc.RequestOption) MessageSender {
	return &MessageSenderImpl{
		client: rpc.NewHttpClient(endpoint, opts...),
	}
}

func (m *MessageSenderImpl) Send(ctx context.Context, message types.MessageRequest) (Response, error) {
	var resp Response
	err := m.client.Post(ctx, "/api/v1/notifications", message, &resp)
	return resp, err
}

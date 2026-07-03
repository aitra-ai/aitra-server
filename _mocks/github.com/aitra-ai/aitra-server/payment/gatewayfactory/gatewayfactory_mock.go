package gatewayfactory

import (
	"github.com/stretchr/testify/mock"
	"github.com/aitra-ai/aitra-server/common/utils/payment/consts"
	"github.com/aitra-ai/aitra-server/payment/gateway"
)

type MockPaymentGatewayFactory struct {
	mock.Mock
}

func (m *MockPaymentGatewayFactory) GetPaymentGateway(paymentChannel consts.PaymentChannel) (gateway.PaymentGateway, error) {
	args := m.Called(paymentChannel)
	return args.Get(0).(gateway.PaymentGateway), args.Error(1)
}

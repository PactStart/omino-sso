package notification

import (
	"context"

	"github.com/omino/omino-sso/internal/store/model"
)

// mockProvider 模拟发送：不调用真实供应商、不产生费用，验证码固定 888888。
// 与真实实现同接口，业务逻辑零分叉（FR-017）。
type mockProvider struct{}

// NewMock 构造模拟发送实现。
func NewMock() Provider { return mockProvider{} }

func (mockProvider) Name() string { return model.SendProviderMock }

func (mockProvider) Send(_ context.Context, channel, _, _ string) error {
	if channel != ChannelSMS && channel != ChannelEmail {
		return ErrNotSupported
	}
	return nil
}

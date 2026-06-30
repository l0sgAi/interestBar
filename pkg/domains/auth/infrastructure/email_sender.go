package infrastructure

import (
	"context"

	"interestBar/pkg/domains/auth/domain"
	emailutil "interestBar/pkg/util/email"
)

// emailSenderImpl 基于 pkg/util/email 的 EmailSender 实现。
type emailSenderImpl struct{}

// NewEmailSender 构造一个基于全局 email client 的 EmailSender。
//
// 注意：旧 controller 在 client == nil 时返回 "Email service unavailable"，
// 这里保持一致：GetClient() 返回 nil 时返回 error，由 service/handler 映射为 500。
func NewEmailSender() domain.EmailSender {
	return &emailSenderImpl{}
}

// SendVerificationCode 调用全局 email client 发送验证码。
//
// 直接透传调用方传入的 ctx，使邮件发送遵循请求的取消信号与 trace 上下文。
func (e *emailSenderImpl) SendVerificationCode(ctx context.Context, email, code, lang string) error {
	client := emailutil.GetClient()
	if client == nil {
		return errEmailServiceUnavailable
	}
	return client.SendVerificationCode(ctx, email, code, lang)
}

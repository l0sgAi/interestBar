package infrastructure

import "errors"

// infrastructure 层的可识别错误。
var (
	errEmailServiceUnavailable = errors.New("email service unavailable")
	errInvalidTokenType        = errors.New("invalid oauth token type")
)

// IsEmailServiceUnavailableErr 判断是否为"邮件服务不可用"错误。
func IsEmailServiceUnavailableErr(err error) bool {
	return errors.Is(err, errEmailServiceUnavailable)
}

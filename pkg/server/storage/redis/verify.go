package redis

import (
	"fmt"
	"time"
)

const (
	registerCodeTTL     = 5 * time.Minute
	registerVerifiedTTL = 10 * time.Minute
	registerRateTTL     = 60 * time.Second
)

// 找回密码验证码与注册验证码复用同一套 TTL（5min / 10min / 60s），
// 但使用独立的 key 前缀（pwd_reset:*），避免两套流程互相覆盖。

// SetVerificationCode 存储注册验证码，有效期5分钟
func SetVerificationCode(email, code string) error {
	return Set(GetRegisterCodeKey(email), code, registerCodeTTL)
}

// GetVerificationCode 获取存储的注册验证码
func GetVerificationCode(email string) (string, error) {
	return Get(GetRegisterCodeKey(email))
}

// DeleteVerificationCode 删除注册验证码
func DeleteVerificationCode(email string) error {
	return Del(GetRegisterCodeKey(email))
}

// SetEmailVerified 标记邮箱已通过验证码校验，有效期10分钟
func SetEmailVerified(email string) error {
	return Set(GetRegisterVerifiedKey(email), "1", registerVerifiedTTL)
}

// IsEmailVerified 检查邮箱是否已通过验证
func IsEmailVerified(email string) (bool, error) {
	n, err := Exists(GetRegisterVerifiedKey(email))
	if err != nil {
		return false, fmt.Errorf("failed to check email verification status: %w", err)
	}
	return n > 0, nil
}

// DeleteEmailVerified 删除邮箱验证标记
func DeleteEmailVerified(email string) error {
	return Del(GetRegisterVerifiedKey(email))
}

// SetSendRateLimit 设置验证码发送频率限制，60秒内不可重发
func SetSendRateLimit(email string) error {
	return Set(GetRegisterRateKey(email), "1", registerRateTTL)
}

// CheckSendRateLimit 检查是否处于发送频率限制中，返回true表示受限
func CheckSendRateLimit(email string) (bool, error) {
	n, err := Exists(GetRegisterRateKey(email))
	if err != nil {
		return false, fmt.Errorf("failed to check send rate limit: %w", err)
	}
	return n > 0, nil
}

// ============ 找回密码验证码 ============

// SetPasswordResetCode 存储找回密码验证码，有效期5分钟
func SetPasswordResetCode(email, code string) error {
	return Set(GetPwdResetCodeKey(email), code, registerCodeTTL)
}

// GetPasswordResetCode 获取存储的找回密码验证码
func GetPasswordResetCode(email string) (string, error) {
	return Get(GetPwdResetCodeKey(email))
}

// DeletePasswordResetCode 删除找回密码验证码
func DeletePasswordResetCode(email string) error {
	return Del(GetPwdResetCodeKey(email))
}

// SetPasswordResetVerified 标记邮箱已通过找回密码验证码校验，有效期10分钟
func SetPasswordResetVerified(email string) error {
	return Set(GetPwdResetVerifiedKey(email), "1", registerVerifiedTTL)
}

// IsPasswordResetVerified 检查邮箱是否已通过找回密码验证
func IsPasswordResetVerified(email string) (bool, error) {
	n, err := Exists(GetPwdResetVerifiedKey(email))
	if err != nil {
		return false, fmt.Errorf("failed to check password reset verification status: %w", err)
	}
	return n > 0, nil
}

// DeletePasswordResetVerified 删除找回密码验证标记
func DeletePasswordResetVerified(email string) error {
	return Del(GetPwdResetVerifiedKey(email))
}

// SetPasswordResetRateLimit 设置找回密码验证码发送频率限制，60秒内不可重发
func SetPasswordResetRateLimit(email string) error {
	return Set(GetPwdResetRateKey(email), "1", registerRateTTL)
}

// CheckPasswordResetRateLimit 检查是否处于找回密码发送频率限制中，返回true表示受限
func CheckPasswordResetRateLimit(email string) (bool, error) {
	n, err := Exists(GetPwdResetRateKey(email))
	if err != nil {
		return false, fmt.Errorf("failed to check password reset rate limit: %w", err)
	}
	return n > 0, nil
}

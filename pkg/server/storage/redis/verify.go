package redis

import (
	"fmt"
	"time"
)

const (
	registerCodeTTL     = 5 * time.Minute
	registerVerifiedTTL = 5 * time.Minute // 收紧：原 10min，与验证码 TTL 对齐，缩小爆破窗口
	registerRateTTL     = 60 * time.Second

	// 验证码尝试硬上限（防爆破）
	maxVerifyAttempts = 5              // 单邮箱累计失败上限
	verifyLockoutTTL  = 15 * time.Minute // 达上限后锁定时长（即 attempts key 的 TTL）
)

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

// SetEmailVerified 标记邮箱已通过验证码校验，有效期 5 分钟
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

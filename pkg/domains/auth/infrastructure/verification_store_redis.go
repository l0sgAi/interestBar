package infrastructure

import (
	"interestBar/pkg/domains/auth/domain"
	redispkg "interestBar/pkg/server/storage/redis"
)

// verificationStoreRedis 基于 pkg/server/storage/redis 的 VerificationStore 实现。
type verificationStoreRedis struct{}

// NewVerificationStore 构造一个基于 Redis 的 VerificationStore。
func NewVerificationStore() domain.VerificationStore {
	return &verificationStoreRedis{}
}

func (v *verificationStoreRedis) SetCode(email, code string) error {
	return redispkg.SetVerificationCode(email, code)
}

func (v *verificationStoreRedis) GetCode(email string) (string, error) {
	return redispkg.GetVerificationCode(email)
}

func (v *verificationStoreRedis) DeleteCode(email string) error {
	return redispkg.DeleteVerificationCode(email)
}

func (v *verificationStoreRedis) MarkVerified(email string) error {
	return redispkg.SetEmailVerified(email)
}

func (v *verificationStoreRedis) IsVerified(email string) (bool, error) {
	return redispkg.IsEmailVerified(email)
}

func (v *verificationStoreRedis) DeleteVerified(email string) error {
	return redispkg.DeleteEmailVerified(email)
}

func (v *verificationStoreRedis) SetSendRateLimit(email string) error {
	return redispkg.SetSendRateLimit(email)
}

func (v *verificationStoreRedis) CheckSendRateLimit(email string) (bool, error) {
	return redispkg.CheckSendRateLimit(email)
}

// VerifyAttempt 转发到 redis 原子脚本，并映射状态枚举（domain 不依赖 redis 包）。
func (v *verificationStoreRedis) VerifyAttempt(email, code string) (domain.VerifyAttemptResult, error) {
	o, err := redispkg.AtomicVerifyAttempt(email, code)
	if err != nil {
		return domain.VerifyAttemptResult{}, err
	}
	var st domain.VerifyAttemptStatus
	switch o.Status {
	case redispkg.VerifyAttemptOK:
		st = domain.VerifyStatusOK
	case redispkg.VerifyAttemptWrong:
		st = domain.VerifyStatusWrong
	case redispkg.VerifyAttemptLocked:
		st = domain.VerifyStatusLocked
	default:
		st = domain.VerifyStatusExpired
	}
	return domain.VerifyAttemptResult{Status: st, Remaining: o.Remaining}, nil
}

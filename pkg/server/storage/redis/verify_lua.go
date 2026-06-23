package redis

import (
	"fmt"
)

// verifyAttemptScript 原子地完成「判锁 → 取码 → 比对 → 失败计数/置 verified」。
//
// 一次 EVALSHA 执行，杜绝并发竞态（攻击者无法靠高频并发绕过尝试上限）。
//
// KEYS:
//   KEYS[1] = register:code:{email}       验证码
//   KEYS[2] = register:attempts:{email}   失败尝试计数 / 锁定标记
//   KEYS[3] = register:verified:{email}   已校验标记
//
// ARGV:
//   ARGV[1] = inputCode        用户输入的验证码
//   ARGV[2] = maxAttempts      失败上限（如 5）
//   ARGV[3] = lockoutTTL_sec   锁定时长（attempts key 的 TTL）
//   ARGV[4] = verifiedTTL_sec  verified 标记的 TTL
//
// 返回 {code, value}:
//   code:  1=ok, 0=wrong, -1=locked, -2=expired
//   value: ok→0; wrong→剩余次数; locked→剩余锁定秒数; expired→0
const verifyAttemptScript = `
local inputCode    = ARGV[1]
local maxAttempts  = tonumber(ARGV[2])
local lockoutTTL   = tonumber(ARGV[3])
local verifiedTTL  = tonumber(ARGV[4])

-- 1. 锁定检查：失败次数已达上限，直接拒
local attempts = tonumber(redis.call('GET', KEYS[2]) or '0') or 0
if attempts >= maxAttempts then
    local ttl = redis.call('TTL', KEYS[2])
    if ttl < 0 then ttl = lockoutTTL end
    return {-1, ttl}
end

-- 2. 验证码存在性
local storedCode = redis.call('GET', KEYS[1])
if not storedCode then
    return {-2, 0}
end

-- 3. 比对
if storedCode == inputCode then
    -- 成功：清码 + 清失败计数 + 置 verified
    redis.call('DEL', KEYS[1])
    redis.call('DEL', KEYS[2])
    redis.call('SET', KEYS[3], '1', 'EX', verifiedTTL)
    return {1, 0}
end

-- 4. 失败：计数（仅首次设 TTL，非滑动）
local n = redis.call('INCR', KEYS[2])
if n == 1 then
    redis.call('EXPIRE', KEYS[2], lockoutTTL)
end
if n >= maxAttempts then
    -- 达上限：删码强制重发，返回 wrong 但剩余 0
    redis.call('DEL', KEYS[1])
    return {0, 0}
end
return {0, maxAttempts - n}
`

var verifyAttemptSHA string

// VerifyAttemptStatus 原子校验的状态枚举。
type VerifyAttemptStatus string

const (
	VerifyAttemptOK      VerifyAttemptStatus = "ok"      // 校验通过，verified 已置
	VerifyAttemptWrong   VerifyAttemptStatus = "wrong"   // 验证码错误，仍有剩余次数
	VerifyAttemptLocked  VerifyAttemptStatus = "locked"  // 失败次数耗尽，已锁定
	VerifyAttemptExpired VerifyAttemptStatus = "expired" // 验证码已过期/不存在
)

// VerifyAttemptOutcome 原子校验结果。
//
//   Status    = ok/wrong/locked/expired
//   Remaining = wrong 时为剩余尝试次数；locked 时为剩余锁定秒数；其余为 0
type VerifyAttemptOutcome struct {
	Status    VerifyAttemptStatus
	Remaining int
}

// InitVerifyAttemptLuaScripts 预加载验证码校验 Lua 脚本（启动时调用）。
func InitVerifyAttemptLuaScripts() error {
	var err error
	verifyAttemptSHA, err = Client.ScriptLoad(ctx, verifyAttemptScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load verify attempt script: %w", err)
	}
	return nil
}

// AtomicVerifyAttempt 原子校验注册验证码（含失败计数与锁定）。
//
// 返回结果状态机见 VerifyAttemptOutcome。Redis 不可用等异常返回 error，
// 调用方应降级（建议提示用户稍后重试或重新发送验证码），不应默认放行。
func AtomicVerifyAttempt(email, inputCode string) (VerifyAttemptOutcome, error) {
	keys := []string{
		GetRegisterCodeKey(email),
		GetRegisterAttemptsKey(email),
		GetRegisterVerifiedKey(email),
	}
	args := []interface{}{
		inputCode,
		maxVerifyAttempts,
		int64(verifyLockoutTTL.Seconds()),
		int64(registerVerifiedTTL.Seconds()),
	}

	code, value, err := execVerifyAttempt(keys, args)
	if err != nil {
		return VerifyAttemptOutcome{}, err
	}

	out := VerifyAttemptOutcome{Remaining: value}
	switch code {
	case 1:
		out.Status = VerifyAttemptOK
	case 0:
		out.Status = VerifyAttemptWrong
	case -1:
		out.Status = VerifyAttemptLocked
	default:
		out.Status = VerifyAttemptExpired
	}
	return out, nil
}

// execVerifyAttempt 执行 EvalSha，处理 NOSCRIPT 重载重试，解析 {code, value} 返回。
func execVerifyAttempt(keys []string, args []interface{}) (int, int, error) {
	res, err := Client.EvalSha(ctx, verifyAttemptSHA, keys, args...).Result()
	if err != nil {
		// SHA 失效（Redis 重启/主从切换），重新加载并重试一次
		verifyAttemptSHA, err = Client.ScriptLoad(ctx, verifyAttemptScript).Result()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to reload verify attempt script: %w", err)
		}
		res, err = Client.EvalSha(ctx, verifyAttemptSHA, keys, args...).Result()
		if err != nil {
			return 0, 0, fmt.Errorf("failed to execute verify attempt: %w", err)
		}
	}

	// Lua 返回 {code, value} → go-redis 解析为 []interface{}{int64, int64}
	arr, ok := res.([]interface{})
	if !ok || len(arr) < 2 {
		return 0, 0, fmt.Errorf("unexpected verify attempt script result: %v", res)
	}
	code, err := toInt(arr[0])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid verify attempt code: %w", err)
	}
	value, err := toInt(arr[1])
	if err != nil {
		return 0, 0, fmt.Errorf("invalid verify attempt value: %w", err)
	}
	return code, value, nil
}

// toInt 把 Lua 返回的元素（int64）转成 int。
func toInt(v interface{}) (int, error) {
	n, ok := v.(int64)
	if !ok {
		return 0, fmt.Errorf("not int64: %T", v)
	}
	return int(n), nil
}

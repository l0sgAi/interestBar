package redis

import (
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// discover 发现页候选池（LIST）+ 版本 token（string）。
//
// 与 feed:recommend:* 同构：DEL 旧池 → RPUSH 有序 IDs → 写版本 token（池与 token 同 TTL）。
// 区别：discover 是「random_score 随机采样」，recommend 是「5 路召回」；
// discover 区分登录（discover:{section}:{uid} 反气泡排除后）与匿名共享（discover:anon:{section} 纯随机）。
// 翻页用 LRANGE offset；token 供客户端回传比对，不一致则池已重建 → 回 offset=0（防翻页错位）。
// 设计见 docs/discover-design.md §四。

// anonUserKey 匿名用户的 userKey 字面量（与登录 user_id 字符串区分）。
const anonUserKey = "anon"

// BuildDiscoverPool 原子重建发现页候选池：DEL 旧池 → RPUSH 有序 IDs → 写版本 token，池与 token 同 TTL。
// userKey=user_id 字符串（登录）或 "anon"（匿名）；section="posts"|"circles"。
// 返回新建的 token。ids 为空时仍建空池（调用方按需降级）。
func BuildDiscoverPool(userKey, section string, ids []string, ttl time.Duration) (string, error) {
	token := uuid.NewString()
	listKey := discoverListKey(userKey, section)
	tokenKey := GetDiscoverTokenKey(userKey)

	pipe := Client.TxPipeline()
	pipe.Del(ctx, listKey)
	if len(ids) > 0 {
		vals := make([]interface{}, 0, len(ids))
		for _, id := range ids {
			vals = append(vals, id)
		}
		pipe.RPush(ctx, listKey, vals...)
	}
	pipe.Set(ctx, tokenKey, token, ttl) // token 自带 TTL
	pipe.Expire(ctx, listKey, ttl)      // list 需单独 EXPIRE
	if _, err := pipe.Exec(ctx); err != nil {
		return "", fmt.Errorf("failed to build discover pool: %w", err)
	}
	return token, nil
}

// DiscoverPoolExists 候选池 LIST 是否存在（未过 TTL）。
func DiscoverPoolExists(userKey, section string) (bool, error) {
	n, err := Client.Exists(ctx, discoverListKey(userKey, section)).Result()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// DiscoverPoolLen 候选池当前长度（LLEN）。
func DiscoverPoolLen(userKey, section string) (int64, error) {
	return Client.LLen(ctx, discoverListKey(userKey, section)).Result()
}

// DiscoverPoolRange 取候选池 [offset, offset+size-1] 区间的 ID（LRANGE）。
func DiscoverPoolRange(userKey, section string, offset, size int64) ([]string, error) {
	if size <= 0 {
		return nil, nil
	}
	return Client.LRange(ctx, discoverListKey(userKey, section), offset, offset+size-1).Result()
}

// GetDiscoverPoolToken 读取候选池版本 token；池不存在时返回 ""（触发调用方按 miss 处理）。
func GetDiscoverPoolToken(userKey string) (string, error) {
	t, err := Client.Get(ctx, GetDiscoverTokenKey(userKey)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return "", nil
		}
		return "", err
	}
	return t, nil
}

// ScanActiveDiscoverUsers 扫描近期活跃登录用户（SCAN discover:token:* 但排除匿名），
// 返回 user_id 字符串列表（仍在 TTL 内的 token 视为活跃）。
// 用于 DiscoverPoolSyncer 决定刷新哪些登录用户池。
// count 是 SCAN 提示（默认 100）。
func ScanActiveDiscoverUsers(count int64) ([]string, error) {
	if count <= 0 {
		count = 100
	}
	var (
		cursor uint64
		result []string
	)
	prefix := DiscoverTokenPrefix
	prefixLen := len(prefix)
	for {
		// MATCH discover:token:* 扫描所有 token key。
		keys, c, err := Client.Scan(ctx, cursor, prefix+"*", count).Result()
		if err != nil {
			return nil, fmt.Errorf("failed to scan active discover users: %w", err)
		}
		for _, k := range keys {
			if len(k) <= prefixLen {
				continue
			}
			uid := k[prefixLen:] // 去掉前缀得 userKey
			if uid == anonUserKey {
				continue // 跳过匿名 token
			}
			result = append(result, uid)
		}
		if c == 0 {
			break
		}
		cursor = c
	}
	return result, nil
}

// discoverListKey 根据是否匿名返回登录池或匿名共享池的完整 key。
func discoverListKey(userKey, section string) string {
	if userKey == anonUserKey {
		return GetDiscoverAnonKey(section)
	}
	return GetDiscoverPoolKey(section, userKey)
}

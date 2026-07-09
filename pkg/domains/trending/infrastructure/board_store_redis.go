package infrastructure

import (
	"context"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"interestBar/pkg/domains/trending/domain"
	redispkg "interestBar/pkg/server/storage/redis"
)

// boardStoreRedis 热点榜单 ZSET 存储（无状态，委托 redispkg 全局 Client）。
//
// 与 circle:hot:{cid} Δ 累加器（circle_hot_syncer 用 GETDEL 读后清零）的根本区别：
// 那是「增量累加 → 定时落库」；本榜是「全量重算 → 覆盖快照」，故用 DEL+ZADD 原子覆盖、
// 不设 TTL、不清零。设计见 docs/trending-design.md §四。
type boardStoreRedis struct{}

// NewBoardStore 构造 BoardStore。
func NewBoardStore() domain.BoardStore {
	return &boardStoreRedis{}
}

// Range 按热度降序读取 [offset, offset+size) 的实体 + 分数。
//
// ZREVRANGE key start stop WITHSCORES：返回已按 score 降序；
// 同 score 按 member 字典序（uuid 字符串），可接受。
func (s *boardStoreRedis) Range(ctx context.Context, dimension, window string, offset, size int64) ([]domain.ScoredID, error) {
	if size <= 0 {
		return nil, nil
	}
	key := redispkg.GetTrendingKey(dimension, window)
	stop := offset + size - 1
	zs, err := redispkg.Client.ZRevRangeWithScores(ctx, key, offset, stop).Result()
	if err != nil {
		return nil, err
	}
	out := make([]domain.ScoredID, 0, len(zs))
	for _, z := range zs {
		member, ok := z.Member.(string)
		if !ok {
			continue
		}
		id, parseErr := uuid.Parse(member)
		if parseErr != nil {
			continue
		}
		out = append(out, domain.ScoredID{ID: id, Score: z.Score})
	}
	return out, nil
}

// RefreshedAt 返回榜单最近刷新 Unix 秒（key 不存在/解析失败返回 0）。
func (s *boardStoreRedis) RefreshedAt(ctx context.Context, dimension, window string) (int64, error) {
	v, err := redispkg.Client.Get(ctx, redispkg.GetTrendingMetaKey(dimension, window)).Result()
	if err != nil {
		// key 不存在返回 0（从未刷新）；其它错也降级为 0，避免读路径因 meta 失败而 5xx。
		return 0, nil
	}
	ts, parseErr := strconv.ParseInt(v, 10, 64)
	if parseErr != nil {
		return 0, nil
	}
	return ts, nil
}

// Rewrite 覆盖式重写榜单：DEL 旧 → ZADD 入本轮 Top-N → 记刷新时间戳。
//
// TxPipeline 保证原子；job 并发只有一个（syncer 单 goroutine + mu 互斥），无竞态。
func (s *boardStoreRedis) Rewrite(ctx context.Context, dimension, window string, items []domain.ScoredID) error {
	key := redispkg.GetTrendingKey(dimension, window)
	metaKey := redispkg.GetTrendingMetaKey(dimension, window)

	pipe := redispkg.Client.TxPipeline()
	pipe.Del(ctx, key) // 先清空，避免旧成员残留（ZADD 无法删除已落榜者）

	if len(items) > 0 {
		members := make([]redis.Z, 0, len(items))
		for _, it := range items {
			members = append(members, redis.Z{Score: it.Score, Member: it.ID.String()})
		}
		pipe.ZAdd(ctx, key, members...)
	}
	pipe.Set(ctx, metaKey, time.Now().Unix(), 0)

	_, err := pipe.Exec(ctx)
	return err
}

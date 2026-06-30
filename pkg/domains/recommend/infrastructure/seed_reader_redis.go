package infrastructure

import (
	"context"
	"fmt"

	"interestBar/pkg/domains/recommend/domain"
	redispkg "interestBar/pkg/server/storage/redis"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// seedReaderRedis 基于 redispkg ZSET 的 SeedReader 实现（用户 like/collect/view 种子 + CF 相似召回）。
type seedReaderRedis struct{}

// NewSeedReader 构造 SeedReader。
func NewSeedReader() domain.SeedReader {
	return &seedReaderRedis{}
}

func parseIDs(raw []string) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(raw))
	for _, s := range raw {
		if id, perr := uuid.Parse(s); perr == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (r *seedReaderRedis) LikedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.ListPostLikedIDs(userID, int64(limit))
	if err != nil {
		return nil, err
	}
	return parseIDs(raw), nil
}

func (r *seedReaderRedis) CollectedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	raw, err := redispkg.ListPostCollectedIDs(userID, int64(limit))
	if err != nil {
		return nil, err
	}
	return parseIDs(raw), nil
}

func (r *seedReaderRedis) ViewedPostIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error) {
	_ = ctx
	entries, _, err := redispkg.ListPostViews(userID, 0, int64(limit))
	if err != nil {
		return nil, err
	}
	raw := make([]string, 0, len(entries))
	for _, e := range entries {
		raw = append(raw, e.ID)
	}
	return parseIDs(raw), nil
}

// CFSimilar 对每个 seed 帖 pipeline 读 cf:item:{seed} ZSET top-N 相似帖，聚合 candidate→Σ相似度。
//
// 单次 pipeline 打包所有 seed 的 ZREVRANGEWITHSCORES（1 RTT）；score 累加体现「被多个 seed 指向」的强度。
// 调用方按 score 倒序取 top 作为 C5 路输出。
func (r *seedReaderRedis) CFSimilar(ctx context.Context, seedPostIDs []uuid.UUID, topNPerSeed int) (map[uuid.UUID]float64, error) {
	if len(seedPostIDs) == 0 || topNPerSeed <= 0 {
		return nil, nil
	}
	cmds := make([]*redis.ZSliceCmd, 0, len(seedPostIDs))
	pipe := redispkg.Client.Pipeline()
	for _, seed := range seedPostIDs {
		key := redispkg.GetCFItemKey(seed)
		cmds = append(cmds, pipe.ZRevRangeWithScores(ctx, key, 0, int64(topNPerSeed-1)))
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("failed to read cf:item for seeds: %w", err)
	}

	scoreByCandidate := make(map[uuid.UUID]float64)
	for _, cmd := range cmds {
		members, err := cmd.Result()
		if err != nil {
			continue
		}
		for _, z := range members {
			s, ok := z.Member.(string)
			if !ok {
				continue
			}
			cid, perr := uuid.Parse(s)
			if perr != nil {
				continue
			}
			scoreByCandidate[cid] += z.Score
		}
	}
	return scoreByCandidate, nil
}

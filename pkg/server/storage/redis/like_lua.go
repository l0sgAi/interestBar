package redis

import (
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

const likeToggleScript = `
local statsKey = KEYS[1]
local zsetKey = KEYS[2]
local targetId = ARGV[1]
local now = tonumber(ARGV[2])
local maxSize = tonumber(ARGV[3])
local ttl = tonumber(ARGV[4])

-- Check if the target is already liked (exists in ZSET)
local score = redis.call('ZSCORE', zsetKey, targetId)

if score then
    -- Unlike: remove from ZSET, decrement count
    redis.call('ZREM', zsetKey, targetId)
    local newCount = redis.call('HINCRBY', statsKey, 'like_count', -1)
    if tonumber(newCount) < 0 then
        redis.call('HSET', statsKey, 'like_count', 0)
    end
    -- Renew TTL on both keys
    redis.call('EXPIRE', statsKey, ttl)
    redis.call('EXPIRE', zsetKey, ttl)
    return -1
else
    -- Like: add to ZSET with current timestamp, increment count
    redis.call('ZADD', zsetKey, now, targetId)
    redis.call('HINCRBY', statsKey, 'like_count', 1)
    -- Evict oldest entries if ZSET exceeds max size
    local zsetSize = redis.call('ZCARD', zsetKey)
    if tonumber(zsetSize) > maxSize then
        local removeCount = tonumber(zsetSize) - maxSize
        redis.call('ZREMRANGEBYRANK', zsetKey, 0, removeCount - 1)
    end
    -- Renew TTL on both keys
    redis.call('EXPIRE', statsKey, ttl)
    redis.call('EXPIRE', zsetKey, ttl)
    return 1
end
`

var likeToggleSHA string

// ToggleLikeResult 点赞切换操作结果
type ToggleLikeResult int

const (
	ToggleLikeLiked   ToggleLikeResult = 1
	ToggleLikeUnliked ToggleLikeResult = -1
)

// InitLikeLuaScripts 预加载 Lua 脚本到 Redis（启动时调用）
func InitLikeLuaScripts() error {
	var err error
	likeToggleSHA, err = Client.ScriptLoad(ctx, likeToggleScript).Result()
	if err != nil {
		return fmt.Errorf("failed to load like toggle script: %w", err)
	}
	return nil
}

// ToggleCommentLike 原子切换评论点赞状态
func ToggleCommentLike(userID, commentID int64) (ToggleLikeResult, error) {
	statsKey := GetCommentStatsKey(commentID)
	zsetKey := GetUserCommentLikeListKey(userID)
	return executeLikeToggle(statsKey, zsetKey, commentID)
}

// TogglePostLike 原子切换帖子点赞状态
func TogglePostLike(userID, postID int64) (ToggleLikeResult, error) {
	statsKey := GetPostStatsKey(postID)
	zsetKey := GetUserPostLikeListKey(userID)
	return executeLikeToggle(statsKey, zsetKey, postID)
}

func executeLikeToggle(statsKey, zsetKey string, targetID int64) (ToggleLikeResult, error) {
	now := time.Now().UnixMilli()
	maxZsetSize := int64(2000)
	ttlSeconds := int64(postStatsTTL.Seconds())

	result, err := Client.EvalSha(ctx, likeToggleSHA,
		[]string{statsKey, zsetKey},
		targetID, now, maxZsetSize, ttlSeconds,
	).Int64()

	if err != nil {
		// If script SHA is missing (Redis restarted), reload and retry
		likeToggleSHA, err = Client.ScriptLoad(ctx, likeToggleScript).Result()
		if err != nil {
			return 0, fmt.Errorf("failed to reload like toggle script: %w", err)
		}
		result, err = Client.EvalSha(ctx, likeToggleSHA,
			[]string{statsKey, zsetKey},
			targetID, now, maxZsetSize, ttlSeconds,
		).Int64()
		if err != nil {
			return 0, fmt.Errorf("failed to execute like toggle: %w", err)
		}
	}

	return ToggleLikeResult(result), nil
}

// BatchCheckCommentLiked 批量检查用户是否点赞了多条评论
// ZMScore 对不存在的成员返回 float64(0)，由于我们的 score 是时间戳(>0)，score==0 等价于不存在
func BatchCheckCommentLiked(userID int64, commentIDs []int64) (map[int64]bool, error) {
	if len(commentIDs) == 0 {
		return make(map[int64]bool), nil
	}

	zsetKey := GetUserCommentLikeListKey(userID)
	members := make([]string, len(commentIDs))
	for i, id := range commentIDs {
		members[i] = strconv.FormatInt(id, 10)
	}

	scores, err := Client.ZMScore(ctx, zsetKey, members...).Result()
	if err != nil {
		return nil, fmt.Errorf("failed to batch check comment liked: %w", err)
	}

	result := make(map[int64]bool, len(commentIDs))
	for i, score := range scores {
		result[commentIDs[i]] = score > 0
	}

	// Renew TTL on the ZSET since we accessed it
	Client.Expire(ctx, zsetKey, postStatsTTL)

	return result, nil
}

// GetCommentLikedMissIDs 从 BatchCheckCommentLiked 结果中提取缓存未命中的ID列表
func GetCommentLikedMissIDs(commentIDs []int64, likedMap map[int64]bool) []int64 {
	var missIDs []int64
	for _, id := range commentIDs {
		if !likedMap[id] {
			missIDs = append(missIDs, id)
		}
	}
	return missIDs
}

// BatchCheckPostLiked 批量检查用户是否点赞了多个帖子
func BatchCheckPostLiked(userID int64, postIDs []int64) (map[int64]bool, []int64, error) {
	if len(postIDs) == 0 {
		return make(map[int64]bool), nil, nil
	}

	zsetKey := GetUserPostLikeListKey(userID)
	members := make([]string, len(postIDs))
	for i, id := range postIDs {
		members[i] = strconv.FormatInt(id, 10)
	}

	scores, err := Client.ZMScore(ctx, zsetKey, members...).Result()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to batch check post liked: %w", err)
	}

	result := make(map[int64]bool, len(postIDs))
	var missIDs []int64
	for i, score := range scores {
		if score > 0 {
			result[postIDs[i]] = true
		} else {
			result[postIDs[i]] = false
			missIDs = append(missIDs, postIDs[i])
		}
	}

	Client.Expire(ctx, zsetKey, postStatsTTL)
	return result, missIDs, nil
}

// BackfillCommentLikes 将DB查询确认的点赞状态回填到ZSET
func BackfillCommentLikes(userID int64, likedCommentIDs []int64) error {
	if len(likedCommentIDs) == 0 {
		return nil
	}
	zsetKey := GetUserCommentLikeListKey(userID)
	now := float64(time.Now().UnixMilli())
	members := make([]redis.Z, len(likedCommentIDs))
	for i, id := range likedCommentIDs {
		members[i] = redis.Z{Score: now, Member: strconv.FormatInt(id, 10)}
	}
	pipe := Client.Pipeline()
	pipe.ZAdd(ctx, zsetKey, members...)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}

// BackfillPostLikes 将DB查询确认的帖子点赞状态回填到ZSET
func BackfillPostLikes(userID int64, likedPostIDs []int64) error {
	if len(likedPostIDs) == 0 {
		return nil
	}
	zsetKey := GetUserPostLikeListKey(userID)
	now := float64(time.Now().UnixMilli())
	members := make([]redis.Z, len(likedPostIDs))
	for i, id := range likedPostIDs {
		members[i] = redis.Z{Score: now, Member: strconv.FormatInt(id, 10)}
	}
	pipe := Client.Pipeline()
	pipe.ZAdd(ctx, zsetKey, members...)
	pipe.Expire(ctx, zsetKey, postStatsTTL)
	_, err := pipe.Exec(ctx)
	return err
}

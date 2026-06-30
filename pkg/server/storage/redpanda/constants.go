package redpanda

import (
	"github.com/google/uuid"
)

// 统计类型常量
const (
	// StatisticsTypeCircleCount 圈子成员计数类型
	StatisticsTypeCircleCount = "circle_count"
	// StatisticsTypePostCount 圈子帖子计数类型
	StatisticsTypePostCount = "post_count"

	// 帖子统计类型常量
	StatisticsTypePostView    = "post_view_count"
	StatisticsTypePostLike    = "post_like_count"
	StatisticsTypePostCollect = "post_collect_count"
)

// CircleStatisticsMessage 圈子统计消息
type CircleStatisticsMessage struct {
	Type     string    `json:"type"`      // 统计类型: "circle_count" 或 "post_count"
	CircleID uuid.UUID `json:"circle_id"` // 圈子ID
	Value    int64     `json:"value"`     // 变化值: 1 或 -1
}

// PostStatisticsMessage 帖子统计消息
type PostStatisticsMessage struct {
	Type   string    `json:"type"`    // 统计类型: "post_view_count", "post_comment_count" 等
	PostID uuid.UUID `json:"post_id"` // 帖子ID
	Value  int64     `json:"value"`   // 变化值: 1 或 -1
}

// PostHotMessage 帖子热度增量消息。
//
// Delta 是事件时已 ApplyHotDelta 计算（权重 × 方向 × clamp）后的最终签名 Δ；
// 消费者只累加 postID → ΣΔ 批量落库，不再做权重/cap 判断（cap 在源头 Lua 原子保证）。
type PostHotMessage struct {
	PostID uuid.UUID `json:"post_id"` // 帖子ID
	Delta  int64     `json:"delta"`   // 已 clamp 的热度增量（可正可负）
}

// InteractionAction 互动动作类型（CF 协同过滤交互矩阵的动作标签）。
type InteractionAction string

const (
	InteractionView        InteractionAction = "view"         // 浏览（隐式弱）
	InteractionLike        InteractionAction = "like"         // 帖子点赞
	InteractionCollect     InteractionAction = "collect"      // 帖子收藏（最强）
	InteractionComment     InteractionAction = "comment"      // 评论
	InteractionCommentLike InteractionAction = "comment_like" // 评论点赞（冗余 post_id）
)

// 互动权重（CF 隐反馈评分表 weight 列取值，max-ever）。
const (
	InteractionWeightView        int16 = 1
	InteractionWeightCommentLike int16 = 2
	InteractionWeightLike        int16 = 3
	InteractionWeightComment     int16 = 4
	InteractionWeightCollect     int16 = 5
)

// PostInteractionMessage 帖子互动事件消息（CF 灌数）。
//
// 每个正向互动（点赞/收藏/评论/评论点赞/浏览）由对应 event publisher 额外发布；
// InteractionConsumer 批量 ON CONFLICT GREATEST upsert 到 domains.post_interaction。
// 仅正向互动发：取消赞/收藏不删行（隐反馈哲学），故不发布负向消息。
type PostInteractionMessage struct {
	UserID uuid.UUID         `json:"user_id"` // 互动用户ID
	PostID uuid.UUID         `json:"post_id"` // 被互动帖子ID
	Action InteractionAction `json:"action"`  // 动作类型（仅作可观测标签，落库只用 weight）
	Weight int16             `json:"weight"`  // 信号强度（1..5）
	Ts     int64             `json:"ts"`      // 事件时间 Unix 毫秒
}

// LikeEventMessage 点赞事件消息
type LikeEventMessage struct {
	Type     string    `json:"type"` // "comment_like" 或 "post_like"
	UserID   uuid.UUID `json:"user_id"`
	TargetID uuid.UUID `json:"target_id"` // commentId 或 postId
	PostID   uuid.UUID `json:"post_id"`   // 仅评论点赞时使用（冗余字段）
	Amount   int64     `json:"amount"`    // 1=点赞, -1=取消点赞
}

// CollectEventType 收藏事件类型（固定为帖子收藏，评论无收藏语义）。
const CollectEventType = "post_collect"

// CollectEventMessage 收藏事件消息
type CollectEventMessage struct {
	Type   string    `json:"type"` // 固定 "post_collect"
	UserID uuid.UUID `json:"user_id"`
	PostID uuid.UUID `json:"post_id"` // 被收藏的帖子ID
	Amount int64     `json:"amount"`  // 1=收藏, -1=取消收藏
}

// HistoryEventType 浏览历史事件类型（固定为帖子浏览,评论无浏览历史语义）。
const HistoryEventType = "post_view"

// HistoryEventMessage 浏览历史事件消息
type HistoryEventMessage struct {
	Type   string    `json:"type"` // 固定 "post_view"
	UserID uuid.UUID `json:"user_id"`
	PostID uuid.UUID `json:"post_id"` // 被浏览的帖子ID
}

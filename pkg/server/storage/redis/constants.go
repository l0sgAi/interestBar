package redis

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Redis 相关键名常量定义

const (
	// RegisterCodePrefix 注册验证码缓存key前缀
	// 完整key格式: register:code:{email}
	// 存储内容：6位数字验证码，TTL 5分钟
	RegisterCodePrefix = "register:code:"

	// RegisterVerifiedPrefix 注册邮箱已验证标记key前缀
	// 完整key格式: register:verified:{email}
	// 存储内容：标记邮箱已通过验证码校验，TTL 10分钟
	RegisterVerifiedPrefix = "register:verified:"

	// RegisterRatePrefix 注册验证码发送频率限制key前缀
	// 完整key格式: register:rate:{email}
	// 存储内容：频率限制标记，TTL 60秒
	RegisterRatePrefix = "register:rate:"
)

const (
	// CircleInfoPrefix 圈子基础信息缓存key前缀
	// 完整key格式: circle:info:{circle_id}
	// 包含字段：id, name, slug, avatar_url, cover_url, description, rule, creator_id, category_id, join_type, status, deleted, create_time, update_time
	CircleInfoPrefix = "circle:info:"

	// CircleStatsPrefix 圈子统计信息Hash key前缀
	// 完整key格式: circle:stats:{circle_id}
	// 包含字段：member_count, post_count, hot
	CircleStatsPrefix = "circle:stats:"

	// UserInfoPrefix 用户基础信息缓存key前缀
	// 完整key格式: user:info:{user_id}
	// 包含字段：id, username, email, phone, google_id, github_id, avatar_url, gender, birthdate, status, role, deleted, create_time, update_time
	UserInfoPrefix = "user:info:"

	// UserJoinedCirclesPrefix 用户已加入圈子 ZSET 缓存key前缀
	// 完整key格式: circle:joined:{user_id}
	// ZSET: member=circle_id, score=加入时间 Unix 毫秒，倒序（最近加入在前）
	// 注：前缀从 user_joined_circles: 改为 circle:joined:，避免旧 string 类型 key
	// 残留导致 ZSET 操作 WRONGTYPE（旧 key 任其 24h TTL 过期）。
	UserJoinedCirclesPrefix = "circle:joined:"
)

// CircleBaseInfo 圈子基础信息（不含统计信息）
type CircleBaseInfo struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	CoverURL    string     `json:"cover_url,omitempty"`
	Description string     `json:"description"`
	Rule        string     `json:"rule,omitempty"`
	CreatorID   uuid.UUID  `json:"creator_id"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	JoinType    int16      `json:"join_type"`
	Status      int16      `json:"status"`
	Deleted     int16      `json:"deleted"`
	CreateTime  time.Time  `json:"create_time"`
	UpdateTime  time.Time  `json:"update_time"`
}

// CircleStatistics 圈子统计信息（用于批量更新和MQ消费等场景）
// 注意：实际读取时直接从3个独立计数器读取，不使用此结构体
type CircleStatistics struct {
	MemberCount int `json:"member_count"` // 成员数量
	PostCount   int `json:"post_count"`   // 帖子数量
	Hot         int `json:"hot"`          // 热度
}

// PostStatsPrefix 帖子统计信息Hash key前缀
// 完整key格式: post:stats:{post_id}
// 包含字段：view_count, comment_count, like_count, collect_count
const PostStatsPrefix = "post:stats:"

// PostStatistics 帖子统计信息
type PostStatistics struct {
	ViewCount    int `json:"view_count"`    // 浏览量
	CommentCount int `json:"comment_count"` // 评论数
	LikeCount    int `json:"like_count"`    // 点赞数
	CollectCount int `json:"collect_count"` // 收藏数
}

// GetUserJoinedCirclesKey 获取用户已加入圈子ID列表缓存的完整key
func GetUserJoinedCirclesKey(userID uuid.UUID) string {
	return UserJoinedCirclesPrefix + userID.String()
}

// GetUserInfoKey 获取用户基础信息缓存的完整key
func GetUserInfoKey(userID uuid.UUID) string {
	return UserInfoPrefix + userID.String()
}

// GetCircleInfoKey 获取圈子基础信息缓存的完整key
func GetCircleInfoKey(circleID uuid.UUID) string {
	return CircleInfoPrefix + circleID.String()
}

// GetCircleStatsKey 获取圈子统计信息Hash缓存的完整key
func GetCircleStatsKey(circleID uuid.UUID) string {
	return CircleStatsPrefix + circleID.String()
}

// GetPostStatsKey 获取帖子统计信息Hash缓存的完整key
func GetPostStatsKey(postID uuid.UUID) string {
	return PostStatsPrefix + postID.String()
}

// GetPostViewDedupeKey 获取帖子浏览去重 key
func GetPostViewDedupeKey(postID, userID uuid.UUID) string {
	return fmt.Sprintf("%s%s:%s", PostViewDedupePrefix, postID.String(), userID.String())
}

// PostViewDedupePrefix 帖子浏览去重 key 前缀
// 完整 key: post:viewdedup:{postID}:{userID}
// TTL: 5 分钟，同一用户对同一帖子在此窗口内只计一次浏览
const PostViewDedupePrefix = "post:viewdedup:"

// CommentStatsPrefix 评论统计信息Hash key前缀
// 完整key格式: comment:stats:{comment_id}
// 包含字段：like_count
const CommentStatsPrefix = "comment:stats:"

// UserCommentLikeListPrefix 用户评论点赞列表ZSET key前缀
// 完整key格式: user:like:comments:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: commentId(UUID字符串)
const UserCommentLikeListPrefix = "user:like:comments:"

// UserPostLikeListPrefix 用户帖子点赞列表ZSET key前缀
// 完整key格式: user:like:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
const UserPostLikeListPrefix = "user:like:posts:"

// GetCommentStatsKey 获取评论统计信息Hash缓存的完整key
func GetCommentStatsKey(commentID uuid.UUID) string {
	return CommentStatsPrefix + commentID.String()
}

// GetUserCommentLikeListKey 获取用户评论点赞列表ZSET的完整key
func GetUserCommentLikeListKey(userID uuid.UUID) string {
	return UserCommentLikeListPrefix + userID.String()
}

// GetUserPostLikeListKey 获取用户帖子点赞列表ZSET的完整key
func GetUserPostLikeListKey(userID uuid.UUID) string {
	return UserPostLikeListPrefix + userID.String()
}

// UserPostCollectListPrefix 用户帖子收藏列表ZSET key前缀
// 完整key格式: user:collect:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
const UserPostCollectListPrefix = "user:collect:posts:"

// GetUserPostCollectListKey 获取用户帖子收藏列表ZSET的完整key
func GetUserPostCollectListKey(userID uuid.UUID) string {
	return UserPostCollectListPrefix + userID.String()
}

// UserPostViewListPrefix 用户浏览历史列表ZSET key前缀
// 完整key格式: user:view:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId(UUID字符串)
// score 倒序即「最近浏览」顺序;cap 500(超限按最低 score 淘汰);TTL 复用 postStatsTTL。
const UserPostViewListPrefix = "user:view:posts:"

// GetUserPostViewListKey 获取用户浏览历史列表ZSET的完整key
func GetUserPostViewListKey(userID uuid.UUID) string {
	return UserPostViewListPrefix + userID.String()
}

// GetRegisterCodeKey 获取注册验证码缓存的完整key
func GetRegisterCodeKey(email string) string {
	return RegisterCodePrefix + email
}

// GetRegisterVerifiedKey 获取注册邮箱已验证标记的完整key
func GetRegisterVerifiedKey(email string) string {
	return RegisterVerifiedPrefix + email
}

// GetRegisterRateKey 获取注册验证码发送频率限制的完整key
func GetRegisterRateKey(email string) string {
	return RegisterRatePrefix + email
}

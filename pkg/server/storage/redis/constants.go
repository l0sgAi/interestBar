package redis

import (
	"fmt"
	"time"
)

// Redis 相关键名常量定义

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

	// UserJoinedCirclesPrefix 用户已加入圈子ID列表缓存key前缀
	// 完整key格式: user_joined_circles:{user_id}
	// 存储内容：[]int64 圈子ID列表，按加入时间倒序排列
	UserJoinedCirclesPrefix = "user_joined_circles:"
)

// CircleBaseInfo 圈子基础信息（不含统计信息）
type CircleBaseInfo struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Slug        string    `json:"slug,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	CoverURL    string    `json:"cover_url,omitempty"`
	Description string    `json:"description"`
	Rule        string    `json:"rule,omitempty"`
	CreatorID   int64     `json:"creator_id"`
	CategoryID  int       `json:"category_id"`
	JoinType    int16     `json:"join_type"`
	Status      int16     `json:"status"`
	Deleted     int16     `json:"deleted"`
	CreateTime  time.Time `json:"create_time"`
	UpdateTime  time.Time `json:"update_time"`
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
func GetUserJoinedCirclesKey(userID int64) string {
	return UserJoinedCirclesPrefix + fmt.Sprint(userID)
}

// GetUserInfoKey 获取用户基础信息缓存的完整key
func GetUserInfoKey(userID int64) string {
	return UserInfoPrefix + fmt.Sprint(userID)
}

// GetCircleInfoKey 获取圈子基础信息缓存的完整key
func GetCircleInfoKey(circleID int64) string {
	return CircleInfoPrefix + fmt.Sprint(circleID)
}

// GetCircleStatsKey 获取圈子统计信息Hash缓存的完整key
func GetCircleStatsKey(circleID int64) string {
	return CircleStatsPrefix + fmt.Sprint(circleID)
}

// GetPostStatsKey 获取帖子统计信息Hash缓存的完整key
func GetPostStatsKey(postID int64) string {
	return PostStatsPrefix + fmt.Sprint(postID)
}

// GetPostViewDedupeKey 获取帖子浏览去重 key
func GetPostViewDedupeKey(postID, userID int64) string {
	return fmt.Sprintf("%s%d:%d", PostViewDedupePrefix, postID, userID)
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
// Score: 最后访问时间戳(Unix毫秒), Member: commentId
const UserCommentLikeListPrefix = "user:like:comments:"

// UserPostLikeListPrefix 用户帖子点赞列表ZSET key前缀
// 完整key格式: user:like:posts:{user_id}
// Score: 最后访问时间戳(Unix毫秒), Member: postId
const UserPostLikeListPrefix = "user:like:posts:"

// GetCommentStatsKey 获取评论统计信息Hash缓存的完整key
func GetCommentStatsKey(commentID int64) string {
	return CommentStatsPrefix + fmt.Sprint(commentID)
}

// GetUserCommentLikeListKey 获取用户评论点赞列表ZSET的完整key
func GetUserCommentLikeListKey(userID int64) string {
	return UserCommentLikeListPrefix + fmt.Sprint(userID)
}

// GetUserPostLikeListKey 获取用户帖子点赞列表ZSET的完整key
func GetUserPostLikeListKey(userID int64) string {
	return UserPostLikeListPrefix + fmt.Sprint(userID)
}

package redis

import (
	"fmt"
	"time"
)

// Redis 相关键名常量定义

const (
	// CircleMemberCountPrefix 圈子成员数缓存key前缀
	// 完整key格式: circle_member_count:{circle_id}
	CircleMemberCountPrefix = "circle_member_count:"

	// CircleInfoPrefix 圈子基础信息缓存key前缀
	// 完整key格式: circle:info:{circle_id}
	// 包含字段：id, name, slug, avatar_url, cover_url, description, rule, creator_id, category_id, join_type, status, deleted, create_time, update_time
	CircleInfoPrefix = "circle:info:"

	// CirclePostCountPrefix 圈子帖子数缓存key前缀
	// 完整key格式: circle_post_count:{circle_id}
	CirclePostCountPrefix = "circle_post_count:"

	// CircleHotPrefix 圈子热度缓存key前缀
	// 完整key格式: circle_hot:{circle_id}
	CircleHotPrefix = "circle_hot:"

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

// GetUserJoinedCirclesKey 获取用户已加入圈子ID列表缓存的完整key
func GetUserJoinedCirclesKey(userID int64) string {
	return UserJoinedCirclesPrefix + fmt.Sprint(userID)
}

// GetCircleMemberCountKey 获取圈子成员数缓存的完整key
func GetCircleMemberCountKey(circleID int64) string {
	return CircleMemberCountPrefix + fmt.Sprint(circleID)
}

// GetCircleInfoKey 获取圈子基础信息缓存的完整key
func GetCircleInfoKey(circleID int64) string {
	return CircleInfoPrefix + fmt.Sprint(circleID)
}

// GetCirclePostCountKey 获取圈子帖子数缓存的完整key
func GetCirclePostCountKey(circleID int64) string {
	return CirclePostCountPrefix + fmt.Sprint(circleID)
}

// GetCircleHotKey 获取圈子热度缓存的完整key
func GetCircleHotKey(circleID int64) string {
	return CircleHotPrefix + fmt.Sprint(circleID)
}

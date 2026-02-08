package redis

import "fmt"

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

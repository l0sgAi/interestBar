package redis

import "fmt"

// Redis 相关键名常量定义

const (
	// CircleMemberCountPrefix 圈子成员数缓存key前缀
	// 完整key格式: circle_member_count:{circle_id}
	CircleMemberCountPrefix = "circle_member_count:"
)

// GetCircleMemberCountKey 获取圈子成员数缓存的完整key
func GetCircleMemberCountKey(circleID int64) string {
	return CircleMemberCountPrefix + fmt.Sprint(circleID)
}

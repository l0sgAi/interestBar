package redpanda

// 统计类型常量
const (
	// StatisticsTypeCircleCount 圈子成员计数类型
	StatisticsTypeCircleCount = "circle_count"
	// StatisticsTypePostCount 帖子计数类型
	StatisticsTypePostCount = "post_count"
)

// CircleStatisticsMessage 圈子统计消息
type CircleStatisticsMessage struct {
	Type     string `json:"type"`     // 统计类型: "circle_count" 或 "post_count"
	CircleID int64  `json:"circle_id"` // 圈子ID
	Value    int64  `json:"value"`    // 变化值: 1 或 -1
}

package redpanda

// 统计类型常量
const (
	// StatisticsTypeCircleCount 圈子成员计数类型
	StatisticsTypeCircleCount = "circle_count"
	// StatisticsTypePostCount 帖子计数类型
	StatisticsTypePostCount = "post_count"

	// 帖子统计类型常量
	StatisticsTypePostView    = "post_view_count"
	StatisticsTypePostComment = "post_comment_count"
	StatisticsTypePostLike    = "post_like_count"
	StatisticsTypePostCollect = "post_collect_count"
)

// CircleStatisticsMessage 圈子统计消息
type CircleStatisticsMessage struct {
	Type     string `json:"type"`      // 统计类型: "circle_count" 或 "post_count"
	CircleID int64  `json:"circle_id"` // 圈子ID
	Value    int64  `json:"value"`     // 变化值: 1 或 -1
}

// PostStatisticsMessage 帖子统计消息
type PostStatisticsMessage struct {
	Type   string `json:"type"`    // 统计类型: "post_view_count", "post_comment_count" 等
	PostID int64  `json:"post_id"` // 帖子ID
	Value  int64  `json:"value"`   // 变化值: 1 或 -1
}

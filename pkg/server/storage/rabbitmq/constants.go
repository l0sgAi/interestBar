package rabbitmq

// RabbitMQ 相关常量定义

const (
	// CircleSyncExchange 圈子同步交换机
	CircleSyncExchange = "circle_sync_exchange"

	// CircleSyncQueue 圈子同步队列
	CircleSyncQueue = "circle_sync_queue"

	// CircleSyncRoutingKey 圈子同步路由键
	CircleSyncRoutingKey = "circle.sync"
)

// CircleSyncAction 圈子同步操作类型
const (
	CircleSyncActionCreate = "create"
	CircleSyncActionUpdate = "update"
	CircleSyncActionDelete = "delete"
)

// 成员计数相关常量
const (
	// CircleMemberCountExchange 圈子成员计数交换机
	CircleMemberCountExchange = "circle_member_count_exchange"

	// CircleMemberCountQueue 圈子成员计数队列
	CircleMemberCountQueue = "circle_member_count_queue"

	// CircleMemberCountRoutingKey 圈子成员计数路由键
	CircleMemberCountRoutingKey = "circle.member.count"
)

// JoinMsg 圈子成员加入/退出消息
type JoinMsg struct {
	CircleID int64 `json:"circle_id"` // 圈子ID
	IsJoin   int64 `json:"is_join"`   // 1表示加入，-1表示退出
}

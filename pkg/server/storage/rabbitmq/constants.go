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

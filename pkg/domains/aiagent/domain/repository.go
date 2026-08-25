package domain

import (
	"context"
	"errors"

	"github.com/google/uuid"
)

// 哨兵错误。
var (
	// ErrAgentNotFound 机器人不存在（含已软删）。
	ErrAgentNotFound = errors.New("agent not found")
)

// AgentRepository 是 aiagent 领域的持久化接口（由 infrastructure 实现）。
type AgentRepository interface {
	// Create 创建机器人。name 撞唯一索引 idx_ai_agent_name 时返回底层错误
	//（调用方先 ExistsByName 预检，并发撞库按 DB 兜底处理）。
	Create(ctx context.Context, agent *AiAgent) error
	// GetByID 根据 ID 获取机器人（未删除）。未找到返回 ErrAgentNotFound。
	GetByID(ctx context.Context, agentID uuid.UUID) (*AiAgent, error)
	// ExistsByName 检查名称是否已被占用（未删除）。excludeID 用于更新时排除自身（创建时传 uuid.Nil）。
	ExistsByName(ctx context.Context, name string, excludeID uuid.UUID) (bool, error)
	// ListByOffset offset 分页获取机器人列表（未删除，按创建时间倒序）。
	// 返回列表与总数（总数供分页响应）。
	ListByOffset(ctx context.Context, offset, limit int) ([]AiAgent, int64, error)
	// UpdateFields 按 map 更新指定字段（部分更新）。
	UpdateFields(ctx context.Context, agentID uuid.UUID, fields map[string]interface{}) error
	// SoftDelete 软删（deleted=1 且 status=0，一并停用）。
	SoftDelete(ctx context.Context, agentID uuid.UUID) error
}

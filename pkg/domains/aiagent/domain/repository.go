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
	// ErrReplyAlreadyExists 机器人对该帖已有回复日志（唯一索引兜底，含失败行）。
	ErrReplyAlreadyExists = errors.New("reply log already exists for agent and post")
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
	// keyword 非空时按 name 模糊过滤（ILIKE %kw%）。返回列表与总数（总数供分页响应）。
	ListByOffset(ctx context.Context, keyword string, offset, limit int) ([]AiAgent, int64, error)
	// UpdateFields 按 map 更新指定字段（部分更新）。
	UpdateFields(ctx context.Context, agentID uuid.UUID, fields map[string]interface{}) error
	// SoftDelete 软删（deleted=1 且 status=0，一并停用）。
	SoftDelete(ctx context.Context, agentID uuid.UUID) error
	// ListEnabled 获取全部启用中的机器人（未删除且 status=1）。
	// 供回复执行链路加载触发候选（表小，走 idx_ai_agent_active 部分索引）。
	ListEnabled(ctx context.Context) ([]AiAgent, error)
	// ExistsByLinkedUserID 检查某系统用户是否为机器人的关联账号（未删除）。
	// 供评论触发钩子反查，机器人自己的评论不再触发关键词回复（防回环）。
	ExistsByLinkedUserID(ctx context.Context, userID uuid.UUID) (bool, error)
}

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
	// ErrCircleAgentLimit 圈内机器人数量已达上限（每圈 MaxAgentsPerCircle 个）。
	// 由 CreateInCircle 事务内计数返回（应用层预检无法防并发，行锁后计数是唯一可靠闸门）。
	ErrCircleAgentLimit = errors.New("circle agent limit exceeded")
)

// AgentRepository 是 aiagent 领域的持久化接口（由 infrastructure 实现）。
type AgentRepository interface {
	// Create 创建机器人（平台全局）。name 撞唯一索引 idx_ai_agent_name 时返回底层错误
	//（调用方先 ExistsByNameInScope 预检，并发撞库按 DB 兜底处理）。
	Create(ctx context.Context, agent *AiAgent) error
	// GetByID 根据 ID 获取机器人（未删除）。未找到返回 ErrAgentNotFound。
	GetByID(ctx context.Context, agentID uuid.UUID) (*AiAgent, error)
	// ExistsByNameInScope 检查 (作用域, name) 是否被占用（未删除）。
	// circleID=uuid.Nil 查全局桶（circle_id IS NULL），否则查该圈桶，
	// 与部分唯一索引 idx_ai_agent_name 的 (COALESCE(circle_id, 全零UUID), name) 分桶一致。
	// excludeID 用于更新时排除自身（创建时传 uuid.Nil）。
	ExistsByNameInScope(ctx context.Context, circleID uuid.UUID, name string, excludeID uuid.UUID) (bool, error)
	// CountByCircleIDs 批量统计各圈未删除机器人数（可管理圈子列表 agent_count 回填用）。
	// 无机器人的圈不在返回 map 中（调用方按 0 处理）。
	CountByCircleIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]int, error)
	// ListByOffset offset 分页获取**全局**机器人列表（未删除，按创建时间倒序）。
	// keyword 非空时按 name 模糊过滤（ILIKE %kw%）。返回列表与总数（总数供分页响应）。
	ListByOffset(ctx context.Context, keyword string, offset, limit int) ([]AiAgent, int64, error)
	// ListByCircle 圈内机器人 offset 分页（未删除，按创建时间倒序）。
	// keyword 非空时按 name 模糊过滤（ILIKE %kw%），语义对齐 ListByOffset。
	ListByCircle(ctx context.Context, circleID uuid.UUID, keyword string, offset, limit int) ([]AiAgent, int64, error)
	// CreateInCircle 单事务创建**圈内**机器人：事务内 SELECT circle 行 FOR UPDATE
	// 把同圈并发创建串行化 → 计数 >= maxPerCircle 返回 ErrCircleAgentLimit → 插入。
	// （PG 无"每组行数上限"约束，先查后插的预检防不了并发，行锁是唯一可靠闸门；
	// 圈行存续期长——软删不物理删行，锁目标稳定。）名称撞唯一索引由 DB 兜底
	//（调用方已 ExistsByNameInScope 预检）。circle_id/creator_id 由调用方写入 agent。
	CreateInCircle(ctx context.Context, agent *AiAgent, maxPerCircle int) error
	// UpdateFields 按 map 更新指定字段（部分更新）。
	UpdateFields(ctx context.Context, agentID uuid.UUID, fields map[string]interface{}) error
	// SoftDelete 软删（deleted=1 且 status=0，一并停用）。
	SoftDelete(ctx context.Context, agentID uuid.UUID) error
	// ListEnabled 获取全部启用中的**全局**机器人（未删除且 status=1 且 circle_id IS NULL）。
	// 供回复执行链路加载触发候选（表小，走 idx_ai_agent_active 部分索引）。
	// circle_id IS NULL 是防泄漏护栏：圈子级机器人创建后不得进入全站触发链。
	ListEnabled(ctx context.Context) ([]AiAgent, error)
	// ExistsByLinkedUserID 检查某系统用户是否为机器人的关联账号（未删除）。
	// 供评论触发钩子反查，机器人自己的评论不再触发关键词回复（防回环）。
	// 不加 circle 过滤：防回环口径应覆盖全部机器人（含圈子级，语义前瞻正确）。
	ExistsByLinkedUserID(ctx context.Context, userID uuid.UUID) (bool, error)
}

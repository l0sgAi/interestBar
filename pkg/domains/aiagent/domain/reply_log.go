package domain

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// ReplyLog AI 回复日志（表 domains.ai_agent_reply_log，append-only）。
//
// 每次真实发起回复调用链路（LLM 调用/评论落库）写一行终态记录，失败不重试、
// 不更新旧行。频率限制按全部行（含失败）统计；(agent_id, post_id) 唯一索引
// 在 DB 层兜底「一个机器人对一帖至多回复一次」。
type ReplyLog struct {
	ID               uuid.UUID  `json:"id" gorm:"type:uuid;primaryKey;column:id"`
	AgentID          uuid.UUID  `json:"agent_id" gorm:"column:agent_id;type:uuid;not null"`
	PostID           uuid.UUID  `json:"post_id" gorm:"column:post_id;type:uuid;not null"`
	CommentID        *uuid.UUID `json:"comment_id,omitempty" gorm:"column:comment_id;type:uuid"` // 失败时为 NULL
	UserID           uuid.UUID  `json:"user_id" gorm:"column:user_id;type:uuid;not null"`        // 帖子作者ID（冗余，风控分析用）
	Status           int16      `json:"status" gorm:"column:status;type:smallint;not null"`      // 0=失败, 1=成功
	ErrorMsg         string     `json:"error_msg,omitempty" gorm:"column:error_msg;type:varchar(2048)"`
	LatencyMs        int        `json:"latency_ms" gorm:"column:latency_ms;not null;default:0"`
	PromptTokens     int        `json:"prompt_tokens" gorm:"column:prompt_tokens;not null;default:0"`
	CompletionTokens int        `json:"completion_tokens" gorm:"column:completion_tokens;not null;default:0"`
	CreateTime       time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
}

// TableName 指定表名。
func (ReplyLog) TableName() string { return "domains.ai_agent_reply_log" }

// 回复结果状态常量。
const (
	ReplyStatusFailed = 0
	ReplyStatusOK     = 1
)

// ReplyLogRepository 是回复日志的持久化接口（由 infrastructure 实现）。
type ReplyLogRepository interface {
	// Create 插入一条终态日志（append-only，不设防重）。
	Create(ctx context.Context, log *ReplyLog) error
	// CountSinceByAgent 统计机器人自 since 以来的日志行数（含失败，限频口径）。
	CountSinceByAgent(ctx context.Context, agentID uuid.UUID, since time.Time) (int64, error)
	// GetLastByAgent 取机器人最新一条日志（算最小回复间隔）。无日志返回 nil, nil。
	GetLastByAgent(ctx context.Context, agentID uuid.UUID) (*ReplyLog, error)
}

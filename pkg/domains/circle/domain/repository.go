package domain

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

// ErrCircleNotFound 圈子未找到（与旧 gorm.ErrRecordNotFound 对应）。
var ErrCircleNotFound = errors.New("circle not found")

// ErrMemberNotFound 成员记录未找到。
var ErrMemberNotFound = errors.New("member not found")

// CircleRepository 是 circle 领域的持久化接口（由 infrastructure 实现）。
type CircleRepository interface {
	// GetByID 根据 ID 获取圈子（仅未删除）。未找到返回 ErrCircleNotFound。
	GetByID(ctx context.Context, circleID uuid.UUID) (*Circle, error)
	// GetByIDs 批量获取圈子。
	GetByIDs(ctx context.Context, circleIDs []uuid.UUID) (map[uuid.UUID]*Circle, error)
	// ExistsByName 检查同名圈子是否已存在（未删除）。
	ExistsByName(ctx context.Context, name string) (bool, error)
	// ExistsBySlug 检查同 slug 圈子是否已存在。
	ExistsBySlug(ctx context.Context, slug string) (bool, error)
	// Create 创建圈子并自动将创建者设为圈主（事务）。
	Create(ctx context.Context, circle *Circle) error
}

// MemberRepository 是圈子成员关系的持久化接口。
type MemberRepository interface {
	// GetMember 获取成员信息。未找到返回 ErrMemberNotFound。
	GetMember(ctx context.Context, circleID, userID uuid.UUID) (*CircleMember, error)
	// GetJoinedCircleIDs 获取用户加入的圈子 ID 列表（按加入时间倒序）。
	// limit=0 表示不限制。
	GetJoinedCircleIDs(ctx context.Context, userID uuid.UUID, limit int) ([]uuid.UUID, error)
	// JoinCircle 用户加入圈子（含状态机：pending/left/banned → normal 等）。
	// 返回错误信息与旧 model.JoinCircle 一致（"user is already a member..." 等）。
	JoinCircle(ctx context.Context, circleID, userID uuid.UUID, joinType int16) (*CircleMember, error)
	// LeaveCircle 用户退出圈子（圈主不能退）。
	LeaveCircle(ctx context.Context, circleID, userID uuid.UUID) error
}

// CircleBaseCache 圈子基础信息缓存（不含统计）。
type CircleBaseCache interface {
	// GetBase 从缓存读取圈子基础信息。未命中返回 nil, nil。
	GetBase(ctx context.Context, circleID uuid.UUID) (*CircleBaseInfo, error)
	// SetBase 写入圈子基础信息缓存。
	SetBase(ctx context.Context, circleID uuid.UUID, info *CircleBaseInfo) error
}

// CircleStatsCache 圈子统计信息缓存（member_count/post_count/hot）。
type CircleStatsCache interface {
	// GetStats 读取统计信息。未命中返回 nil, nil（触发 DB 恢复）。
	GetStats(ctx context.Context, circleID uuid.UUID) (*CircleStatistics, error)
	// StatsExists 检查统计 Hash 是否存在。
	StatsExists(ctx context.Context, circleID uuid.UUID) (bool, error)
	// SetStats 设置统计信息（用于从 DB 恢复）。
	SetStats(ctx context.Context, circleID uuid.UUID, stats *CircleStatistics) error
	// IncrMemberCount / DecrMemberCount 原子增减成员计数。
	IncrMemberCount(ctx context.Context, circleID uuid.UUID) error
	DecrMemberCount(ctx context.Context, circleID uuid.UUID) error
	// IncrPostCount 原子增加帖子计数。
	IncrPostCount(ctx context.Context, circleID uuid.UUID) error
}

// JoinedCirclesCache 用户已加入圈子 ID 列表的缓存（旁路缓存）。
type JoinedCirclesCache interface {
	// GetJoined 获取用户加入的圈子 ID 列表。未命中返回 nil, nil。
	GetJoined(ctx context.Context, userID uuid.UUID) ([]uuid.UUID, error)
	// SetJoined 写入用户加入的圈子 ID 列表。
	SetJoined(ctx context.Context, userID uuid.UUID, circleIDs []uuid.UUID) error
	// InvalidateJoined 删除用户加入圈子缓存（join/leave 后调用）。
	InvalidateJoined(ctx context.Context, userID uuid.UUID) error
}

// CircleEventPublisher 圈子事件发布（用于异步持久化计数到 DB）。
type CircleEventPublisher interface {
	// PublishMemberCount 发布成员计数变化消息（+1 表示加入，-1 表示退出）。
	PublishMemberCount(ctx context.Context, circleID uuid.UUID, delta int64) error
	// PublishPostCount 发布帖子计数 +1 消息（新发帖）。
	PublishPostCount(ctx context.Context, circleID uuid.UUID) error
}

// CircleBrief 是给跨领域调用的圈子精简视图（Facade DTO）。
//
// post 领域组装帖子列表时需要圈子名称/头像，依赖此视图而非完整 Circle。
type CircleBrief struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url,omitempty"`
}

// CircleBaseInfo 圈子基础信息（不含统计，对应旧 redispkg.CircleBaseInfo）。
type CircleBaseInfo struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug,omitempty"`
	AvatarURL   string     `json:"avatar_url,omitempty"`
	CoverURL    string     `json:"cover_url,omitempty"`
	Description string     `json:"description"`
	Rule        string     `json:"rule,omitempty"`
	CreatorID   uuid.UUID  `json:"creator_id"`
	CategoryID  *uuid.UUID `json:"category_id,omitempty"`
	JoinType    int16      `json:"join_type"`
	Status      int16      `json:"status"`
	Deleted     int16      `json:"deleted"`
	CreateTime  time.Time  `json:"create_time"`
	UpdateTime  time.Time  `json:"update_time"`
}

// CircleStatistics 圈子统计信息（对应旧 redispkg.CircleStatistics）。
type CircleStatistics struct {
	MemberCount int `json:"member_count"`
	PostCount   int `json:"post_count"`
	Hot         int `json:"hot"`
}

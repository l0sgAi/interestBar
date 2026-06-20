package domain

import (
	"context"

	"github.com/google/uuid"
)

// UserRepository 是 user 领域的持久化接口（由 infrastructure 实现）。
//
// 所有方法都接收 context.Context，便于超时/取消与链路追踪透传。
type UserRepository interface {
	// GetByID 根据用户 ID 查询（仅未删除）。未找到返回 nil, nil。
	GetByID(ctx context.Context, userID uuid.UUID) (*SysUser, error)
	// GetByEmail 根据邮箱查询。未找到返回 nil, nil。
	GetByEmail(ctx context.Context, email string) (*SysUser, error)
	// GetByIDs 批量查询，返回以 ID 为 key 的 map（便于组装列表）。
	GetByIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]*SysUser, error)
	// Create 创建用户。
	Create(ctx context.Context, user *SysUser) error
	// UpdateFields 按 map 更新指定字段（用于 UpdateProfile 部分字段更新）。
	UpdateFields(ctx context.Context, userID uuid.UUID, fields map[string]interface{}) error
	// GetByProviderIDOrEmail 按 OAuth provider ID 或 email 查询（OAuth 登录用）。
	// lookupField 是 db 列名（google_id/github_id/microsoft_id）。
	GetByProviderIDOrEmail(ctx context.Context, lookupField, providerID, email string) (*SysUser, error)
	// Save 整行保存（OAuth 首次登录后补 provider_id 用）。
	Save(ctx context.Context, user *SysUser) error
}

// UserCache 是 user 领域的缓存接口（可选，由 infrastructure 实现）。
type UserCache interface {
	// GetUser 从缓存读取用户信息。未命中返回 nil, nil。
	GetUser(ctx context.Context, userID uuid.UUID) (*SysUser, error)
	// SetUser 写入用户信息缓存。
	SetUser(ctx context.Context, userID uuid.UUID, user *SysUser) error
}

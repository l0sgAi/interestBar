// Package infrastructure 提供 user 领域基础设施层实现：PostgreSQL 持久化 + Redis 缓存。
package infrastructure

import (
	"context"
	"errors"

	"interestBar/pkg/domains/user/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// userRepoPG 基于 GORM 的 UserRepository 实现。
type userRepoPG struct {
	db *gorm.DB
}

// NewUserRepository 构造一个基于 PostgreSQL 的 UserRepository。
func NewUserRepository(db *gorm.DB) domain.UserRepository {
	return &userRepoPG{db: db}
}

// GetByID 根据用户 ID 查询（仅未删除）。
func (r *userRepoPG) GetByID(ctx context.Context, userID uuid.UUID) (*domain.SysUser, error) {
	var user domain.SysUser
	err := r.db.WithContext(ctx).
		Where("id = ? AND deleted = ?", userID, 0).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetByEmail 根据邮箱查询。
func (r *userRepoPG) GetByEmail(ctx context.Context, email string) (*domain.SysUser, error) {
	var user domain.SysUser
	err := r.db.WithContext(ctx).
		Where("email = ? AND deleted = ?", email, 0).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetByIDs 批量查询。
func (r *userRepoPG) GetByIDs(ctx context.Context, userIDs []uuid.UUID) (map[uuid.UUID]*domain.SysUser, error) {
	if len(userIDs) == 0 {
		return make(map[uuid.UUID]*domain.SysUser), nil
	}

	var users []domain.SysUser
	err := r.db.WithContext(ctx).
		Where("id IN ? AND deleted = ?", userIDs, 0).
		Find(&users).Error
	if err != nil {
		return nil, err
	}

	userMap := make(map[uuid.UUID]*domain.SysUser, len(users))
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}
	return userMap, nil
}

// Create 创建用户。
func (r *userRepoPG) Create(ctx context.Context, user *domain.SysUser) error {
	return r.db.WithContext(ctx).Create(user).Error
}

// UpdateFields 按 map 更新指定字段。
//
// 用 Updates(map) 而非 Updates(struct) 是为了支持零值字段更新
// （如 phone=nil 删除手机号），与旧 controller 行为一致。
func (r *userRepoPG) UpdateFields(ctx context.Context, userID uuid.UUID, fields map[string]interface{}) error {
	return r.db.WithContext(ctx).
		Model(&domain.SysUser{}).
		Where("id = ? AND deleted = ?", userID, 0).
		Updates(fields).Error
}

// GetByProviderIDOrEmail 按 OAuth provider ID 或 email 查询。
//
// 对应旧 oauth.go 中的查询：
//
//	WHERE (google_id = ? OR email = ?) AND deleted = 0
func (r *userRepoPG) GetByProviderIDOrEmail(ctx context.Context, lookupField, providerID, email string) (*domain.SysUser, error) {
	var user domain.SysUser
	err := r.db.WithContext(ctx).
		Where("("+lookupField+" = ? OR email = ?) AND deleted = ?", providerID, email, 0).
		First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// Save 整行保存。
func (r *userRepoPG) Save(ctx context.Context, user *domain.SysUser) error {
	return r.db.WithContext(ctx).Save(user).Error
}

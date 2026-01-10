package model

import (
	"encoding/json"
	"errors"
	"fmt"
	"interestBar/pkg/server/storage/redis"
	"time"

	"gorm.io/gorm"
)

type SysUser struct {
	ID         int64      `json:"id" gorm:"primarykey;column:id"`
	CreateTime time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
	Username   string     `json:"username" gorm:"column:username;not null"`
	Email      string     `json:"email" gorm:"column:email;unique;not null"`
	Phone      string     `json:"phone,omitempty" gorm:"column:phone"`
	GoogleID   string     `json:"google_id,omitempty" gorm:"column:google_id"`
	XID        string     `json:"x_id,omitempty" gorm:"column:x_id"`
	GithubID   string     `json:"github_id,omitempty" gorm:"column:github_id"`
	AvatarURL  string     `json:"avatar_url,omitempty" gorm:"column:avatar_url"`
	Gender     int        `json:"gender" gorm:"column:gender;default:0"`
	Birthdate  *time.Time `json:"birthdate,omitempty" gorm:"column:birthdate"`
	Status     int        `json:"status" gorm:"column:status;default:1"`
	Role       int        `json:"role" gorm:"column:role;default:0"`
	Deleted    int        `json:"deleted" gorm:"column:deleted;default:0"`
}

func (SysUser) TableName() string {
	return "users"
}

const (
	// UserCachePrefix 用户信息缓存前缀
	UserCachePrefix = "user:info:"
	// UserCacheExpiration 用户信息缓存过期时间（30分钟）
	UserCacheExpiration = 30 * time.Minute
)

// GetUserByID 根据用户ID获取用户信息（带Redis缓存）
// 缓存策略：先查Redis，未命中则查数据库并写入缓存
func GetUserByID(db *gorm.DB, userID int64) (*SysUser, error) {
	// 1. 尝试从Redis缓存获取
	cacheKey := GetUserCacheKey(userID)
	cachedData, err := redis.Get(cacheKey)

	if err == nil && cachedData != "" {
		// 缓存命中，反序列化
		user, err := DeserializeUser(cachedData)
		if err == nil {
			return user, nil
		}
		// 反序列化失败，记录日志后继续查数据库
	}

	// 2. 缓存未命中或出错，从数据库查询
	var user SysUser
	err = db.Where("id = ? AND deleted = ?", userID, 0).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}

	// 3. 将查询结果写入Redis缓存
	serializedUser, err := SerializeUser(&user)
	if err == nil {
		_ = redis.Set(cacheKey, serializedUser, UserCacheExpiration)
	}

	return &user, nil
}

// InvalidateUserCache 使指定用户的缓存失效
func InvalidateUserCache(userID int64) error {
	cacheKey := GetUserCacheKey(userID)
	return redis.Del(cacheKey)
}

// GetUserCacheKey 生成用户缓存键
func GetUserCacheKey(userID int64) string {
	return fmt.Sprintf("%s%d", UserCachePrefix, userID)
}

// SerializeUser 序列化用户对象为JSON字符串
func SerializeUser(user *SysUser) (string, error) {
	data, err := json.Marshal(user)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// DeserializeUser 反序列化JSON字符串为用户对象
func DeserializeUser(data string) (*SysUser, error) {
	var user SysUser
	err := json.Unmarshal([]byte(data), &user)
	if err != nil {
		return nil, err
	}
	return &user, nil
}

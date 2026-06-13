package model

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type SysUser struct {
	BaseModel
	Username    string     `json:"username" gorm:"column:username;not null"`
	Email       string     `json:"email" gorm:"column:email;unique;not null"`
	Phone       string     `json:"phone,omitempty" gorm:"column:phone"`
	Pwd         string     `json:"-" gorm:"column:pwd"`
	GoogleID    string     `json:"google_id,omitempty" gorm:"column:google_id"`
	XID         string     `json:"x_id,omitempty" gorm:"column:x_id"`
	GithubID    string     `json:"github_id,omitempty" gorm:"column:github_id"`
	MicrosoftID string     `json:"microsoft_id,omitempty" gorm:"column:microsoft_id"`
	AvatarURL   string     `json:"avatar_url,omitempty" gorm:"column:avatar_url"`
	Gender      int        `json:"gender" gorm:"column:gender;default:0"`
	Birthdate   *time.Time `json:"birthdate,omitempty" gorm:"column:birthdate"`
	Status      int        `json:"status" gorm:"column:status;default:1"`
	Role        int        `json:"role" gorm:"column:role;default:0"`
	Deleted     int        `json:"deleted" gorm:"column:deleted;default:0"`
}

func (SysUser) TableName() string {
	return "domains.users"
}

// GetUserByID 根据用户ID从数据库获取用户信息
func GetUserByID(db *gorm.DB, userID uuid.UUID) (*SysUser, error) {
	var user SysUser
	err := db.Where("id = ? AND deleted = ?", userID, 0).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetUserByEmail 根据邮箱从数据库获取用户信息
func GetUserByEmail(db *gorm.DB, email string) (*SysUser, error) {
	var user SysUser
	err := db.Where("email = ? AND deleted = ?", email, 0).First(&user).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}

// GetUsersByIDs 根据用户ID列表批量获取用户信息
func GetUsersByIDs(db *gorm.DB, userIDs []uuid.UUID) (map[uuid.UUID]*SysUser, error) {
	if len(userIDs) == 0 {
		return make(map[uuid.UUID]*SysUser), nil
	}

	var users []SysUser
	err := db.Where("id IN ? AND deleted = ?", userIDs, 0).Find(&users).Error
	if err != nil {
		return nil, err
	}

	// 将用户切片转换为以ID为key的map，方便快速查找
	userMap := make(map[uuid.UUID]*SysUser, len(users))
	for i := range users {
		userMap[users[i].ID] = &users[i]
	}

	return userMap, nil
}

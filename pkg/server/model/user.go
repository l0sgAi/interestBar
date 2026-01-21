package model

import (
	"errors"
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

// GetUserByID 根据用户ID从数据库获取用户信息
func GetUserByID(db *gorm.DB, userID int64) (*SysUser, error) {
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

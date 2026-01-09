package model

import (
	"time"

	"gorm.io/gorm"
)

// CircleMember 圈子成员关系与权限表
type CircleMember struct {
	ID           int64      `json:"id" gorm:"primarykey;column:id"`
	CircleID     int64      `json:"circle_id" gorm:"column:circle_id;not null"`                 // 圈子ID
	UserID       int64      `json:"user_id" gorm:"column:user_id;not null"`                     // 用户ID
	Role         int16      `json:"role" gorm:"column:role;type:smallint;default:10"`           // 角色
	Status       int16      `json:"status" gorm:"column:status;type:smallint;default:1"`       // 成员状态
	MuteEndTime  *time.Time `json:"mute_end_time,omitempty" gorm:"column:mute_end_time"`        // 禁言结束时间
	IsTop        int16      `json:"is_top" gorm:"column:is_top;type:smallint;default:0"`        // 是否置顶显示
	IsDisturb    int16      `json:"is_disturb" gorm:"column:is_disturb;type:smallint;default:0"` // 消息免打扰
	CreateTime   time.Time  `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime   time.Time  `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名
func (CircleMember) TableName() string {
	return "circle_member"
}

// CircleMemberRole 角色常量
const (
	MemberRoleMember    = 10 // 普通成员
	MemberRoleAdmin     = 20 // 管理员
	MemberRoleOwner     = 30 // 圈主
)

// CircleMemberStatus 成员状态常量
const (
	MemberStatusPending  = 0 // 待审核(申请中)
	MemberStatusNormal   = 1 // 正常
	MemberStatusMuted    = 2 // 禁言
	MemberStatusBanned   = 3 // 拉黑/踢出
)

// GetMember 获取成员信息
func GetMember(db *gorm.DB, circleID, userID int64) (*CircleMember, error) {
	var member CircleMember
	err := db.Where("circle_id = ? AND user_id = ?", circleID, userID).First(&member).Error
	if err != nil {
		return nil, err
	}
	return &member, nil
}

// GetCirclesByUserID 获取用户加入的圈子列表
func GetCirclesByUserID(db *gorm.DB, userID int64) ([]CircleMember, error) {
	var members []CircleMember
	err := db.Where("user_id = ? AND status = ?", userID, MemberStatusNormal).
		Order("create_time DESC").
		Find(&members).Error
	return members, err
}

// GetMembersByCircleID 获取圈子成员列表
func GetMembersByCircleID(db *gorm.DB, circleID int64, role int16, page, pageSize int) ([]CircleMember, int64, error) {
	var members []CircleMember
	var total int64

	query := db.Model(&CircleMember{}).Where("circle_id = ?", circleID)

	if role > 0 {
		query = query.Where("role = ?", role)
	}

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("role DESC, create_time DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&members).Error

	return members, total, err
}

// GetAdminsByCircleID 获取圈子管理员列表
func GetAdminsByCircleID(db *gorm.DB, circleID int64) ([]CircleMember, error) {
	var members []CircleMember
	err := db.Where("circle_id = ? AND role >= ? AND status = ?", circleID, MemberRoleAdmin, MemberStatusNormal).
		Order("role DESC").
		Find(&members).Error
	return members, err
}

// IsMember 检查用户是否是圈子成员
func IsMember(db *gorm.DB, circleID, userID int64) (bool, error) {
	var count int64
	err := db.Model(&CircleMember{}).
		Where("circle_id = ? AND user_id = ? AND status = ?", circleID, userID, MemberStatusNormal).
		Count(&count).Error
	return count > 0, err
}

// IsAdmin 检查用户是否是圈子管理员或圈主
func IsAdmin(db *gorm.DB, circleID, userID int64) (bool, error) {
	var member CircleMember
	err := db.Where("circle_id = ? AND user_id = ? AND status = ?", circleID, userID, MemberStatusNormal).
		First(&member).Error
	if err != nil {
		return false, err
	}
	return member.Role >= MemberRoleAdmin, nil
}

// IsOwner 检查用户是否是圈主
func IsOwner(db *gorm.DB, circleID, userID int64) (bool, error) {
	var member CircleMember
	err := db.Where("circle_id = ? AND user_id = ? AND status = ?", circleID, userID, MemberStatusNormal).
		First(&member).Error
	if err != nil {
		return false, err
	}
	return member.Role == MemberRoleOwner, nil
}

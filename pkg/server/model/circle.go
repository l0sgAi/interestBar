package model

import (
	"time"

	"gorm.io/gorm"
)

// Circle 兴趣圈/社区表
type Circle struct {
	ID          int64     `json:"id" gorm:"primarykey;column:id"`
	Name        string    `json:"name" gorm:"column:name;type:varchar(50);not null"`              // 兴趣圈名称
	Slug        string    `json:"slug,omitempty" gorm:"column:slug;type:varchar(60)"`              // 唯一标识符(用于URL SEO)
	AvatarURL   string    `json:"avatar_url,omitempty" gorm:"column:avatar_url;type:varchar(500)"` // 兴趣圈头像
	CoverURL    string    `json:"cover_url,omitempty" gorm:"column:cover_url;type:varchar(500)"`  // 背景图URL
	Description string    `json:"description" gorm:"column:description;type:varchar(2000);not null"` // 描述信息
	CreatorID   int64     `json:"creator_id" gorm:"column:creator_id;not null"`                    // 创建人ID
	CategoryID  int       `json:"category_id" gorm:"column:category_id;default:0"`                 // 分类ID
	Hot         int       `json:"hot" gorm:"column:hot;default:0"`                                 // 热度值
	MemberCount int       `json:"member_count" gorm:"column:member_count;default:0"`               // 成员数量
	PostCount   int       `json:"post_count" gorm:"column:post_count;default:0"`                   // 帖子数量
	JoinType    int16     `json:"join_type" gorm:"column:join_type;type:smallint;default:0"`       // 加入方式
	Status      int16     `json:"status" gorm:"column:status;type:smallint;default:1"`             // 状态
	Deleted     int16     `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`           // 逻辑删除
	CreateTime  time.Time `json:"create_time" gorm:"column:create_time;autoCreateTime"`
	UpdateTime  time.Time `json:"update_time" gorm:"column:update_time;autoUpdateTime"`
}

// TableName 指定表名
func (Circle) TableName() string {
	return "circle"
}

// CircleJoinType 加入方式常量
const (
	CircleJoinTypeDirect   = 0 // 直接加入
	CircleJoinTypeApproval = 1 // 需审核
	CircleJoinTypePrivate  = 2 // 私密(邀请制)
)

// CircleStatus 圈子状态常量
const (
	CircleStatusPending   = 0 // 审核中
	CircleStatusNormal    = 1 // 正常
	CircleStatusBanned    = 2 // 被封禁/冻结
)

// GetCircleByID 根据ID获取圈子信息
func GetCircleByID(db *gorm.DB, circleID int64) (*Circle, error) {
	var circle Circle
	err := db.Where("id = ? AND deleted = ?", circleID, 0).First(&circle).Error
	if err != nil {
		return nil, err
	}
	return &circle, nil
}

// GetCircleBySlug 根据Slug获取圈子信息
func GetCircleBySlug(db *gorm.DB, slug string) (*Circle, error) {
	var circle Circle
	err := db.Where("slug = ? AND deleted = ?", slug, 0).First(&circle).Error
	if err != nil {
		return nil, err
	}
	return &circle, nil
}

// GetCirclesByCategory 根据分类ID获取圈子列表
func GetCirclesByCategory(db *gorm.DB, categoryID int, page, pageSize int) ([]Circle, int64, error) {
	var circles []Circle
	var total int64

	query := db.Model(&Circle{}).Where("category_id = ? AND status = ? AND deleted = ?", categoryID, CircleStatusNormal, 0)

	// 获取总数
	query.Count(&total)

	// 分页查询
	err := query.Order("hot DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&circles).Error

	return circles, total, err
}

// GetCirclesByCreator 根据创建人ID获取圈子列表
func GetCirclesByCreator(db *gorm.DB, creatorID int64) ([]Circle, error) {
	var circles []Circle
	err := db.Where("creator_id = ? AND deleted = ?", creatorID, 0).
		Order("create_time DESC").
		Find(&circles).Error
	return circles, err
}

package model

import (
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// Circle 兴趣圈/社区表
type Circle struct {
	BaseModel
	Name        string     `json:"name" gorm:"column:name;type:varchar(50);not null"`                 // 兴趣圈名称
	Slug        string     `json:"slug,omitempty" gorm:"column:slug;type:varchar(60)"`                // 唯一标识符(用于URL SEO)
	AvatarURL   string     `json:"avatar_url,omitempty" gorm:"column:avatar_url;type:varchar(500)"`   // 兴趣圈头像
	CoverURL    string     `json:"cover_url,omitempty" gorm:"column:cover_url;type:varchar(500)"`     // 背景图URL
	Description string     `json:"description" gorm:"column:description;type:varchar(2000);not null"` // 描述信息
	Rule        string     `json:"rule,omitempty" gorm:"column:rule;type:text"`                       // 圈子规则/公告
	CreatorID   uuid.UUID  `json:"creator_id" gorm:"column:creator_id;type:uuid;not null"`            // 创建人ID
	CategoryID  *uuid.UUID `json:"category_id,omitempty" gorm:"column:category_id;type:uuid"`         // 分类ID，NULL表示未分类
	Hot         int        `json:"hot" gorm:"column:hot;default:0"`                                   // 热度值
	MemberCount int        `json:"member_count" gorm:"column:member_count;default:0"`                 // 成员数量
	PostCount   int        `json:"post_count" gorm:"column:post_count;default:0"`                     // 帖子数量
	JoinType    int16      `json:"join_type" gorm:"column:join_type;type:smallint;default:0"`         // 加入方式
	Status      int16      `json:"status" gorm:"column:status;type:smallint;default:1"`               // 状态
	Deleted     int16      `json:"deleted" gorm:"column:deleted;type:smallint;default:0"`             // 逻辑删除
}

// TableName 指定表名
func (Circle) TableName() string {
	return "domains.circle"
}

// CircleJoinType 加入方式常量
const (
	CircleJoinTypeDirect   = 0 // 直接加入
	CircleJoinTypeApproval = 1 // 需审核
	CircleJoinTypePrivate  = 2 // 私密(邀请制)
)

// CircleStatus 圈子状态常量
const (
	CircleStatusPending = 0 // 审核中
	CircleStatusNormal  = 1 // 正常
	CircleStatusBanned  = 2 // 被封禁/冻结
)

// GetCircleByID 根据ID获取圈子信息
func GetCircleByID(db *gorm.DB, circleID uuid.UUID) (*Circle, error) {
	var circle Circle
	err := db.Where("id = ? AND deleted = ?", circleID, 0).First(&circle).Error
	if err != nil {
		return nil, err
	}
	return &circle, nil
}

// GetCirclesByIDs 根据圈子ID列表批量获取圈子信息
func GetCirclesByIDs(db *gorm.DB, circleIDs []uuid.UUID) (map[uuid.UUID]*Circle, error) {
	if len(circleIDs) == 0 {
		return make(map[uuid.UUID]*Circle), nil
	}

	var circles []Circle
	err := db.Where("id IN ? AND deleted = ?", circleIDs, 0).Find(&circles).Error
	if err != nil {
		return nil, err
	}

	// 将圈子切片转换为以ID为key的map，方便快速查找
	circleMap := make(map[uuid.UUID]*Circle, len(circles))
	for i := range circles {
		circleMap[circles[i].ID] = &circles[i]
	}

	return circleMap, nil
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
func GetCirclesByCategory(db *gorm.DB, categoryID uuid.UUID, page, pageSize int) ([]Circle, int64, error) {
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
func GetCirclesByCreator(db *gorm.DB, creatorID uuid.UUID) ([]Circle, error) {
	var circles []Circle
	err := db.Where("creator_id = ? AND deleted = ?", creatorID, 0).
		Order("create_time DESC").
		Find(&circles).Error
	return circles, err
}

// CreateCircle 创建圈子并自动将创建者设为圈主（使用事务）
func CreateCircle(db *gorm.DB, circle *Circle) error {
	// 使用事务确保数据一致性
	return db.Transaction(func(tx *gorm.DB) error {
		// 1. 插入 circle 表
		if err := tx.Create(circle).Error; err != nil {
			return err
		}

		// 2. 同步插入 circle_member 表，赋予创建者圈主权限
		member := CircleMember{
			CircleID:  circle.ID,
			UserID:    circle.CreatorID,
			Role:      MemberRoleOwner,    // 30=圈主
			Status:    MemberStatusNormal, // 1=正常
			IsTop:     0,
			IsDisturb: 0,
		}

		if err := tx.Create(&member).Error; err != nil {
			return err
		}

		return nil
	})
}

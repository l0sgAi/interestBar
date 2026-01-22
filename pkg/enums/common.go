package enums

// Status 通用状态枚举
type Status int8

const (
	StatusDisabled Status = 0 // 禁用/隐藏
	StatusEnabled  Status = 1 // 启用/显示
)

// Deleted 逻辑删除枚举
type Deleted int8

const (
	DeletedNormal Deleted = 0 // 正常/未删除
	DeletedYes   Deleted = 1 // 已删除
)

// Bool 布尔枚举 (用于数据库 SMALLINT 类型)
type Bool int8

const (
	BoolNo  Bool = 0 // 否
	BoolYes Bool = 1 // 是
)

// String 返回状态的字符串表示
func (s Status) String() string {
	switch s {
	case StatusDisabled:
		return "禁用"
	case StatusEnabled:
		return "启用"
	default:
		return "未知"
	}
}

// String 返回删除状态的字符串表示
func (d Deleted) String() string {
	switch d {
	case DeletedNormal:
		return "正常"
	case DeletedYes:
		return "已删除"
	default:
		return "未知"
	}
}

// String 返回布尔值的字符串表示
func (b Bool) String() string {
	switch b {
	case BoolNo:
		return "否"
	case BoolYes:
		return "是"
	default:
		return "未知"
	}
}

// IsValid 检查状态是否有效
func (s Status) IsValid() bool {
	return s == StatusDisabled || s == StatusEnabled
}

// IsValid 检查删除状态是否有效
func (d Deleted) IsValid() bool {
	return d == DeletedNormal || d == DeletedYes
}

// IsValid 检查布尔值是否有效
func (b Bool) IsValid() bool {
	return b == BoolNo || b == BoolYes
}

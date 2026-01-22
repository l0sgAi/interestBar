package enums

// CategoryStatus 分类状态枚举
type CategoryStatus int8

const (
	CategoryStatusHidden CategoryStatus = 0 // 隐藏
	CategoryStatusShown  CategoryStatus = 1 // 显示
)

// String 返回分类状态的字符串表示
func (s CategoryStatus) String() string {
	switch s {
	case CategoryStatusHidden:
		return "隐藏"
	case CategoryStatusShown:
		return "显示"
	default:
		return "未知"
	}
}

// IsValid 检查分类状态是否有效
func (s CategoryStatus) IsValid() bool {
	return s == CategoryStatusHidden || s == CategoryStatusShown
}

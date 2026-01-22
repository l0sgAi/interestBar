package enums

// CommentStatus 评论状态枚举
type CommentStatus int8

const (
	CommentStatusNormal    CommentStatus = 1 // 正常
	CommentStatusReviewing CommentStatus = 2 // 审核中
	CommentStatusHidden    CommentStatus = 3 // 审核不通过/折叠
)

// String 返回评论状态的字符串表示
func (s CommentStatus) String() string {
	switch s {
	case CommentStatusNormal:
		return "正常"
	case CommentStatusReviewing:
		return "审核中"
	case CommentStatusHidden:
		return "已隐藏"
	default:
		return "未知"
	}
}

// IsValid 检查评论状态是否有效
func (s CommentStatus) IsValid() bool {
	return s >= CommentStatusNormal && s <= CommentStatusHidden
}

// IsVisible 检查评论是否可见
func (s CommentStatus) IsVisible() bool {
	return s == CommentStatusNormal
}

// NeedReview 检查评论是否需要审核
func (s CommentStatus) NeedReview() bool {
	return s == CommentStatusReviewing
}

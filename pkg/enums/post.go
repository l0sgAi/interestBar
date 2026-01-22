package enums

// PostType 帖子类型枚举
type PostType int8

const (
	PostTypeTextImage PostType = 1 // 图文
	PostTypeVideo     PostType = 2 // 纯视频
	PostTypeVoteLink  PostType = 3 // 投票/链接
)

// PostStatus 帖子状态枚举
type PostStatus int8

const (
	PostStatusDraft      PostStatus = 0 // 草稿
	PostStatusPublished  PostStatus = 1 // 发布(正常)
	PostStatusReviewing  PostStatus = 2 // 审核中
	PostStatusRejected   PostStatus = 3 // 审核失败
	PostStatusBlocked    PostStatus = 4 // 被屏蔽(软删/违规)
)

// String 返回帖子类型的字符串表示
func (t PostType) String() string {
	switch t {
	case PostTypeTextImage:
		return "图文"
	case PostTypeVideo:
		return "视频"
	case PostTypeVoteLink:
		return "投票"
	default:
		return "未知"
	}
}

// String 返回帖子状态的字符串表示
func (s PostStatus) String() string {
	switch s {
	case PostStatusDraft:
		return "草稿"
	case PostStatusPublished:
		return "已发布"
	case PostStatusReviewing:
		return "审核中"
	case PostStatusRejected:
		return "审核失败"
	case PostStatusBlocked:
		return "已屏蔽"
	default:
		return "未知"
	}
}

// IsValid 检查帖子类型是否有效
func (t PostType) IsValid() bool {
	return t >= PostTypeTextImage && t <= PostTypeVoteLink
}

// IsValid 检查帖子状态是否有效
func (s PostStatus) IsValid() bool {
	return s >= PostStatusDraft && s <= PostStatusBlocked
}

// IsPublished 检查帖子是否已发布
func (s PostStatus) IsPublished() bool {
	return s == PostStatusPublished
}

// IsDraft 检查帖子是否为草稿
func (s PostStatus) IsDraft() bool {
	return s == PostStatusDraft
}

// NeedReview 检查帖子是否需要审核
func (s PostStatus) NeedReview() bool {
	return s == PostStatusReviewing
}

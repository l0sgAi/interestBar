package enums

// CircleJoinType 兴趣圈加入方式枚举
type CircleJoinType int8

const (
	CircleJoinTypePublic  CircleJoinType = 0 // 公开(直接加入)
	CircleJoinTypeReview  CircleJoinType = 1 // 审核(需审核)
	CircleJoinTypePrivate CircleJoinType = 2 // 私密(邀请制)
)

// CircleStatus 兴趣圈状态枚举
type CircleStatus int8

const (
	CircleStatusPending  CircleStatus = 0 // 待审(审核中)
	CircleStatusNormal   CircleStatus = 1 // 正常
	CircleStatusBanned   CircleStatus = 2 // 封禁
)

// CircleMemberRole 圈子成员角色枚举
type CircleMemberRole int8

const (
	CircleMemberRoleMember   CircleMemberRole = 10 // 普通成员
	CircleMemberRoleAdmin    CircleMemberRole = 20 // 管理员
	CircleMemberRoleOwner    CircleMemberRole = 30 // 圈主
)

// CircleMemberStatus 圈子成员状态枚举
type CircleMemberStatus int8

const (
	CircleMemberStatusPending   CircleMemberStatus = 0 // 待审核(申请中)
	CircleMemberStatusNormal    CircleMemberStatus = 1 // 正常
	CircleMemberStatusMuted     CircleMemberStatus = 2 // 禁言
	CircleMemberStatusBanned    CircleMemberStatus = 3 // 拉黑/踢出
)

// String 返回加入方式的字符串表示
func (j CircleJoinType) String() string {
	switch j {
	case CircleJoinTypePublic:
		return "公开"
	case CircleJoinTypeReview:
		return "审核"
	case CircleJoinTypePrivate:
		return "私密"
	default:
		return "未知"
	}
}

// String 返回圈子状态的字符串表示
func (s CircleStatus) String() string {
	switch s {
	case CircleStatusPending:
		return "待审"
	case CircleStatusNormal:
		return "正常"
	case CircleStatusBanned:
		return "封禁"
	default:
		return "未知"
	}
}

// String 返回成员角色的字符串表示
func (r CircleMemberRole) String() string {
	switch r {
	case CircleMemberRoleMember:
		return "普通成员"
	case CircleMemberRoleAdmin:
		return "管理员"
	case CircleMemberRoleOwner:
		return "圈主"
	default:
		return "未知"
	}
}

// String 返回成员状态的字符串表示
func (s CircleMemberStatus) String() string {
	switch s {
	case CircleMemberStatusPending:
		return "待审核"
	case CircleMemberStatusNormal:
		return "正常"
	case CircleMemberStatusMuted:
		return "禁言"
	case CircleMemberStatusBanned:
		return "拉黑"
	default:
		return "未知"
	}
}

// IsValid 检查加入方式是否有效
func (j CircleJoinType) IsValid() bool {
	return j >= CircleJoinTypePublic && j <= CircleJoinTypePrivate
}

// IsValid 检查圈子状态是否有效
func (s CircleStatus) IsValid() bool {
	return s >= CircleStatusPending && s <= CircleStatusBanned
}

// IsValid 检查成员角色是否有效
func (r CircleMemberRole) IsValid() bool {
	return r == CircleMemberRoleMember || r == CircleMemberRoleAdmin || r == CircleMemberRoleOwner
}

// IsValid 检查成员状态是否有效
func (s CircleMemberStatus) IsValid() bool {
	return s >= CircleMemberStatusPending && s <= CircleMemberStatusBanned
}

// IsAdmin 检查是否为管理员或圈主
func (r CircleMemberRole) IsAdmin() bool {
	return r == CircleMemberRoleAdmin || r == CircleMemberRoleOwner
}

// IsOwner 检查是否为圈主
func (r CircleMemberRole) IsOwner() bool {
	return r == CircleMemberRoleOwner
}

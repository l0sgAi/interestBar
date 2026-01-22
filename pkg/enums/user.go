package enums

// UserStatus 用户状态枚举
type UserStatus int8

const (
	UserStatusDisabled UserStatus = 0 // 禁用
	UserStatusEnabled  UserStatus = 1 // 启用
)

// UserRole 用户角色枚举
type UserRole int8

const (
	UserRoleUser      UserRole = 0 // 用户
	UserRoleAdmin     UserRole = 1 // 管理员
)

// UserGender 用户性别枚举
type UserGender int8

const (
	UserGenderUnknown UserGender = 0 // 未知
	UserGenderMale    UserGender = 1 // 男
	UserGenderFemale  UserGender = 2 // 女
)

// String 返回用户状态的字符串表示
func (s UserStatus) String() string {
	switch s {
	case UserStatusDisabled:
		return "禁用"
	case UserStatusEnabled:
		return "启用"
	default:
		return "未知"
	}
}

// String 返回用户角色的字符串表示
func (r UserRole) String() string {
	switch r {
	case UserRoleUser:
		return "用户"
	case UserRoleAdmin:
		return "管理员"
	default:
		return "未知"
	}
}

// String 返回用户性别的字符串表示
func (g UserGender) String() string {
	switch g {
	case UserGenderUnknown:
		return "未知"
	case UserGenderMale:
		return "男"
	case UserGenderFemale:
		return "女"
	default:
		return "未知"
	}
}

// IsValid 检查用户状态是否有效
func (s UserStatus) IsValid() bool {
	return s == UserStatusDisabled || s == UserStatusEnabled
}

// IsValid 检查用户角色是否有效
func (r UserRole) IsValid() bool {
	return r == UserRoleUser || r == UserRoleAdmin
}

// IsValid 检查用户性别是否有效
func (g UserGender) IsValid() bool {
	return g >= UserGenderUnknown && g <= UserGenderFemale
}

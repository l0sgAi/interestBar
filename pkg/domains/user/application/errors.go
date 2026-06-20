package application

import "errors"

// 领域层定义的"可识别错误"。HTTP handler 层据此映射到对应的响应码/消息。
var (
	errAtLeastOneField = errors.New("at least one field must be provided")
	errUsernameEmpty   = errors.New("username cannot be empty")
	errGenderRange     = errors.New("gender must be 0 (unknown), 1 (male), 2 (female) or 3 (others)")
	errBirthdateFuture = errors.New("birthdate cannot be in the future")
)

// IsAtLeastOneFieldErr 判断是否为"至少修改一个字段"错误（供 handler 层判断）。
func IsAtLeastOneFieldErr(err error) bool { return errors.Is(err, errAtLeastOneField) }

// IsUsernameEmptyErr 判断是否为"用户名不能为空"错误。
func IsUsernameEmptyErr(err error) bool { return errors.Is(err, errUsernameEmpty) }

// IsGenderRangeErr 判断是否为"性别值非法"错误。
func IsGenderRangeErr(err error) bool { return errors.Is(err, errGenderRange) }

// IsBirthdateFutureErr 判断是否为"生日不能在未来"错误。
func IsBirthdateFutureErr(err error) bool { return errors.Is(err, errBirthdateFuture) }

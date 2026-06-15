package application

import (
	"errors"
	"strings"
)

// 领域层可识别错误。
var (
	errInvalidJoinType    = errors.New("join_type must be 0 (direct), 1 (approval), or 2 (private)")
	errCircleNameExists   = errors.New("circle name already exists")
	errCircleSlugExists   = errors.New("circle slug already exists")
	errCircleNotAvailable = errors.New("this circle is not available for joining")

	// join/leave 流程的错误（与旧 controller 错误信息对应）
	errAlreadyMember    = errors.New("already a member of this circle")
	errBannedFromCircle = errors.New("user is banned from this circle")
	errPrivateCircle    = errors.New("this circle is private and requires invitation")
	errOwnerCannotLeave = errors.New("circle owner cannot leave the circle")
	errNotMember        = errors.New("not a member of this circle")
)

// 错误判断函数（供 handler 层使用）。
func IsInvalidJoinTypeErr(err error) bool   { return errors.Is(err, errInvalidJoinType) }
func IsCircleNameExistsErr(err error) bool  { return errors.Is(err, errCircleNameExists) }
func IsCircleSlugExistsErr(err error) bool  { return errors.Is(err, errCircleSlugExists) }
func IsCircleNotAvailableErr(err error) bool { return errors.Is(err, errCircleNotAvailable) }
func IsAlreadyMemberErr(err error) bool     { return errors.Is(err, errAlreadyMember) }
func IsBannedFromCircleErr(err error) bool  { return errors.Is(err, errBannedFromCircle) }
func IsPrivateCircleErr(err error) bool     { return errors.Is(err, errPrivateCircle) }
func IsOwnerCannotLeaveErr(err error) bool  { return errors.Is(err, errOwnerCannotLeave) }
func IsNotMemberErr(err error) bool         { return errors.Is(err, errNotMember) }

// mapJoinLeaveError 把 memberRepo 返回的错误（字符串匹配）映射为可识别错误。
//
// 旧 model.JoinCircle/LeaveCircle 用 fmt.Errorf 返回字符串错误，
// 这里通过字符串匹配转换。未来 repository 改用 sentinel error 后可简化。
func mapJoinLeaveError(err error) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "user is already a member"):
		return errAlreadyMember
	case strings.Contains(msg, "user is banned"):
		return errBannedFromCircle
	case strings.Contains(msg, "private and requires invitation"):
		return errPrivateCircle
	case strings.Contains(msg, "circle owner cannot leave"):
		return errOwnerCannotLeave
	case strings.Contains(msg, "not a member"):
		return errNotMember
	}
	return err
}

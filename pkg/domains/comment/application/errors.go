package application

import (
	"errors"
)

// errNotRootComment 不是根评论（GetReplies 校验用）。
//
// 这是 service 层独立定义的哨兵错误（domain 层没有对应的语义错误，
// 因为 GetReplies 的入参校验属于应用层职责）。
var errNotRootComment = errors.New("not a root comment")

// IsNotRootCommentErr 判断是否为"非根评论"错误。
func IsNotRootCommentErr(err error) bool { return errors.Is(err, errNotRootComment) }

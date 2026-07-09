package application

import "errors"

// errSection 请求的 section 暂未实现。
var errSection = errors.New("discover section not supported")

// IsSectionErr 判断是否为不支持的 section 错误（handler 映射 400）。
func IsSectionErr(err error) bool { return errors.Is(err, errSection) }

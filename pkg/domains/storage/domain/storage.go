// Package domain 存放 storage 领域的纯领域模型：DTO、常量、
// 以及定义的 ObjectStorage 接口（由 infrastructure 层实现）。
//
// 依赖规则：本包不得 import 任何 gin/hertz/S3 SDK 等基础设施或框架库，
// 也不得 import 其他领域包。
package domain

import (
	"context"
	"mime/multipart"
)

// ObjectStorage 是对象存储的抽象接口（由 infrastructure 实现）。
//
// 抽象 *multipart.FileHeader 让接口保持"无框架依赖"：
// 任何 Web 框架只要能产出标准库的 multipart.FileHeader 即可对接。
type ObjectStorage interface {
	// Upload 上传单个文件，返回可访问的 URL。
	Upload(ctx context.Context, key string, file *multipart.FileHeader, acl string) (string, error)
	// Delete 删除一个文件。
	Delete(ctx context.Context, key string) error
	// PresignedURL 生成预签名 URL（私有文件临时访问）。
	PresignedURL(ctx context.Context, key string) (string, error)
	// Available 返回底层 client 是否已初始化（缺失时返回 false）。
	Available() bool
}

// FileKey 生成策略：以用户 ID 为路径前缀 + UUID 后缀避免覆盖。
// 这里把它抽象成方法签名而非直接调用 S3 包，便于 mock。
//
// ValidateFile 校验文件类型与大小（infrastructure 层注入实际规则）。
// 保留为独立函数而非方法，便于在 application service 复用。

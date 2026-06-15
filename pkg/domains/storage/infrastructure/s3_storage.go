// Package infrastructure 提供 storage 领域基础设施层实现：基于 S3。
//
// 它实现 domain.ObjectStorage 接口，复用项目既有的
// pkg/server/storage/s3 包（全局单例 + helper 函数），保持过渡期行为零变化。
package infrastructure

import (
	"context"
	"mime/multipart"

	"interestBar/pkg/domains/storage/domain"
	s3storage "interestBar/pkg/server/storage/s3"
)

// s3Storage 基于 pkg/server/storage/s3 包的 ObjectStorage 实现。
type s3Storage struct{}

// NewObjectStorage 构造一个基于全局 S3 单例的 ObjectStorage。
//
// 注意：这里故意不注入 *s3.Client，而是复用 s3storage.GetS3Client() 全局访问，
// 与旧 upload controller 行为一致。后续重构可以把 client 持有在 Deps。
func NewObjectStorage() domain.ObjectStorage {
	return &s3Storage{}
}

// Available 返回 S3 client 是否已初始化。
func (s *s3Storage) Available() bool {
	return s3storage.GetS3Client() != nil
}

// Upload 调用 S3 client 上传文件。
func (s *s3Storage) Upload(ctx context.Context, key string, file *multipart.FileHeader, acl string) (string, error) {
	return s3storage.GetS3Client().UploadFile(ctx, key, file, acl)
}

// Delete 删除文件。
func (s *s3Storage) Delete(ctx context.Context, key string) error {
	return s3storage.GetS3Client().DeleteFile(ctx, key)
}

// PresignedURL 生成预签名 URL。
func (s *s3Storage) PresignedURL(ctx context.Context, key string) (string, error) {
	return s3storage.GetS3Client().GetPresignedURL(ctx, key)
}

// ValidateFile 是对 s3storage.ValidateFile 的直通包装。
// 放在 infrastructure 层是为了让 application service 依赖本包而非 s3 SDK 包。
//
// 参数语义与 s3storage.ValidateFile 一致：
//   - allowedExts：允许的扩展名（带点），如 []string{".jpg", ".png"}
//   - maxSize：最大字节数
func ValidateFile(file *multipart.FileHeader, allowedExts []string, maxSize int64) error {
	return s3storage.ValidateFile(file, allowedExts, maxSize)
}

// GenerateKey 生成 S3 对象键（调用 s3storage.GenerateKeyWithUUID）。
func GenerateKey(basePath, filename string) string {
	return s3storage.GenerateKeyWithUUID(basePath, filename)
}

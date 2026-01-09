package controller

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"interestBar/pkg/server/response"
	s3storage "interestBar/pkg/server/storage/s3"
)

// UploadController 文件上传控制器
type UploadController struct {
	logger *zap.Logger
}

// NewUploadController 创建上传控制器
func NewUploadController(logger *zap.Logger) *UploadController {
	return &UploadController{
		logger: logger,
	}
}

// UploadImage 上传图片
// @Summary 上传图片
// @Description 上传图片到 S3，支持 jpg, png, gif, webp 等格式
// @Tags 文件上传
// @Accept multipart/form-data
// @Param file formData file true "图片文件"
// @Success 200 {object} response.Response
// @Router /api/v1/upload/image [post]
func (uc *UploadController) UploadImage(c *gin.Context) {
	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		uc.logger.Error("failed to get uploaded file", zap.Error(err))
		response.BadRequest(c, "Failed to get uploaded file")
		return
	}

	// 验证文件类型
	allowedExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp", ".svg"}
	err = s3storage.ValidateFile(file, allowedExts, 10*1024*1024) // 10MB
	if err != nil {
		uc.logger.Error("file validation failed", zap.Error(err))
		response.BadRequest(c, fmt.Sprintf("File validation failed: %v", err))
		return
	}

	// 生成 S3 对象键名
	key := s3storage.GenerateKeyWithUUID("images", file.Filename)

	// 上传到 S3
	s3Client := s3storage.GetS3Client()
	if s3Client == nil {
		uc.logger.Error("S3 client is not initialized")
		response.InternalError(c, "S3 service is not available")
		return
	}

	fileURL, err := s3Client.UploadFile(c.Request.Context(), key, file, "public-read")
	if err != nil {
		uc.logger.Error("failed to upload file to S3", zap.Error(err))
		response.InternalError(c, "Failed to upload file")
		return
	}

	// 返回成功响应
	response.Success(c, gin.H{
		"url":      fileURL,
		"key":      key,
		"filename": file.Filename,
		"size":     file.Size,
	})
}

// UploadVideo 上传视频
// @Summary 上传视频
// @Description 上传视频到 S3，支持 mp4, avi, mov 等格式
// @Tags 文件上传
// @Accept multipart/form-data
// @Param file formData file true "视频文件"
// @Success 200 {object} response.Response
// @Router /api/v1/upload/video [post]
func (uc *UploadController) UploadVideo(c *gin.Context) {
	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		uc.logger.Error("failed to get uploaded file", zap.Error(err))
		response.BadRequest(c, "Failed to get uploaded file")
		return
	}

	// 验证文件类型
	allowedExts := []string{".mp4", ".avi", ".mov", ".mkv", ".webm"}
	err = s3storage.ValidateFile(file, allowedExts, 500*1024*1024) // 500MB
	if err != nil {
		uc.logger.Error("file validation failed", zap.Error(err))
		response.BadRequest(c, fmt.Sprintf("File validation failed: %v", err))
		return
	}

	// 生成 S3 对象键名
	key := s3storage.GenerateKeyWithUUID("videos", file.Filename)

	// 上传到 S3
	s3Client := s3storage.GetS3Client()
	if s3Client == nil {
		uc.logger.Error("S3 client is not initialized")
		response.InternalError(c, "S3 service is not available")
		return
	}

	fileURL, err := s3Client.UploadFile(c.Request.Context(), key, file, "public-read")
	if err != nil {
		uc.logger.Error("failed to upload file to S3", zap.Error(err))
		response.InternalError(c, "Failed to upload file")
		return
	}

	// 返回成功响应
	response.Success(c, gin.H{
		"url":      fileURL,
		"key":      key,
		"filename": file.Filename,
		"size":     file.Size,
	})
}

// UploadAvatar 上传头像
// @Summary 上传头像
// @Description 上传用户头像到 S3
// @Tags 文件上传
// @Accept multipart/form-data
// @Param file formData file true "头像文件"
// @Success 200 {object} response.Response
// @Router /api/v1/upload/avatar [post]
func (uc *UploadController) UploadAvatar(c *gin.Context) {
	// 获取用户 ID（假设从认证中间件获取）
	userID := c.GetString("user_id")
	if userID == "" {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	// 获取上传的文件
	file, err := c.FormFile("file")
	if err != nil {
		uc.logger.Error("failed to get uploaded file", zap.Error(err))
		response.BadRequest(c, "Failed to get uploaded file")
		return
	}

	// 验证文件类型
	allowedExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}
	err = s3storage.ValidateFile(file, allowedExts, 5*1024*1024) // 5MB
	if err != nil {
		uc.logger.Error("file validation failed", zap.Error(err))
		response.BadRequest(c, fmt.Sprintf("File validation failed: %v", err))
		return
	}

	// 生成 S3 对象键名（使用用户 ID 作为路径）
	ext := strings.ToLower(filepath.Ext(file.Filename))
	key := fmt.Sprintf("avatars/%s/%s%s", userID, userID, ext)

	// 上传到 S3
	s3Client := s3storage.GetS3Client()
	if s3Client == nil {
		uc.logger.Error("S3 client is not initialized")
		response.InternalError(c, "S3 service is not available")
		return
	}

	fileURL, err := s3Client.UploadFile(c.Request.Context(), key, file, "public-read")
	if err != nil {
		uc.logger.Error("failed to upload file to S3", zap.Error(err))
		response.InternalError(c, "Failed to upload file")
		return
	}

	// 返回成功响应
	response.Success(c, gin.H{
		"url":      fileURL,
		"key":      key,
		"filename": file.Filename,
		"size":     file.Size,
	})
}

// UploadPostImages 上传帖子图片（支持多图上传）
// @Summary 上传帖子图片
// @Description 批量上传帖子图片到 S3
// @Tags 文件上传
// @Accept multipart/form-data
// @Param files formData file true "图片文件（可多个）"
// @Success 200 {object} response.Response
// @Router /api/v1/upload/post-images [post]
func (uc *UploadController) UploadPostImages(c *gin.Context) {
	// 获取用户 ID
	userID := c.GetString("user_id")
	if userID == "" {
		response.Unauthorized(c, "User not authenticated")
		return
	}

	// 获取上传的文件（支持多文件）
	form, err := c.MultipartForm()
	if err != nil {
		uc.logger.Error("failed to get multipart form", zap.Error(err))
		response.BadRequest(c, "Failed to get uploaded files")
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		response.BadRequest(c, "No files uploaded")
		return
	}

	// 限制最多上传 9 张图片
	if len(files) > 9 {
		response.BadRequest(c, "Maximum 9 images allowed")
		return
	}

	s3Client := s3storage.GetS3Client()
	if s3Client == nil {
		uc.logger.Error("S3 client is not initialized")
		response.InternalError(c, "S3 service is not available")
		return
	}

	// 上传所有文件
	var uploadResults []gin.H
	allowedExts := []string{".jpg", ".jpeg", ".png", ".gif", ".webp"}

	for _, file := range files {
		// 验证文件
		err = s3storage.ValidateFile(file, allowedExts, 10*1024*1024) // 10MB
		if err != nil {
			uc.logger.Error("file validation failed",
				zap.String("filename", file.Filename),
				zap.Error(err),
			)
			continue
		}

		// 生成 S3 对象键名
		key := s3storage.GenerateKeyWithUUID(fmt.Sprintf("posts/%s", userID), file.Filename)

		// 上传到 S3
		fileURL, err := s3Client.UploadFile(c.Request.Context(), key, file, "public-read")
		if err != nil {
			uc.logger.Error("failed to upload file to S3",
				zap.String("filename", file.Filename),
				zap.Error(err),
			)
			continue
		}

		uploadResults = append(uploadResults, gin.H{
			"url":      fileURL,
			"key":      key,
			"filename": file.Filename,
			"size":     file.Size,
		})
	}

	if len(uploadResults) == 0 {
		response.InternalError(c, "Failed to upload any files")
		return
	}

	// 返回成功响应
	response.SuccessWithMessage(c, fmt.Sprintf("Successfully uploaded %d/%d files", len(uploadResults), len(files)), gin.H{
		"uploaded": len(uploadResults),
		"total":    len(files),
		"images":   uploadResults,
	})
}

// DeleteFile 删除文件
// @Summary 删除文件
// @Description 从 S3 删除文件
// @Tags 文件上传
// @Param key query string true "文件 S3 Key"
// @Success 200 {object} response.Response
// @Router /api/v1/upload/delete [delete]
func (uc *UploadController) DeleteFile(c *gin.Context) {
	// 获取文件 Key
	key := c.Query("key")
	if key == "" {
		response.BadRequest(c, "File key is required")
		return
	}

	// 获取 S3 客户端
	s3Client := s3storage.GetS3Client()
	if s3Client == nil {
		uc.logger.Error("S3 client is not initialized")
		response.InternalError(c, "S3 service is not available")
		return
	}

	// 删除文件
	err := s3Client.DeleteFile(c.Request.Context(), key)
	if err != nil {
		uc.logger.Error("failed to delete file from S3",
			zap.String("key", key),
			zap.Error(err),
		)
		response.InternalError(c, "Failed to delete file")
		return
	}

	response.SuccessWithMessage(c, "File deleted successfully", nil)
}

// GetPresignedURL 获取预签名 URL
// @Summary 获取预签名 URL
// @Description 获取文件的预签名 URL，用于临时访问私有文件
// @Tags 文件上传
// @Param key query string true "文件 S3 Key"
// @Success 200 {object} response.Response
// @Router /api/v1/upload/presign [get]
func (uc *UploadController) GetPresignedURL(c *gin.Context) {
	// 获取文件 Key
	key := c.Query("key")
	if key == "" {
		response.BadRequest(c, "File key is required")
		return
	}

	// 获取 S3 客户端
	s3Client := s3storage.GetS3Client()
	if s3Client == nil {
		uc.logger.Error("S3 client is not initialized")
		response.InternalError(c, "S3 service is not available")
		return
	}

	// 生成预签名 URL
	url, err := s3Client.GetPresignedURL(c.Request.Context(), key)
	if err != nil {
		uc.logger.Error("failed to generate presigned URL",
			zap.String("key", key),
			zap.Error(err),
		)
		response.InternalError(c, "Failed to generate presigned URL")
		return
	}

	response.Success(c, gin.H{
		"url": url,
		"key": key,
	})
}

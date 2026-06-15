// Package http 提供 storage 领域的 HTTP 入站适配器（handler + 路由注册）。
//
// handler 只做：解析请求 → 调用 application.Service → 用 httputil 写响应。
// 通过 appctx.AppContext 与具体框架解耦，迁移 hertz 时本文件不动。
package http

import (
	"strconv"

	"interestBar/pkg/domains/storage/application"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/httputil"
)

// Handler 处理 storage 相关的 HTTP 请求。
type Handler struct {
	svc application.StorageService
}

// NewHandler 构造一个 storage Handler。
func NewHandler(svc application.StorageService) *Handler {
	return &Handler{svc: svc}
}

// UploadImage POST /upload/image —— 上传单张图片，需登录。
func (h *Handler) UploadImage(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	file, err := c.FormFile("file")
	if err != nil {
		httputil.BadRequest(c, "Failed to get uploaded file")
		return
	}

	vo, err := h.svc.UploadImage(c, userID, file)
	if err != nil {
		httputil.InternalError(c, err.Error())
		return
	}
	httputil.Success(c, vo)
}

// UploadPostImages POST /upload/post-images —— 批量上传帖子图片，需登录。
func (h *Handler) UploadPostImages(c appctx.AppContext) {
	userID, ok := requireUserID(c)
	if !ok {
		return
	}

	form, err := c.MultipartForm()
	if err != nil {
		httputil.BadRequest(c, "Failed to get uploaded files")
		return
	}

	files := form.File["files"]
	vo, err := h.svc.UploadPostImages(c, userID, files)
	if err != nil {
		httputil.BadRequest(c, err.Error())
		return
	}
	httputil.SuccessWithMessage(c,
		"Successfully uploaded "+strconv.Itoa(vo.Uploaded)+"/"+strconv.Itoa(vo.Total)+" files", vo)
}

// UploadVideo POST /upload/video —— 上传视频，需登录。
func (h *Handler) UploadVideo(c appctx.AppContext) {
	file, err := c.FormFile("file")
	if err != nil {
		httputil.BadRequest(c, "Failed to get uploaded file")
		return
	}

	vo, err := h.svc.UploadVideo(c, file)
	if err != nil {
		httputil.InternalError(c, err.Error())
		return
	}
	httputil.Success(c, vo)
}

// DeleteFile DELETE /upload/delete —— 按 key 删除文件。
func (h *Handler) DeleteFile(c appctx.AppContext) {
	key := c.Query("key")
	if key == "" {
		httputil.BadRequest(c, "File key is required")
		return
	}

	if err := h.svc.DeleteFile(c, key); err != nil {
		httputil.InternalError(c, err.Error())
		return
	}
	httputil.SuccessWithMessage(c, "File deleted successfully", nil)
}

// PresignedURL GET /upload/presign —— 生成预签名 URL。
func (h *Handler) PresignedURL(c appctx.AppContext) {
	key := c.Query("key")
	if key == "" {
		httputil.BadRequest(c, "File key is required")
		return
	}

	vo, err := h.svc.PresignedURL(c, key)
	if err != nil {
		httputil.InternalError(c, err.Error())
		return
	}
	httputil.Success(c, vo)
}

// requireUserID 从 AppContext 取出已登录的 loginID（UUIDv7 字符串）。
// RequireLogin 中间件已填充 SetLoginID，这里直接读。
func requireUserID(c appctx.AppContext) (string, bool) {
	loginID, ok := c.LoginID()
	if !ok || loginID == "" {
		httputil.Unauthorized(c, "Token not found")
		return "", false
	}
	return loginID, true
}

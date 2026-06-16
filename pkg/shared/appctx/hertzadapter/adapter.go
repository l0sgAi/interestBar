// Package hertzadapter 提供 AppContext 接口的 hertz 实现。
//
// 它把 *app.RequestContext 包装成 appctx.AppContext，让业务层只依赖 AppContext，
// 不直接 import hertz。这是 gin→hertz 迁移后的唯一 AppContext 实现。
//
// 注意 hertz 的若干 API 与 gin 不同：
//   - Method/Path/GetHeader 返回 []byte，这里统一转成 string；
//   - RequestContext 是值类型，用指针持有；
//   - 业务上下文用 c.Set/c.Get（hertz 支持，跨 handler 存活）。
package hertzadapter

import (
	"context"
	"mime/multipart"

	"interestBar/pkg/shared/appctx"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
)

// hertzAppContext 把 *app.RequestContext 适配为 appctx.AppContext。
type hertzAppContext struct {
	c *app.RequestContext
	// 内嵌 context：用请求的 ctx 作为标准库 context.Context 的来源
	context.Context
}

// New 把 *app.RequestContext 包装为 AppContext。
//
// ctx 是 hertz handler 签名里的 context.Context（请求级，含 trace/超时）。
func New(ctx context.Context, c *app.RequestContext) appctx.AppContext {
	return &hertzAppContext{
		c:       c,
		Context: ctx,
	}
}

// ---- 请求信息 ----

func (h *hertzAppContext) Method() string        { return string(h.c.Method()) }
func (h *hertzAppContext) Path() string          { return string(h.c.Request.URI().Path()) }
func (h *hertzAppContext) Param(name string) string { return h.c.Param(name) }
func (h *hertzAppContext) Query(name string) string { return string(h.c.Query(name)) }
func (h *hertzAppContext) Header(name string) string { return string(h.c.GetHeader(name)) }
func (h *hertzAppContext) PostForm(name string) string { return string(h.c.PostForm(name)) }

// FormFile 读取 multipart 上传的单个文件，映射到 RequestContext.FormFile。
func (h *hertzAppContext) FormFile(name string) (*multipart.FileHeader, error) {
	return h.c.FormFile(name)
}

// MultipartForm 读取整个 multipart 表单，映射到 RequestContext.MultipartForm。
func (h *hertzAppContext) MultipartForm() (*multipart.Form, error) {
	return h.c.MultipartForm()
}

// ---- 请求体绑定 ----

func (h *hertzAppContext) BindJSON(v any) error {
	return h.c.BindJSON(v)
}

// BindQuery 用 hertz 的 Bind 绑定 query（依赖 `form` tag）。
// hertz 的 Bind 会同时尝试 query/form/path，对纯 query 绑定行为兼容 gin ShouldBindQuery。
func (h *hertzAppContext) BindQuery(v any) error {
	return h.c.Bind(v)
}

// ---- 响应 ----

func (h *hertzAppContext) JSON(code int, v any) {
	h.c.JSON(code, v)
}

// Redirect 发送 HTTP 重定向。hertz 的 Redirect 第二参是 []byte。
func (h *hertzAppContext) Redirect(code int, url string) {
	h.c.Redirect(code, []byte(url))
}

func (h *hertzAppContext) SetHeader(key, value string) {
	h.c.Header(key, value)
}

// Abort 终止后续中间件执行，映射到 RequestContext.Abort()。
func (h *hertzAppContext) Abort() {
	h.c.Abort()
}

// ---- 业务上下文 ----

func (h *hertzAppContext) UserID() (uuid.UUID, bool) {
	if v, ok := h.c.Get(userIDKey); ok {
		if id, ok := v.(uuid.UUID); ok && id != uuid.Nil {
			return id, true
		}
	}
	return uuid.Nil, false
}

func (h *hertzAppContext) SetUserID(id uuid.UUID) {
	h.c.Set(userIDKey, id)
}

func (h *hertzAppContext) LoginID() (string, bool) {
	if v, ok := h.c.Get(loginIDKey); ok {
		if id, ok := v.(string); ok && id != "" {
			return id, true
		}
	}
	return "", false
}

func (h *hertzAppContext) SetLoginID(id string) {
	h.c.Set(loginIDKey, id)
}

func (h *hertzAppContext) Device() string {
	if v, ok := h.c.Get(deviceKey); ok {
		if d, ok := v.(string); ok {
			return d
		}
	}
	return ""
}

func (h *hertzAppContext) SetDevice(device string) {
	h.c.Set(deviceKey, device)
}

// hertz RequestContext 里存取业务值的 key。
// 用字符串常量，与 hertz 的 Keys map[string]interface{} 对齐。
const (
	userIDKey  = "appctx:user_id"
	loginIDKey = "appctx:login_id"
	deviceKey  = "appctx:device"
)

// compile-time guard: 确保 hertzAppContext 实现了 AppContext 接口。
var _ appctx.AppContext = (*hertzAppContext)(nil)

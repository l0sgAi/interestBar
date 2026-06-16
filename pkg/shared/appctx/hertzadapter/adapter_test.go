// Package hertzadapter 的单元测试。
//
// 重点回归 hertz 迁移后 AppContext.BindQuery 的行为：
//   - 只读 query string，不碰请求体（修复前用 c.Bind(v) 会被 JSON body 污染）
//   - 只认 `query` tag（不认 `form` tag）
//
// 测试不启动完整 hertz server，直接构造 *app.RequestContext。
package hertzadapter

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/hertz/pkg/app"
)

// newTestContext 构造一个用于测试的 AppContext，可选附带 query string 和 body。
func newTestContext(queryString string, body string, contentType string) (context.Context, *app.RequestContext) {
	c := &app.RequestContext{}
	if queryString != "" {
		c.Request.SetQueryString(queryString)
	}
	if body != "" {
		c.Request.SetBody([]byte(body))
		c.Request.Header.Set("Content-Type", contentType)
	}
	// SetQueryString 不会改 Method，BindQuery 不依赖 Method，这里不设置。
	return context.Background(), c
}

// ===== BindQuery =====

// queryOnly 用于测试的 struct，字段打 `query` tag。
type queryOnly struct {
	Keyword string `query:"keyword"`
	Size    int    `query:"size"`
}

// TestBindQuery_ReadsQueryString 验证 BindQuery 能从 query string 正确读取 `query` tag 字段。
func TestBindQuery_ReadsQueryString(t *testing.T) {
	ctx, c := newTestContext("keyword=hello&size=10", "", "")
	a := New(ctx, c)

	var req queryOnly
	if err := a.BindQuery(&req); err != nil {
		t.Fatalf("BindQuery failed: %v", err)
	}
	if req.Keyword != "hello" {
		t.Fatalf("Keyword = %q, want %q", req.Keyword, "hello")
	}
	if req.Size != 10 {
		t.Fatalf("Size = %d, want 10", req.Size)
	}
}

// TestBindQuery_NotPollutedByBody 核心回归断言：GET 带 JSON body 时，
// body 不得覆盖 query 字段。（修复前用 c.Bind(v)，body 会 unmarshal 覆盖 query。）
func TestBindQuery_NotPollutedByBody(t *testing.T) {
	// query 里 keyword=from_query；body 里故意写 keyword=from_body
	ctx, c := newTestContext("keyword=from_query", `{"keyword":"from_body"}`, "application/json")
	a := New(ctx, c)

	var req queryOnly
	if err := a.BindQuery(&req); err != nil {
		t.Fatalf("BindQuery failed: %v", err)
	}
	if req.Keyword != "from_query" {
		t.Fatalf("Keyword = %q, want %q (body 污染了 query)", req.Keyword, "from_query")
	}
}

// TestBindQuery_IgnoresFormTag 验证 BindQuery 只认 `query` tag，不认 `form` tag。
// （hertz BindQuery 的语义：只读 query tag 字段。form tag 字段应保持零值。）
type mixedTag struct {
	WithQuery string `query:"keyword"`
	WithForm  string `form:"keyword"`
}

func TestBindQuery_IgnoresFormTag(t *testing.T) {
	ctx, c := newTestContext("keyword=hello", "", "")
	a := New(ctx, c)

	var req mixedTag
	if err := a.BindQuery(&req); err != nil {
		t.Fatalf("BindQuery failed: %v", err)
	}
	if req.WithQuery != "hello" {
		t.Fatalf("WithQuery = %q, want %q", req.WithQuery, "hello")
	}
	if req.WithForm != "" {
		t.Fatalf("WithForm = %q, want empty (form tag should be ignored by BindQuery)", req.WithForm)
	}
}

// TestBindQuery_EmptyQueryString 空请求不应报错，字段保持零值。
func TestBindQuery_EmptyQueryString(t *testing.T) {
	ctx, c := newTestContext("", "", "")
	a := New(ctx, c)

	var req queryOnly
	if err := a.BindQuery(&req); err != nil {
		t.Fatalf("BindQuery on empty query failed: %v", err)
	}
	if req.Keyword != "" || req.Size != 0 {
		t.Fatalf("expected zero values, got Keyword=%q Size=%d", req.Keyword, req.Size)
	}
}

// ===== BindJSON =====

// jsonBody 用于测试 BindJSON。
type jsonBody struct {
	Name string `json:"name"`
	Age  int    `json:"age"`
}

// TestBindJSON_DecodesBody 验证 BindJSON 能正确解码 JSON body。
// 注意：hertz 的 BindJSON 不含 validator（不校验 `binding` tag），只做 JSON 反序列化。
func TestBindJSON_DecodesBody(t *testing.T) {
	ctx, c := newTestContext("", `{"name":"alice","age":30}`, "application/json")
	a := New(ctx, c)

	var req jsonBody
	if err := a.BindJSON(&req); err != nil {
		t.Fatalf("BindJSON failed: %v", err)
	}
	if req.Name != "alice" {
		t.Fatalf("Name = %q, want %q", req.Name, "alice")
	}
	if req.Age != 30 {
		t.Fatalf("Age = %d, want 30", req.Age)
	}
}

// TestBindJSON_EmptyBody 空请求体时 BindJSON 应返回 EOF 类错误（hertz decodeJSON 对空 body 返回 io.EOF）。
// 这是 hertz 与 gin 的行为差异点（gin ShouldBindJSON 对空 body 也返回 EOF），
// 记录此行为以防未来误改。
func TestBindJSON_EmptyBody(t *testing.T) {
	ctx, c := newTestContext("", "", "")
	a := New(ctx, c)

	var req jsonBody
	err := a.BindJSON(&req)
	if err == nil {
		t.Fatal("expected error on empty body, got nil")
	}
	// 不严格断言错误类型，只确认是错误（io.EOF 或包装）
	if !strings.Contains(strings.ToLower(err.Error()), "eof") && !strings.Contains(err.Error(), "EOF") {
		// hertz 可能返回别的错误信息，只要非 nil 即可接受
		t.Logf("BindJSON empty body error (acceptable): %v", err)
	}
}

// ===== 其他方法冒烟测试 =====

// TestRequestInfo 基础冒烟：Method/Path/Query/Header/Param 的包装是否正确转发。
func TestRequestInfo(t *testing.T) {
	c := &app.RequestContext{}
	c.Request.SetRequestURI("/foo/bar?k=v")
	c.Request.Header.SetMethod("GET")
	c.Request.Header.Set("X-Test", "hdr")

	ctx := context.Background()
	a := New(ctx, c)

	if got := a.Method(); got != "GET" {
		t.Fatalf("Method() = %q, want GET", got)
	}
	if got := a.Path(); got != "/foo/bar" {
		t.Fatalf("Path() = %q, want /foo/bar", got)
	}
	if got := a.Query("k"); got != "v" {
		t.Fatalf("Query(k) = %q, want v", got)
	}
	if got := a.Header("X-Test"); got != "hdr" {
		t.Fatalf("Header(X-Test) = %q, want hdr", got)
	}
}

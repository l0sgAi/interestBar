// Package http 的可管理圈子列表 handler 单元测试。
//
// 覆盖：未登录 401（不触达 service）、BindQuery 失败 400、
// 正常路径分页信封（Pagination：code/data/total/page/per_page）。
// 不启动 hertz server，直接构造 *app.RequestContext（对齐 hertzadapter 测试范式）。
package http

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"interestBar/pkg/domains/circle/application"
	"interestBar/pkg/shared/appctx"
	"interestBar/pkg/shared/appctx/hertzadapter"

	"github.com/cloudwego/hertz/pkg/app"
	"github.com/google/uuid"
)

// fakeSvcForManageList 仅实现 ListManagedCircles 的最小 CircleService fake。
type fakeSvcForManageList struct {
	application.CircleService

	called     bool
	gotKeyword string
	gotPage    int
	gotSize    int
	result     *application.ManagedCircleListResult
}

func (f *fakeSvcForManageList) ListManagedCircles(ctx context.Context, operatorID uuid.UUID, keyword string, page, size int) (*application.ManagedCircleListResult, error) {
	f.called = true
	f.gotKeyword, f.gotPage, f.gotSize = keyword, page, size
	if f.result == nil {
		f.result = &application.ManagedCircleListResult{
			Total: 1, Page: page, Size: size,
			Data: []application.ManagedCircleItem{{
				ID:   uuid.MustParse("0192a0d0-0000-7000-8000-0000000000aa"),
				Name: "管理圈", MyRole: 30, AgentCount: 0,
			}},
		}
	}
	return f.result, nil
}

// newManageListCtx 构造测试用 AppContext，登录态可选。
func newManageListCtx(queryString string, login bool) (appctx.AppContext, *app.RequestContext) {
	rc := &app.RequestContext{}
	if queryString != "" {
		rc.Request.SetQueryString(queryString)
	}
	ac := hertzadapter.New(context.Background(), rc)
	if login {
		ac.SetLoginID("0192a0d0-0000-7000-8000-000000000001")
	}
	return ac, rc
}

// TestListManagedCircles_Unauthenticated 未登录直接 401，service 不得被触达。
func TestListManagedCircles_Unauthenticated(t *testing.T) {
	svc := &fakeSvcForManageList{}
	h := NewHandler(svc)

	ac, rc := newManageListCtx("page=1&size=20", false)
	h.ListManagedCircles(ac)

	if rc.Response.StatusCode() != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rc.Response.StatusCode())
	}
	if svc.called {
		t.Fatal("service must not be called when unauthenticated")
	}
}

// TestListManagedCircles_BindFailure 非法 query（page=abc）→ 400，service 不得被触达。
func TestListManagedCircles_BindFailure(t *testing.T) {
	svc := &fakeSvcForManageList{}
	h := NewHandler(svc)

	ac, rc := newManageListCtx("page=abc", true)
	h.ListManagedCircles(ac)

	if rc.Response.StatusCode() != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rc.Response.StatusCode())
	}
	if svc.called {
		t.Fatal("service must not be called on bind failure")
	}
}

// TestListManagedCircles_PaginationEnvelope 正常路径：Pagination 信封
// {code,data,total,page,per_page}，参数原样透传 service（规整在 service 层做）。
func TestListManagedCircles_PaginationEnvelope(t *testing.T) {
	svc := &fakeSvcForManageList{}
	h := NewHandler(svc)

	ac, rc := newManageListCtx("keyword=%E5%9C%88&page=2&size=5", true)
	h.ListManagedCircles(ac)

	if rc.Response.StatusCode() != http.StatusOK {
		t.Fatalf("status = %d, want 200", rc.Response.StatusCode())
	}
	if !svc.called {
		t.Fatal("service was not called")
	}
	if svc.gotKeyword != "圈" {
		t.Fatalf("keyword = %q, want %q", svc.gotKeyword, "圈")
	}
	if svc.gotPage != 2 || svc.gotSize != 5 {
		t.Fatalf("page=%d size=%d, want 2/5（raw 透传，规整在 service）", svc.gotPage, svc.gotSize)
	}

	var body struct {
		Code    int              `json:"code"`
		Data    []map[string]any `json:"data"`
		Total   int64            `json:"total"`
		Page    int              `json:"page"`
		PerPage int              `json:"per_page"`
	}
	if err := json.Unmarshal(rc.Response.Body(), &body); err != nil {
		t.Fatalf("unmarshal response: %v\nbody: %s", err, rc.Response.Body())
	}
	if body.Code != 200 {
		t.Fatalf("business code = %d, want 200", body.Code)
	}
	if body.Total != 1 || body.Page != 2 || body.PerPage != 5 {
		t.Fatalf("envelope total=%d page=%d per_page=%d, want 1/2/5", body.Total, body.Page, body.PerPage)
	}
	if len(body.Data) != 1 || body.Data[0]["my_role"].(float64) != 30 {
		t.Fatalf("unexpected data: %+v", body.Data)
	}
}

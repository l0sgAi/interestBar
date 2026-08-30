// Package infrastructure 的成员列表游标单元测试。
//
// 核心回归：decodeMemberCursor 对用户可控的游标参数必须防御性解析，
// 任何字段缺失/类型错误/坏 UUID 都返回包装 domain.ErrInvalidCursor 的错误，绝不 panic。
package infrastructure

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"interestBar/pkg/domains/circle/domain"

	"github.com/google/uuid"
)

// rawCursor 直接把任意 map/字符串编码为 base64 游标（绕过 encodeMemberCursor，便于模拟篡改）。
func rawCursor(t *testing.T, v interface{}) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal raw cursor: %v", err)
	}
	return base64.StdEncoding.EncodeToString(b)
}

// TestMemberCursor_RoundTrip 正常游标 encode → decode 字段一致（微秒精度不丢失）。
func TestMemberCursor_RoundTrip(t *testing.T) {
	now := time.Now().Truncate(time.Microsecond) // timestamptz 微秒精度
	m := &domain.CircleMember{
		ID:         uuid.MustParse("0192a0d0-0000-7000-8000-000000000001"),
		Role:       domain.MemberRoleAdmin,
		CreateTime: now,
	}

	c, err := decodeMemberCursor(encodeMemberCursor(m))
	if err != nil {
		t.Fatalf("decodeMemberCursor failed: %v", err)
	}
	if c.Role != m.Role {
		t.Fatalf("role mismatch: got %d, want %d", c.Role, m.Role)
	}
	if !c.Time.Equal(now) {
		t.Fatalf("time mismatch: got %v, want %v", c.Time, now)
	}
	if c.ID != m.ID {
		t.Fatalf("id mismatch: got %s, want %s", c.ID, m.ID)
	}
}

// TestMemberCursor_DecodeTampered 篡改游标：坏 base64/非 JSON/缺字段/坏 UUID/类型错
// 均返回 ErrInvalidCursor 包装错误，不 panic。
func TestMemberCursor_DecodeTampered(t *testing.T) {
	id := "0192a0d0-0000-7000-8000-000000000001"

	cases := []struct {
		name   string
		cursor string
	}{
		{"not base64", "!!!not-base64!!!"},
		{"not json", base64.StdEncoding.EncodeToString([]byte("garbage"))},
		{"missing role", rawCursor(t, map[string]interface{}{"t": float64(1), "i": id})},
		{"missing time", rawCursor(t, map[string]interface{}{"r": float64(20), "i": id})},
		{"missing id", rawCursor(t, map[string]interface{}{"r": float64(20), "t": float64(1)})},
		{"empty id", rawCursor(t, map[string]interface{}{"r": float64(20), "t": float64(1), "i": ""})},
		{"bad uuid", rawCursor(t, map[string]interface{}{"r": float64(20), "t": float64(1), "i": "not-a-uuid"})},
		{"role wrong type", rawCursor(t, map[string]interface{}{"r": "twenty", "t": float64(1), "i": id})},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := decodeMemberCursor(tc.cursor)
			if err == nil {
				t.Fatalf("expected error, got cursor %+v", c)
			}
			if !errors.Is(err, domain.ErrInvalidCursor) {
				t.Fatalf("error must wrap domain.ErrInvalidCursor, got: %v", err)
			}
		})
	}
}

// TestMemberCursor_EmptyStringFallsBackToFirstPage 空游标由调用方跳过解析（ListMembers
// 仅在 cursor != "" 时解码）；这里验证 decodeMemberCursor 自身对空串报错，
// 防止未来有人去掉调用方的空串短路。
func TestMemberCursor_EmptyStringFallsBackToFirstPage(t *testing.T) {
	if _, err := decodeMemberCursor(""); !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got: %v", err)
	}
}

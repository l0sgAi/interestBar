// Package infrastructure 的游标解析单元测试。
//
// 核心回归：parseCursorValues 对用户可控的游标参数必须防御性解析，
// 任何字段缺失/类型错误都返回 error（包装 ErrInvalidCursor），绝不 panic。
// （修复前 applyCursorCondition 用 values["like_count"].(float64) 无 comma-ok，
//
//	攻击者构造缺 like_count 的游标即可触发远程 panic。）
package infrastructure

import (
	"errors"
	"testing"

	"interestBar/pkg/domains/comment/domain"

	"github.com/google/uuid"
)

// makeCursor 直接构造 base64 游标（绕过 buildNextCursor，便于模拟篡改）。
func makeCursor(t *testing.T, m map[string]interface{}) string {
	t.Helper()
	enc := encodeCursor(m)
	// 双向自校验：确保 makeCursor 产出的字符串 decodeCursor 能读回
	if _, err := decodeCursor(enc); err != nil {
		t.Fatalf("makeCursor produced undecodable cursor: %v", err)
	}
	return enc
}

// ===== 正常 round-trip =====

// TestCursor_RoundTrip_SortByLike 正常游标（按点赞）能 encode → parse 回来。
func TestCursor_RoundTrip_SortByLike(t *testing.T) {
	id := uuid.MustParse("0192a0d0-0000-7000-8000-000000000001")
	cursor := buildNextCursor(&domain.Comment{ID: id, LikeCount: 42}, 0)

	likeCount, parsedID, err := parseCursorValues(cursor, 0)
	if err != nil {
		t.Fatalf("parseCursorValues failed: %v", err)
	}
	if likeCount != 42 {
		t.Fatalf("likeCount = %d, want 42", likeCount)
	}
	if parsedID != id {
		t.Fatalf("id = %v, want %v", parsedID, id)
	}
}

// TestCursor_RoundTrip_SortByTime 正常游标（按时间）能 encode → parse 回来。
func TestCursor_RoundTrip_SortByTime(t *testing.T) {
	id := uuid.MustParse("0192a0d0-0000-7000-8000-000000000002")
	cursor := buildNextCursor(&domain.Comment{ID: id}, 1)

	_, parsedID, err := parseCursorValues(cursor, 1)
	if err != nil {
		t.Fatalf("parseCursorValues failed: %v", err)
	}
	if parsedID != id {
		t.Fatalf("id = %v, want %v", parsedID, id)
	}
}

// ===== 防御性解析（核心回归）=====

// TestParseCursor_MissingLikeCount_NoPanic 核心回归：sort==0 缺 like_count 必须返回 error 不 panic。
// （修复前：values["like_count"].(float64) 在字段缺失时 panic）
func TestParseCursor_MissingLikeCount_NoPanic(t *testing.T) {
	cursor := makeCursor(t, map[string]interface{}{
		// 故意缺 like_count
		"id": "0192a0d0-0000-7000-8000-000000000001",
	})

	_, _, err := parseCursorValues(cursor, 0)
	if err == nil {
		t.Fatal("expected error for missing like_count, got nil")
	}
	if !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("error should wrap ErrInvalidCursor, got: %v", err)
	}
}

// TestParseCursor_WrongLikeCountType 错误的 like_count 类型（字符串）必须返回 error 不 panic。
func TestParseCursor_WrongLikeCountType(t *testing.T) {
	cursor := makeCursor(t, map[string]interface{}{
		"like_count": "not-a-number", // 故意用字符串
		"id":         "0192a0d0-0000-7000-8000-000000000001",
	})

	_, _, err := parseCursorValues(cursor, 0)
	if err == nil {
		t.Fatal("expected error for wrong like_count type, got nil")
	}
	if !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("error should wrap ErrInvalidCursor, got: %v", err)
	}
}

// TestParseCursor_MissingID 缺 id 必须返回 error。
func TestParseCursor_MissingID(t *testing.T) {
	cursor := makeCursor(t, map[string]interface{}{
		"like_count": float64(42),
		// 故意缺 id
	})

	for _, sort := range []int{0, 1} {
		_, _, err := parseCursorValues(cursor, sort)
		if err == nil {
			t.Fatalf("sort=%d: expected error for missing id, got nil", sort)
		}
		if !errors.Is(err, domain.ErrInvalidCursor) {
			t.Fatalf("sort=%d: error should wrap ErrInvalidCursor, got: %v", sort, err)
		}
	}
}

// TestParseCursor_InvalidID id 不是合法 UUID 必须返回 error。
func TestParseCursor_InvalidID(t *testing.T) {
	cursor := makeCursor(t, map[string]interface{}{
		"id": "not-a-uuid",
	})

	_, _, err := parseCursorValues(cursor, 1)
	if err == nil {
		t.Fatal("expected error for invalid id, got nil")
	}
	if !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("error should wrap ErrInvalidCursor, got: %v", err)
	}
}

// TestParseCursor_MalformedBase64 非 base64 / 非 JSON 必须返回 error 不 panic。
func TestParseCursor_MalformedBase64(t *testing.T) {
	badCursors := []string{
		"!!!not-base64!!!",
		"bm90LWpzb24=", // "not-json" 的 base64
		"",             // 空字符串在 applyCursorCondition 已短路，但 parseCursorValues 本身会尝试 decode
	}
	for _, c := range badCursors {
		_, _, err := parseCursorValues(c, 0)
		// 空字符串 decodeCursor 返回空 map，缺 id → ErrInvalidCursor；其余也返回 error
		if err == nil {
			t.Fatalf("cursor %q: expected error, got nil", c)
		}
	}
}

// ===== 空游标 =====

// TestParseCursor_EmptyStringForSortByTime 空字符串游标：decodeCursor 返回空 map，
// parseCursorValues 因缺 id 返回 ErrInvalidCursor（注意：applyCursorCondition 层对空串短路，
// 但 parseCursorValues 是纯解析函数，会走 decode，这里验证它的行为一致性）。
func TestParseCursor_EmptyStringForSortByTime(t *testing.T) {
	_, _, err := parseCursorValues("", 1)
	if err == nil {
		t.Fatal("expected error for empty cursor, got nil")
	}
	if !errors.Is(err, domain.ErrInvalidCursor) {
		t.Fatalf("error should wrap ErrInvalidCursor, got: %v", err)
	}
}

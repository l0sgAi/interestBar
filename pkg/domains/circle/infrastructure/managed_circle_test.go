// Package infrastructure 的可管理圈子列表单元测试。
//
// 核心回归：ILIKE 关键词的通配符转义——用户输入的 % _ \ 不得扩大匹配范围
// 或破坏子串语义（配合 ESCAPE '\' 使用）。
package infrastructure

import "testing"

// TestEscapeLike 通配符转义：\ % _ 各自翻倍/前缀转义，普通文本（含 CJK、空格）原样保留。
func TestEscapeLike(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"percent", `100%`, `100\%`},
		{"underscore", `foo_bar`, `foo\_bar`},
		{"backslash", `a\b`, `a\\b`},
		{"mixed wildcards", `%_\%`, `\%\_\\\%`},
		{"cjk unchanged", "圈子·描述", "圈子·描述"},
		{"cjk with wildcards", `圈_子%`, `圈\_子\%`},
		{"space unchanged", "go lang", "go lang"},
		{"only backslashes", `\\\`, `\\\\\\`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := escapeLike(tc.in); got != tc.want {
				t.Fatalf("escapeLike(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

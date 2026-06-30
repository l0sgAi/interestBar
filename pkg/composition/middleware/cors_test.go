// Package middleware 的 CORS 匹配逻辑单元测试。
//
// 核心回归：isOriginAllowed 的 ":*" 端口通配不得误匹配。
// （修复前：http://localhost:* 用 HasPrefix(prefix) 会放行 http://localhost.evil.com）
package middleware

import "testing"

func TestIsOriginAllowed(t *testing.T) {
	// 典型允许列表：精确 + 端口通配 + 路径前缀（不含 "*"，便于精确验证各模式）
	allowed := []string{
		"https://example.com",
		"http://localhost:*",
		"https://app.foo.com",
	}

	tests := []struct {
		name   string
		origin string
		want   bool
	}{
		// 精确匹配
		{"exact match", "https://example.com", true},
		{"exact no match (different host)", "https://example.org", false},
		{"exact match with path via path-prefix", "https://example.com/app", true}, // 路径前缀命中

		// 端口通配 http://localhost:* —— 核心回归点
		{"port wildcard matches legit port", "http://localhost:3000", true},
		{"port wildcard matches other port", "http://localhost:5173", true},

		// 🔴 修复前的 bug：以下应被拒绝，但旧实现会放行
		{"port wildcard NOT match subdomain evil", "http://localhost.evil.com", false},
		{"port wildcard NOT match subdomain with port", "http://localhost.evil.com:8080", false},

		// 路径前缀
		{"path prefix match", "https://app.foo.com/dashboard", true},
		// 🔴 路径前缀的同类边界问题：app.foo.com.evil.com 不应被 https://app.foo.com 放行
		{"path prefix NOT match sibling host", "https://app.foo.com.evil.com", false},

		// 无关 origin
		{"unrelated origin", "https://evil.com", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isOriginAllowed(tt.origin, allowed)
			if got != tt.want {
				t.Fatalf("isOriginAllowed(%q) = %v, want %v", tt.origin, got, tt.want)
			}
		})
	}
}

// TestIsOriginAllowed_WildcardStar 单独验证 "*" 通配放行一切。
func TestIsOriginAllowed_WildcardStar(t *testing.T) {
	allowed := []string{"*"}
	for _, origin := range []string{
		"https://anything.com",
		"http://localhost:3000",
		"http://localhost.evil.com",
	} {
		if !isOriginAllowed(origin, allowed) {
			t.Errorf("isOriginAllowed(%q) with [\"*\"] = false, want true", origin)
		}
	}
}

// TestIsOriginAllowed_PortWildcard_NoWildcardStar 单独验证：
// 允许列表不含 "*" 时，端口通配的误匹配修复仍生效。
func TestIsOriginAllowed_PortWildcard_NoWildcardStar(t *testing.T) {
	// 只有端口通配，没有 "*" 兜底
	allowed := []string{"http://localhost:*"}

	cases := map[string]bool{
		"http://localhost:3000":          true,  // 合法端口
		"http://localhost:5173":          true,  // 合法端口
		"http://localhost.evil.com":      false, // 🔴 旧 bug：会被放行
		"http://localhost.evil.com:8080": false, // 🔴 旧 bug：会被放行
		"https://example.com":            false, // 无关 origin
		"http://localhost":               false, // 无端口（prefix+":" 要求有冒号）
	}
	for origin, want := range cases {
		got := isOriginAllowed(origin, allowed)
		if got != want {
			t.Errorf("isOriginAllowed(%q) = %v, want %v", origin, got, want)
		}
	}
}

func TestIsAllDigits(t *testing.T) {
	cases := map[string]bool{
		"3000":  true,
		"0":     true,
		"":      false,
		"12a":   false,
		"evil":  false,
		"12 34": false,
		"-1":    false,
	}
	for s, want := range cases {
		got := isAllDigits(s)
		if got != want {
			t.Errorf("isAllDigits(%q) = %v, want %v", s, got, want)
		}
	}
}

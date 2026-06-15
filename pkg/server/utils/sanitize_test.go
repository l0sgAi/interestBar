package utils

import (
	"strings"
	"testing"
)

// TestSanitizeForPg_StripsNullByte 回归测试：对应生产事故
// "invalid byte sequence for encoding UTF8: 0x00" (SQLSTATE 22021)。
// 粘贴 README 等富文本时混入 NUL 字节，必须在写入 PostgreSQL 前被剔除。
func TestSanitizeForPg_StripsNullByte(t *testing.T) {
	// 模拟事故场景：正常文本中间夹一个 NUL 字节（0x00）
	original := "InterestBar\x00一个兴趣社区"
	got := SanitizeForPg(original)

	if strings.ContainsRune(got, 0) {
		t.Fatalf("NUL byte not stripped: %q", got)
	}
	if got != "InterestBar一个兴趣社区" {
		t.Fatalf("unexpected result: %q", got)
	}
}

// TestSanitizeForPg_StripsInvalidUTF8 校验无效 UTF-8 字节序列也被剔除
// （PostgreSQL 同样以 SQLSTATE 22021 拒绝这类输入）。
func TestSanitizeForPg_StripsInvalidUTF8(t *testing.T) {
	// "\xff\xfe" 是无效的 UTF-8 字节序列
	original := "ok\xff\xfefine"
	got := SanitizeForPg(original)

	if got != "okfine" {
		t.Fatalf("invalid UTF-8 not stripped: %q", got)
	}
}

func TestSanitizeForPg_PreservesAllowedControls(t *testing.T) {
	// PostgreSQL 允许的制表符/换行符应保留（正文/Markdown 需要）
	original := "line1\n\tindented\r\nline2"
	got := SanitizeForPg(original)
	if got != original {
		t.Fatalf("allowed control chars altered: %q", got)
	}
}

func TestSanitizeForPg_Empty(t *testing.T) {
	if got := SanitizeForPg(""); got != "" {
		t.Fatalf("expected empty, got %q", got)
	}
}

func TestSanitizeForPg_MultipleNullBytes(t *testing.T) {
	original := "\x00a\x00b\x00"
	if got := SanitizeForPg(original); got != "ab" {
		t.Fatalf("multiple NUL not stripped: %q", got)
	}
}

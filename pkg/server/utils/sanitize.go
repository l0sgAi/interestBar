package utils

import "strings"

// SanitizeForPg 清洗字符串，使其可安全写入 PostgreSQL text/varchar 字段。
//
// PostgreSQL 不允许 text/varchar 列包含 NUL 字符（U+0000），也不接受任何
// 无效的 UTF-8 字节序列——二者都会在写入时报错：
//
//	invalid byte sequence for encoding UTF8: 0x00 (SQLSTATE 22021)
//
// 这些字节常来自富文本/跨平台粘贴、Markdown 解析残留或客户端编码错误。
//
// 处理策略：原地删除脏字符，而非拒绝整条数据——避免单个异常字节就让用户
// 发帖/发评论失败。保留 PostgreSQL 允许的制表符/换行符 \t \n \r（正文与
// Markdown 需要它们）。对结果不可见的字段（如已通过 JSON 绑定校验的
// extra_data/media_extra）无需调用本函数。
func SanitizeForPg(s string) string {
	if s == "" {
		return s
	}
	// 1. 剔除 NUL 字符（UTF-8 编码合法，但 PostgreSQL 拒绝）。
	//    用 "\x00" 转义字面量而非直接写入不可见字节，便于 git diff / 编辑器审视。
	s = strings.ReplaceAll(s, "\x00", "")
	// 2. 剔除其余无效 UTF-8 字节序列（PostgreSQL 同样以 SQLSTATE 22021 拒绝）。
	s = strings.ToValidUTF8(s, "")
	return s
}

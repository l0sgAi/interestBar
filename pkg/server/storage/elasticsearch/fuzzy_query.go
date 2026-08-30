package elasticsearch

import "strings"

// escapeWildcard 转义 ES wildcard 查询的元字符（* ? \），防用户输入注入通配符
// 造成词表全扫的慢查询。
func escapeWildcard(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `*`, `\*`, `?`, `\?`)
	return r.Replace(s)
}

// fuzzyShouldClauses 构建模糊关键词召回的 should 子句对（调用方包一层 bool should
// + minimum_should_match=1，可再追加其他召回路，如 email match）：
//  1. multi_match + fuzziness AUTO：分词匹配 + 拼写容错（编辑距离自适应：
//     1-2 字符词 0、3-5 字符词 1、更长 2），主字段权重 3，副字段权重 1；
//  2. wildcard 主字段 .keyword 子串包含：真·模糊匹配（"mie" 命中 "miemie"、
//     "君几" 命中 "盼君几多愁"），case_insensitive；用户输入 * ? \ 已转义防注入。
//
// 副字段（description/summary 等长文本）不挂 wildcard：.keyword 子字段受
// ignore_above 256 截断，长文本子串召回无意义且代价高，仅参与分词匹配。
//
// 排序不受影响（调用方自行 _score DESC + id DESC）；副字段权重语义与历史一致。
func fuzzyShouldClauses(keyword, primaryField string, secondaryFields ...string) []map[string]interface{} {
	fields := make([]string, 0, len(secondaryFields)+1)
	fields = append(fields, primaryField+"^3")
	for _, f := range secondaryFields {
		fields = append(fields, f+"^1")
	}
	return []map[string]interface{}{
		{
			"multi_match": map[string]interface{}{
				"query":     keyword,
				"fields":    fields,
				"type":      "best_fields",
				"operator":  "or",
				"fuzziness": "AUTO",
			},
		},
		{
			"wildcard": map[string]interface{}{
				primaryField + ".keyword": map[string]interface{}{
					"value":            "*" + escapeWildcard(keyword) + "*",
					"case_insensitive": true,
				},
			},
		},
	}
}

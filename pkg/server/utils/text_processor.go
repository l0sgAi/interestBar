package utils

import (
	"bytes"
	"strings"

	"github.com/gomarkdown/markdown"
	"github.com/gomarkdown/markdown/ast"
	"github.com/gomarkdown/markdown/parser"
)

// 创建解析器的别名函数，避免与 markdown 包名冲突
func createParser() *parser.Parser {
	extensions := parser.CommonExtensions | parser.AutoHeadingIDs | parser.BackslashLineBreak | parser.Strikethrough
	return parser.NewWithExtensions(extensions)
}

const (
	// SummaryMaxLength 摘要最大长度（字符数）
	SummaryMaxLength = 2000
)

// GenerateSummary 从 Markdown 内容生成纯文本摘要
// 逻辑：
// 1. 解析 Markdown 内容，去除所有格式标记
// 2. 提取纯文本内容
// 3. 清洗多余空白和换行
// 4. 截取前 2000 字符
func GenerateSummary(markdownContent string) string {
	if markdownContent == "" {
		return ""
	}

	// 1. 创建 Markdown 解析器
	p := createParser()

	// 2. 解析 Markdown 为 AST
	doc := markdown.Parse([]byte(markdownContent), p)

	// 3. 从 AST 中提取纯文本
	text := extractTextFromNode(doc)

	// 4. 清洗文本：去除多余空白和换行
	cleanedText := cleanWhitespace(text)

	// 5. 截取前 2000 字符（按 UTF-8 字符计算）
	summary := truncateCleanly(cleanedText, SummaryMaxLength)

	return summary
}

// extractTextFromNode 递归提取 AST 节点中的文本内容
func extractTextFromNode(node ast.Node) string {
	var buf bytes.Buffer

	// 遍历 AST 节点
	ast.WalkFunc(node, func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}

		switch n := node.(type) {
		case *ast.Text:
			// 普通文本
			buf.Write(n.Literal)
		case *ast.Paragraph:
			// 段落之间添加换行
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
		case *ast.Heading:
			// 标题前后添加换行
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
		case *ast.List:
			// 列表前后添加换行
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
		case *ast.ListItem:
			// 列表项
			buf.WriteString("• ")
		case *ast.CodeBlock:
			// 代码块前后添加换行
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
			// 保留代码内容，但标记为代码
			buf.WriteString("[代码] ")
			buf.Write(n.Literal)
			buf.WriteString(" [/代码]")
			buf.WriteString("\n\n")
		case *ast.Code:
			// 行内代码
			buf.WriteString("[代码]")
			buf.Write(n.Literal)
			buf.WriteString("[/代码]")
		case *ast.Link:
			// 链接：只显示文本，不显示 URL
			buf.Write(n.Title) // 使用链接标题作为显示文本
			if len(n.Children) > 0 {
				for _, child := range n.Children {
					if textNode, ok := child.(*ast.Text); ok {
						buf.Write(textNode.Literal)
					}
				}
			}
		case *ast.Image:
			// 图片：显示 alt 文本
			buf.WriteString("[图片: ")
			buf.Write(n.Title) // 使用图片标题
			if len(n.Children) > 0 {
				for _, child := range n.Children {
					if textNode, ok := child.(*ast.Text); ok {
						buf.Write(textNode.Literal)
					}
				}
			}
			buf.WriteString("]")
		case *ast.BlockQuote:
			// 引用块前后添加换行
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
		case *ast.HorizontalRule:
			// 分割线
			if buf.Len() > 0 {
				buf.WriteString("\n\n---\n\n")
			}
		case *ast.Document:
			// 文档节点，不做处理
		default:
			// 其他类型的节点，继续遍历子节点
		}

		return ast.GoToNext
	})

	return buf.String()
}

// cleanWhitespace 清洗文本中的多余空白和换行
func cleanWhitespace(text string) string {
	if text == "" {
		return ""
	}

	// 1. 按行分割
	lines := strings.Split(text, "\n")

	// 2. 处理每一行
	var cleanedLines []string
	emptyLineCount := 0

	for _, line := range lines {
		// 去除行首行尾空白
		trimmed := strings.TrimSpace(line)

		if trimmed == "" {
			// 空行计数
			emptyLineCount++
			// 最多保留 1 个空行（即连续空行合并为 1 个）
			if emptyLineCount <= 1 {
				cleanedLines = append(cleanedLines, "")
			}
		} else {
			// 非空行
			emptyLineCount = 0

			// 去除行内的多余空格（但保留单个空格）
			// 使用 strings.Fields 可以正确处理 Unicode 字符
			words := strings.Fields(trimmed)
			if len(words) > 0 {
				cleanedLines = append(cleanedLines, strings.Join(words, " "))
			}
		}
	}

	// 3. 重新组合
	result := strings.Join(cleanedLines, "\n")

	// 4. 再次 trim 整体文本
	result = strings.TrimSpace(result)

	return result
}

// truncateCleanly 智能截断文本，避免在单词或句子中间截断
func truncateCleanly(text string, maxLen int) string {
	if text == "" {
		return ""
	}

	// 转换为 rune 切片以正确处理 Unicode
	runes := []rune(text)

	if len(runes) <= maxLen {
		return text
	}

	// 截取到指定长度
	truncated := runes[:maxLen]

	// 尝试在合适的截断点截断：
	// 优先级：句号 > 问号 > 感叹号 > 分号 > 逗号 > 空格
	breakPoints := []rune{
		'。', '！', '？', // 中文标点
		'.', '?', '!', // 英文标点
		';', '：', ':', // 其他标点
		'，', ',', // 逗号
		' ', '\n', // 空格和换行
	}

	// 从截断点向前查找合适的截断位置（向前查找最多 100 个字符）
	searchStart := len(truncated) - 1
	searchEnd := len(truncated) - 100
	if searchEnd < 0 {
		searchEnd = 0
	}

	for i := searchStart; i >= searchEnd; i-- {
		for _, bp := range breakPoints {
			if truncated[i] == bp {
				// 找到截断点，保留这个标点符号
				return string(truncated[:i+1])
			}
		}
	}

	// 如果没有找到合适的截断点，直接截断并添加省略号
	result := string(truncated)
	if len(result) < len(runes) {
		result += "..."
	}

	return result
}

// ExtractPlainText 从 Markdown 内容中提取纯文本（完整版本，不限长度）
// 这个函数会去除所有 Markdown 语法，保留纯文本内容
func ExtractPlainText(markdownContent string) string {
	if markdownContent == "" {
		return ""
	}

	p := createParser()
	doc := markdown.Parse([]byte(markdownContent), p)
	text := extractTextFromNode(doc)
	cleanedText := cleanWhitespace(text)

	return cleanedText
}

// RenderMarkdownToHTML 将 Markdown 转换为 HTML（用于前端渲染）
func RenderMarkdownToHTML(markdownContent string) string {
	if markdownContent == "" {
		return ""
	}

	p := createParser()
	html := markdown.ToHTML([]byte(markdownContent), p, nil)
	return string(html)
}

// RenderMarkdownToTextWithFormat 将 Markdown 转换为格式化的纯文本
// 保留基本的格式结构（如标题、列表等），但去除 Markdown 语法
func RenderMarkdownToTextWithFormat(markdownContent string) string {
	if markdownContent == "" {
		return ""
	}

	p := createParser()
	doc := markdown.Parse([]byte(markdownContent), p)

	var buf bytes.Buffer

	ast.WalkFunc(doc, func(node ast.Node, entering bool) ast.WalkStatus {
		if !entering {
			return ast.GoToNext
		}

		switch n := node.(type) {
		case *ast.Heading:
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
			// 添加标题层级标记
			buf.WriteString(strings.Repeat("#", n.Level))
			buf.WriteString(" ")
		case *ast.Paragraph:
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
		case *ast.List:
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
		case *ast.ListItem:
			buf.WriteString("• ")
		case *ast.CodeBlock:
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
			buf.WriteString("[代码块]\n")
			buf.Write(n.Literal)
			buf.WriteString("\n[/代码块]\n\n")
		case *ast.Code:
			buf.WriteString("[代码]")
			buf.Write(n.Literal)
			buf.WriteString("[/代码]")
		case *ast.Text:
			buf.Write(n.Literal)
		case *ast.Link:
			buf.Write(n.Title)
		case *ast.Image:
			buf.WriteString("[图片: ")
			buf.Write(n.Title)
			buf.WriteString("]")
		case *ast.BlockQuote:
			if buf.Len() > 0 {
				buf.WriteString("\n\n")
			}
			buf.WriteString("> ")
		}

		return ast.GoToNext
	})

	return buf.String()
}

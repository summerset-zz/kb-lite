package daemon

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// FormatTOC 渲染 docs/04 规定的 toc 文本格式。
//
//	<编号> <标题> · 268 字
//	<编号> <标题> · 空
//
// 规则（照抄，不要美化）：
//   - 行首缩进 = 相对最浅一层的层级，2 空格/级；
//   - `· 空` 表示这一节自己没有正文（通常是个父标题）；
//   - 导言（section 0）**不在** toc 里 —— 它是单独的节，直接传 n=0 读。
func FormatTOC(entries []core.TocEntry) string {
	if len(entries) == 0 {
		return ""
	}

	shallowest := entries[0].Level
	for _, e := range entries {
		if e.Level < shallowest {
			shallowest = e.Level
		}
	}

	var b strings.Builder
	for i, e := range entries {
		if i > 0 {
			b.WriteByte('\n')
		}
		depth := e.Level - shallowest
		if depth < 0 {
			depth = 0
		}
		b.WriteString(strings.Repeat("  ", depth))
		b.WriteString(strconv.Itoa(e.No))
		b.WriteByte(' ')
		b.WriteString(e.Title)
		if e.Chars == 0 {
			b.WriteString(" · 空")
		} else {
			b.WriteString(" · ")
			b.WriteString(strconv.Itoa(e.Chars))
			b.WriteString(" 字")
		}
	}
	return b.String()
}

// 标题归一化用的转义集合（docs/04 第 4 条）。
const escapableChars = "\\`*_{}[]()#+.!~<>|=-"

var trailingSuffix = regexp.MustCompile(`_\d+$`)

// NormalizeTitle 按 docs/04 的七步把章节标题洗成可比较的两个键。
//
//	full    = 空白折叠 + 小写之后的完整标题
//	compact = full 去掉全部空白
//
// 只做「一模一样」这一档：不做相似度、不做编辑距离。相似度会猜错，而猜错
// 比直接告诉调用方「对不上，这是候选清单」更糟（ADR-010）。
func NormalizeTitle(s string) (full, compact string) {
	// 1. 百分号解码（对不上就保持原样，不能因为一个坏 % 让整次定位失败）
	if decoded, err := url.PathUnescape(s); err == nil {
		s = decoded
	}

	// 2. 全角 U+FF01–FF5E → 半角；3. U+3000 / U+00A0 等空白 → 普通空格。
	runes := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			runes = append(runes, r-0xFEE0)
		case unicode.IsSpace(r):
			runes = append(runes, ' ')
		default:
			runes = append(runes, r)
		}
	}

	// 4. 还原 Markdown 残留转义（`\(` → `(`）。
	s = unescapeMarkdown(string(runes))

	// 5. 空白折叠 + 小写。
	full = strings.ToLower(strings.Join(strings.Fields(s), " "))
	// 6. 紧凑键 = 去掉全部空白。
	compact = strings.ReplaceAll(full, " ", "")
	return full, compact
}

// unescapeMarkdown 把 `\X` 还原成 `X`，但只对 Markdown 里真的会被转义的那些字符。
func unescapeMarkdown(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		if runes[i] == '\\' && i+1 < len(runes) && strings.ContainsRune(escapableChars, runes[i+1]) {
			b.WriteRune(runes[i+1])
			i++
			continue
		}
		b.WriteRune(runes[i])
	}
	return b.String()
}

// MatchSectionsByTitle 按标题找节（docs/04「读一节的正文」的定位算法）。
//
// 两个档位：**一模一样**，以及**剥掉 `_\d+$` 重名后缀之后再一模一样**。
// 返回所有命中的节 —— 多于一个时上层要报「同名章节」，让调用方自己挑编号。
func MatchSectionsByTitle(sections []core.Section, query string) []core.Section {
	_, qCompact := NormalizeTitle(query)
	if qCompact == "" {
		return nil
	}

	var out []core.Section
	for _, s := range sections {
		_, c := NormalizeTitle(s.Title)
		if c == qCompact {
			out = append(out, s)
		}
	}
	if len(out) > 0 {
		return out
	}

	// 直连失败时剥掉重名后缀再比一次（`常见问题_2` → `常见问题`）。
	stripped := trailingSuffix.ReplaceAllString(qCompact, "")
	if stripped == "" || stripped == qCompact {
		return nil
	}
	for _, s := range sections {
		_, c := NormalizeTitle(s.Title)
		if c == stripped {
			out = append(out, s)
		}
	}
	return out
}

// Candidate 是「没找到」时回给调用方的候选（docs/04 的 candidates）。
type Candidate struct {
	Section      int    `json:"section"`
	SectionTitle string `json:"sectionTitle"`
	SectionPath  string `json:"sectionPath"`
	Chars        int    `json:"chars"`
}

// CandidatesOf 把整页的章节列表压成 candidates。
//
// 这是 ADR-010 的核心：「你查的东西不存在」时不是抛 404，而是把**真实可用的编号清单**
// 端给模型 —— 它下一次调用就对了。带上 chars 是为了让它一眼看出该读哪节。
func CandidatesOf(sections []core.Section) []Candidate {
	out := make([]Candidate, 0, len(sections))
	for _, s := range sections {
		out = append(out, Candidate{
			Section:      s.No,
			SectionTitle: s.Title,
			SectionPath:  s.PathText,
			Chars:        s.Chars,
		})
	}
	return out
}

package core

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	// DefaultMaxSectionChars 是一节正文的读取上限。超过只标记 Long，**不拆编号** ——
	// 编号一旦不稳定，agent 手上的编号立刻失效（ADR-009）。
	DefaultMaxSectionChars = 1500

	// LeadTitle 是导言节的标题。
	LeadTitle = "导言"

	// PathSeparator 是 PathText 的分隔符。
	PathSeparator = " / "
)

// SliceOptions 是切片参数。
type SliceOptions struct {
	// MaxSectionChars <= 0 时用 DefaultMaxSectionChars。
	MaxSectionChars int
}

func (o SliceOptions) maxSectionChars() int {
	if o.MaxSectionChars <= 0 {
		return DefaultMaxSectionChars
	}
	return o.MaxSectionChars
}

// ParseMarkdownSections 按默认参数切片。
func ParseMarkdownSections(markdown string) SplitResult {
	return ParseMarkdownSectionsWith(markdown, SliceOptions{})
}

// ParseMarkdownSectionsWith 是 docs/02-slicing.md「分节算法」的逐条实现。
//
// 逐行扫描而不是用完整 Markdown 解析器：我们只要「按标题行切段」，
// 行内语法一个都不用解释（除了标题里那点修饰）。一个完整解析器在这里是净负担，
// 而且它的边界行为（比如 setext 标题、HTML 块）不是我们要的。
func ParseMarkdownSectionsWith(markdown string, opts SliceOptions) SplitResult {
	lines := splitLines(markdown)
	heads := scanHeadings(lines)

	// 导言 = 第一个标题之前的全部行。没有标题时就是整篇 —— 于是「一篇没有标题的
	// 4000 字笔记」会成为一个 4000 字的 section 0，正好符合 docs/02 的「超长节」。
	leadEnd := len(lines)
	if len(heads) > 0 {
		leadEnd = heads[0].line
	}
	lead := joinTrimmed(lines[:leadEnd])

	if len(heads) == 0 && lead == "" {
		return SplitResult{}
	}

	limit := opts.maxSectionChars()
	sections := make([]Section, 0, len(heads)+1)
	if lead != "" {
		sections = append(sections, Section{
			No:       0,
			Level:    1,
			Title:    LeadTitle,
			Path:     []string{LeadTitle},
			PathText: LeadTitle,
			Content:  lead,
			Chars:    runeLen(lead),
		})
	}

	// 标题栈：弹出所有 level >= 当前 level 的项，然后 push 当前项。
	// 第一个标题若是 h3（栈为空），Path 就是它自己，不做层级补齐。
	var stack []stackItem
	for i, h := range heads {
		end := len(lines)
		if i+1 < len(heads) {
			end = heads[i+1].line
		}
		content := joinTrimmed(lines[h.line+1 : end])

		for len(stack) > 0 && stack[len(stack)-1].level >= h.level {
			stack = stack[:len(stack)-1]
		}
		path := make([]string, 0, len(stack)+1)
		for _, item := range stack {
			path = append(path, item.title)
		}
		path = append(path, h.title)
		stack = append(stack, stackItem{level: h.level, title: h.title})

		// 编号永远从 1 起、按出现顺序递增 —— 与导言存不存在无关。
		sections = append(sections, Section{
			No:       i + 1,
			Level:    h.level,
			Title:    h.title,
			Path:     path,
			PathText: strings.Join(path, PathSeparator),
			Content:  content,
			Chars:    runeLen(content),
		})
	}

	toc := make([]TocEntry, 0, len(sections))
	for _, s := range sections {
		if s.No == 0 {
			continue // 导言不是 toc 的一行
		}
		toc = append(toc, TocEntry{
			No:       s.No,
			Level:    s.Level,
			Title:    s.Title,
			PathText: s.PathText,
			Chars:    s.Chars,
			Long:     s.Chars > limit,
		})
	}

	return SplitResult{Summary: lead, Sections: sections, TOC: toc}
}

type stackItem struct {
	level int
	title string
}

type heading struct {
	level int
	title string
	// line 是标题行在 lines 里的下标（0 基）。
	line int
}

// scanHeadings 找出所有真正的标题行。
//
// 围栏状态机：以 ``` 或 ~~~ 开头的行翻转状态（围栏内的 # 不算标题），
// 未闭合到文件末尾同样有效。这是 docs/02 第 1 步。
func scanHeadings(lines []string) []heading {
	inFence := false
	var out []heading
	for i, line := range lines {
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		level, title, ok := parseHeading(line)
		if !ok {
			continue
		}
		out = append(out, heading{level: level, title: title, line: i})
	}
	return out
}

func isFenceLine(line string) bool {
	// 允许前导空白；同时 ``` 与 ```` 都算围栏行（```` 用来包住含 ``` 的示例）。
	t := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~")
}

// parseHeading 解析一行 ATX 标题。
//
// 规则（比 docs/02 里那行正则更严一点，见该文档的「实现注记」）：
//   - 顶格的 1–6 个 #，后面必须跟至少一个空格/制表符；
//   - 尾部闭合序列的 # 只有在**前面是空白**时才算闭合 —— 否则 `# C#` 会被
//     削成 `C`，而 CommonMark 认为标题就是 `C#`；
//   - 清洗后标题为空（`#`、`# ###`）→ 不是标题，当普通正文行。
func parseHeading(line string) (level int, title string, ok bool) {
	n := 0
	for n < len(line) && line[n] == '#' {
		n++
	}
	if n == 0 || n > 6 {
		return 0, "", false
	}
	if n >= len(line) || (line[n] != ' ' && line[n] != '\t') {
		return 0, "", false
	}
	rest := strings.TrimLeft(line[n:], " \t")
	rest = trimClosingHashes(rest)
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return 0, "", false
	}
	title = cleanHeadingText(rest)
	if title == "" {
		return 0, "", false
	}
	return n, title, true
}

// trimClosingHashes 去掉 ATX 标题的尾部闭合序列（`## 标题 ##` → `标题`）。
//
// 关键：闭合的 # 必须前面是空白。`C#` 的 # 是标题内容的一部分。
func trimClosingHashes(s string) string {
	t := strings.TrimRight(s, " \t")
	j := len(t)
	for j > 0 && t[j-1] == '#' {
		j--
	}
	if j == len(t) {
		return s // 没有尾随 #
	}
	if j == 0 || t[j-1] == ' ' || t[j-1] == '\t' {
		return t[:j]
	}
	return s
}

var (
	// 图片：留 alt（没有 alt 就整个删掉）。
	imageRef = regexp.MustCompile(`!\[([^\]]*)\]\([^)]*\)`)
	// 行内链接：`[文字](目标 "title")` → 文字。裸 URL / autolink 不动。
	inlineLinkRef = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
)

// cleanHeadingText 去掉标题里的行内 markdown 修饰，只留纯文本。
func cleanHeadingText(s string) string {
	s = imageRef.ReplaceAllString(s, "$1")
	s = inlineLinkRef.ReplaceAllString(s, "$1")
	s = strings.ReplaceAll(s, "`", "")
	return stripEmphasisMarkers(s)
}

// stripEmphasisMarkers 去掉 `*` 与 `_` 当强调分隔符用的那些。
//
// 判据是「两侧都是 ASCII 字母/数字时保留，其余一律去掉」：
//   - `**粗体**` → `粗体`（两侧不是字母数字）
//   - `*斜体*`   → `斜体`
//   - `用*斜体*举例` → `用斜体举例`（汉字不算 word 字符 —— 中文强调常常不带空格）
//   - `snake_case` → 原样（两侧都是 ASCII 标识符字符）
//
// 注意这里只认 ASCII：用 unicode.IsLetter 的话 `用*斜体*举例` 会被误判成标识符。
func stripEmphasisMarkers(s string) string {
	if !strings.ContainsAny(s, "*_") {
		return s
	}
	runes := []rune(s)
	out := make([]rune, 0, len(runes))
	for i, r := range runes {
		if r == '*' || r == '_' {
			prev := i > 0 && isASCIIWordRune(runes[i-1])
			next := i+1 < len(runes) && isASCIIWordRune(runes[i+1])
			if prev && next {
				out = append(out, r)
			}
			continue
		}
		out = append(out, r)
	}
	return string(out)
}

func isASCIIWordRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}

// splitLines 把文本按行拆开，顺带把 CRLF / CR 统一成 LF（docs/02「解码」第 4 步）。
func splitLines(s string) []string {
	if strings.ContainsRune(s, '\r') {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
	}
	return strings.Split(s, "\n")
}

// joinTrimmed 把若干行拼回文本并去掉首尾空白。
// 空行（含中间的空行）原样保留 —— 正文里的空行是段落分隔，不能吃。
func joinTrimmed(lines []string) string {
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func runeLen(s string) int { return utf8.RuneCountInString(s) }

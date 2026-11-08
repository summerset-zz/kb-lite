package core

import (
	"path"
	"regexp"
	"strings"
)

// LowInformationStems 是「没有信息量的文件名」：标题取值链走到文件名这一档时，
// 这些名字换成父目录名（`docs/api/README.md` → `api`）。
//
// 理由见 docs/02-slicing.md：几百个 `README.md` / `index.md` 都叫「README」的话，
// 搜索结果的标题栏等于没有信息。语言代码同理 —— `creds/adobe-systems/en.md` 的标题
// 应当是 `adobe-systems`，而不是毫无意义的 `en`。
var LowInformationStems = map[string]bool{
	"readme":     true,
	"index":      true,
	"untitled":   true,
	"untitled-1": true,
	"untitled 1": true,

	// 语言代码。`<slug>/en.md` + `<slug>/zh.md` 的双语目录很常见。
	"en": true, "zh": true, "ja": true, "ko": true, "fr": true, "de": true,
	"es": true, "ru": true, "pt": true, "it": true, "nl": true, "pl": true,
	"tr": true, "ar": true, "cs": true, "sv": true,
	"zh-cn": true, "zh-tw": true, "zh-hans": true, "zh-hant": true,
	"en-us": true, "en-gb": true,
}

// SummaryLimit 是自动摘要的字数上限。
const SummaryLimit = 200

// BuildResult 是 BuildEntry 的结果。
type BuildResult struct {
	Entry Entry
	// Skipped 表示这一篇被 frontmatter 要求整篇跳过。
	Skipped    bool
	SkipReason string
	// Warnings 是给 daemon 记一笔的问题（frontmatter 解析失败之类）。core 自己不记日志。
	Warnings []string
}

// BuildEntry 把一个文件的内容变成 Entry。
//
// relPath 是相对 KB 根目录的 POSIX 路径（它决定标题兜底与 path 档匹配）。
// 输入是**原始文件内容**（可能含 frontmatter），frontmatter 在这里被剥掉。
func BuildEntry(relPath, src string, opts SliceOptions) BuildResult {
	var res BuildResult

	fm, body, warn := SplitFrontmatter(src)
	if warn != "" {
		res.Warnings = append(res.Warnings, warn)
	}
	if fm.Skip {
		res.Skipped = true
		res.SkipReason = "frontmatter"
		return res
	}

	split := ParseMarkdownSectionsWith(body, opts)

	entry := Entry{
		RelPath:  relPath,
		Title:    resolveTitle(fm, split, relPath),
		Summary:  resolveSummary(fm, split),
		Tags:     fm.Tags,
		Sections: split.Sections,
		TOC:      split.TOC,
		Chars:    runeLen(body),
	}
	entry.SearchTitle = strings.ToLower(entry.Title)
	entry.SearchPath = strings.ToLower(relPath)
	entry.SearchPathLeaf = strings.ToLower(pathLeafOf(relPath))
	entry.SearchMeta = strings.ToLower(joinNonEmpty(append([]string{entry.Summary}, fm.Tags...)...))
	// tocText 与 full 都封顶（docs/02 末段）：几千节的文档里，toc 全量拼起来并不小。
	entry.TocText = strings.ToLower(truncateRunes(tocTextOf(split.Sections), BodyLimit))
	entry.SearchFull = strings.ToLower(truncateRunes(body, BodyLimit))

	res.Entry = entry
	return res
}

// resolveTitle 是 docs/02 的标题取值链：
//
//	frontmatter.title
//	  → 文档第一个非空块就是 ATX 标题时的那个标题（等价于「导言为空」）
//	  → 文件名（低信息量时用父目录名）
func resolveTitle(fm Frontmatter, split SplitResult, relPath string) string {
	if fm.Title != "" {
		return fm.Title
	}
	// 导言为空 + 第一个节就是编号 1 → 文档是从标题开头的。
	if split.Summary == "" && len(split.Sections) > 0 && split.Sections[0].No == 1 {
		if t := strings.TrimSpace(split.Sections[0].Title); t != "" {
			return t
		}
	}
	return titleFromPath(relPath)
}

// resolveSummary 是 docs/02 的摘要取值链：
//
//	frontmatter.summary（人写的，原样用）
//	  → 导言里第一段非空、非表格、非代码块的文字（最多 200 字）
//	  → 空串
func resolveSummary(fm Frontmatter, split SplitResult) string {
	if s := strings.TrimSpace(fm.Summary); s != "" {
		return s
	}
	return SummarizeLead(split.Summary)
}

// SummarizeLead 把导言洗成能当简介用的样子（只洗摘要，不改正文）。
//
// 导言节（section 0）本身**不洗** —— 那里是给 agent 读的正文，链接和表格都要保留。
func SummarizeLead(lead string) string {
	block := firstSummaryBlock(dropLeadingTable(lead))
	if block == "" {
		return ""
	}
	return truncateRunes(cleanSummary(block), SummaryLimit)
}

// dropLeadingTable 去掉导言**开头连续的表格行**。
//
// 那是从网页/维基转过来的 infobox：`键: 值` 的结构化数据，当简介毫无意义
// （战争类条目的信息框动辄一两千字）。只删开头的 —— 正文里再出现表格时前面已经有字了。
func dropLeadingTable(text string) string {
	lines := splitLines(text)
	start := 0
	for start < len(lines) && strings.TrimSpace(lines[start]) == "" {
		start++
	}
	if start >= len(lines) || !strings.HasPrefix(strings.TrimSpace(lines[start]), "|") {
		return text
	}
	end := start
	for end < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[end]), "|") {
		end++
	}
	for end < len(lines) && strings.TrimSpace(lines[end]) == "" {
		end++
	}
	return strings.Join(lines[end:], "\n")
}

// firstSummaryBlock 取第一段「有意义的」文字。
//
// 跳过：空块、表格块、代码围栏块、分隔线。段落之间用空行分隔。
func firstSummaryBlock(text string) string {
	for _, block := range splitBlocks(text) {
		first := strings.TrimSpace(firstLine(block))
		switch {
		case first == "":
			continue
		case strings.HasPrefix(first, "|"):
			continue
		case strings.HasPrefix(first, "```"), strings.HasPrefix(first, "~~~"):
			continue
		case isThematicBreak(first):
			continue
		}
		return strings.TrimSpace(block)
	}
	return ""
}

func isThematicBreak(line string) bool {
	t := strings.TrimSpace(line)
	if len(t) < 3 {
		return false
	}
	c := t[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	for i := 0; i < len(t); i++ {
		if t[i] != c {
			return false
		}
	}
	return true
}

// splitBlocks 按空行切块。
func splitBlocks(text string) []string {
	var blocks []string
	var cur []string
	for _, line := range splitLines(text) {
		if strings.TrimSpace(line) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, strings.Join(cur, "\n"))
				cur = cur[:0]
			}
			continue
		}
		cur = append(cur, line)
	}
	if len(cur) > 0 {
		blocks = append(blocks, strings.Join(cur, "\n"))
	}
	return blocks
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

var (
	summaryImageRef = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	summaryLinkRef  = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// 引用角标 `[^cite]`；turndown 会把方括号转义成 `\[^cite\]`，那两个反斜杠要一起吃掉。
	summaryCiteRef = regexp.MustCompile(`\\?\[\^[^\]]*\\?\]`)
	multiBlankRe   = regexp.MustCompile(`\n{3,}`)
)

// cleanSummary 是摘要的清洗（docs/02「摘要」）：
// 去链接目标、图片整段删除、去引用角标、去站点残留的「（未创建）」、压缩空行。
func cleanSummary(s string) string {
	s = summaryImageRef.ReplaceAllString(s, "")
	s = summaryLinkRef.ReplaceAllString(s, "$1")
	s = summaryCiteRef.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "（未创建）", "")
	s = multiBlankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// titleFromPath 是标题链的最后一档。
func titleFromPath(relPath string) string {
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	dir, file := path.Split(relPath)
	stem := strings.TrimSuffix(file, path.Ext(file))
	if LowInformationStems[strings.ToLower(stem)] {
		parent := path.Base(strings.TrimSuffix(dir, "/"))
		if parent != "" && parent != "." && parent != "/" {
			return parent
		}
	}
	if stem != "" {
		return stem
	}
	return file
}

// pathLeafOf 取相对路径的末段（`ops/runbook.md` → `runbook.md`）。
func pathLeafOf(relPath string) string {
	relPath = strings.ReplaceAll(relPath, "\\", "/")
	if i := strings.LastIndexByte(relPath, '/'); i >= 0 {
		return relPath[i+1:]
	}
	return relPath
}

func tocTextOf(sections []Section) string {
	var b strings.Builder
	for _, s := range sections {
		if b.Len() > 0 {
			b.WriteByte(' ')
		}
		b.WriteString(s.Title)
		b.WriteByte(' ')
		b.WriteString(s.PathText)
	}
	return b.String()
}

func joinNonEmpty(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " ")
}

// truncateRunes 截到 n 个字符（rune），不会切断多字节字符。
func truncateRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}

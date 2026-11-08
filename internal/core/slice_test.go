package core

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// docs/02-slicing.md「示例」一节的那篇运维手册（frontmatter 已在解码阶段剥掉）。
const docExample = "线上环境的操作手册。详见[内部文档](https://example.com)。\n" +
	"\n" +
	"## 上线流程\n" +
	"\n" +
	"1. 打 tag\n" +
	"2. `deploy.sh`\n" +
	"\n" +
	"## 回滚\n" +
	"\n" +
	"### 快速回滚\n" +
	"\n" +
	"回滚到上一个 tag。\n" +
	"\n" +
	"## 附录\n" +
	"\n" +
	"````markdown\n" +
	"# 这不是标题\n" +
	"````\n"

func TestParseDocExample(t *testing.T) {
	got := ParseMarkdownSections(docExample)

	want := []struct {
		no       int
		level    int
		title    string
		pathText string
	}{
		{0, 1, "导言", "导言"},
		{1, 2, "上线流程", "上线流程"},
		{2, 2, "回滚", "回滚"},
		{3, 3, "快速回滚", "回滚" + PathSeparator + "快速回滚"},
		{4, 2, "附录", "附录"},
	}

	if len(got.Sections) != len(want) {
		t.Fatalf("节数 = %d, 想要 %d：%+v", len(got.Sections), len(want), got.Sections)
	}
	for i, w := range want {
		s := got.Sections[i]
		if s.No != w.no || s.Level != w.level || s.Title != w.title || s.PathText != w.pathText {
			t.Errorf("第 %d 节 = {No:%d Level:%d Title:%q PathText:%q}，想要 %+v",
				i, s.No, s.Level, s.Title, s.PathText, w)
		}
		// Chars 必须永远等于 Content 的 rune 数 —— 这个不变式撑着 toc 里那行字数。
		if n := utf8.RuneCountInString(s.Content); s.Chars != n {
			t.Errorf("第 %d 节 Chars = %d，但 Content 有 %d 个字符", i, s.Chars, n)
		}
	}

	// 空父节必须保留：`## 回滚` 自己没有正文，正文在它的子节里。
	if got.Sections[2].Content != "" || got.Sections[2].Chars != 0 {
		t.Errorf("「回滚」应当是空节，实际 Content=%q Chars=%d",
			got.Sections[2].Content, got.Sections[2].Chars)
	}

	// 导言节不洗：链接原样保留（它是给 agent 读的正文，不是摘要）。
	if !strings.Contains(got.Sections[0].Content, "[内部文档](https://example.com)") {
		t.Errorf("导言节被清洗了：%q", got.Sections[0].Content)
	}
	if got.Sections[0].Chars != 40 {
		t.Errorf("导言 Chars = %d，想要 40", got.Sections[0].Chars)
	}

	// 代码围栏里的 `# 这不是标题` 不能产生新节，而且要留在「附录」的正文里。
	if !strings.Contains(got.Sections[4].Content, "# 这不是标题") {
		t.Errorf("围栏内容丢了：%q", got.Sections[4].Content)
	}

	// toc 不含导言。
	if len(got.TOC) != 4 {
		t.Fatalf("toc 有 %d 行，想要 4：%+v", len(got.TOC), got.TOC)
	}
	if got.TOC[0].No != 1 || got.TOC[1].No != 2 || got.TOC[3].No != 4 {
		t.Errorf("toc 编号不对：%+v", got.TOC)
	}
	if got.TOC[1].Chars != 0 {
		t.Errorf("toc 里「回滚」的 Chars 应为 0（空节），实际 %d", got.TOC[1].Chars)
	}
}

func TestNumberingAlwaysStartsAtOne(t *testing.T) {
	// 文档以标题开头 → 没有导言节，但标题编号仍然从 1 起（不能因为缺 0 而错位）。
	got := ParseMarkdownSections("# 甲\n\n正文甲\n\n## 乙\n\n正文乙\n")

	if got.Summary != "" {
		t.Errorf("Summary = %q，想要空", got.Summary)
	}
	if _, ok := got.SectionByNo(0); ok {
		t.Error("不该存在导言节")
	}
	if len(got.Sections) != 2 {
		t.Fatalf("节数 = %d，想要 2", len(got.Sections))
	}
	if got.Sections[0].No != 1 || got.Sections[1].No != 2 {
		t.Errorf("编号 = %d,%d，想要 1,2", got.Sections[0].No, got.Sections[1].No)
	}
	if want := "甲" + PathSeparator + "乙"; got.Sections[1].PathText != want {
		t.Errorf("PathText = %q，想要 %q", got.Sections[1].PathText, want)
	}
}

func TestNoHeadingLongDocBecomesSingleSection(t *testing.T) {
	body := strings.Repeat("一", 4000)
	got := ParseMarkdownSections(body)

	if len(got.Sections) != 1 {
		t.Fatalf("节数 = %d，想要 1", len(got.Sections))
	}
	if s := got.Sections[0]; s.No != 0 || s.Title != LeadTitle || s.Chars != 4000 {
		t.Errorf("单节 = %+v", s)
	}
	if len(got.TOC) != 0 {
		t.Errorf("无标题文档不该有 toc，实际 %+v", got.TOC)
	}
}

func TestFencedBlockHidesHeadings(t *testing.T) {
	md := "## 真的标题\n" +
		"\n" +
		"~~~\n" +
		"## 假的\n" +
		"~~~\n" +
		"\n" +
		"## 另一个真的\n"
	got := ParseMarkdownSections(md)

	if len(got.Sections) != 2 || got.Sections[0].Title != "真的标题" || got.Sections[1].Title != "另一个真的" {
		t.Fatalf("截面不对：%+v", got.Sections)
	}
	if !strings.Contains(got.Sections[0].Content, "## 假的") {
		t.Errorf("围栏里的内容应当留在正文里：%q", got.Sections[0].Content)
	}
}

func TestUnclosedFenceSwallowsRest(t *testing.T) {
	md := "## 标题\n\n```\n## 后面的都不算\n"
	got := ParseMarkdownSections(md)

	if len(got.Sections) != 1 {
		t.Fatalf("未闭合的围栏应当吞掉后面所有标题，实际 %+v", got.Sections)
	}
	if !strings.Contains(got.Sections[0].Content, "## 后面的都不算") {
		t.Errorf("正文丢了：%q", got.Sections[0].Content)
	}
}

func TestHeadingParsing(t *testing.T) {
	cases := []struct {
		line  string
		level int
		title string
		is    bool
	}{
		{"# 一级", 1, "一级", true},
		{"###### 六级", 6, "六级", true},
		{"####### 七级不是标题", 0, "", false},
		{"#没有空格", 0, "", false},
		{"#", 0, "", false},
		{"# ###", 0, "", false},
		{"## 标题 ##", 2, "标题", true},
		{"## 标题##", 2, "标题##", true}, // 闭合序列前面必须有空白
		{"# C#", 1, "C#", true},      // 不能被削成 C
		{"## 用 **粗体** 和 `code` 举例", 2, "用 粗体 和 code 举例", true},
		{"## 用*斜体*举例", 2, "用斜体举例", true},
		{"## snake_case 保留", 2, "snake_case 保留", true},
		{"## [链接文字](https://x.example/a_b)", 2, "链接文字", true},
		{"## ![](a.png)", 0, "", false}, // 没有 alt 的图片整个删掉，清洗后标题为空 → 当正文行
		{"## ![图](a.png)", 2, "图", true},
		{"## 图片 ![alt](a.png) 保留 alt", 2, "图片 alt 保留 alt", true},
		{"正文 ## 不是标题", 0, "", false},
	}

	for _, c := range cases {
		level, title, ok := parseHeading(c.line)
		if ok != c.is || level != c.level || title != c.title {
			t.Errorf("parseHeading(%q) = (%d, %q, %v)，想要 (%d, %q, %v)",
				c.line, level, title, ok, c.level, c.title, c.is)
		}
	}
}

func TestSetextHeadingIsNotHeading(t *testing.T) {
	// 我们只认 ATX。Setext（下划线式）标题会让「第一个标题之前的正文」判断变复杂，
	// 而真实笔记里几乎不用。
	md := "标题\n====\n\n正文\n"
	got := ParseMarkdownSections(md)

	if len(got.Sections) != 1 || got.Sections[0].No != 0 {
		t.Fatalf("Setext 不该产生标题：%+v", got.Sections)
	}
}

func TestLongFlag(t *testing.T) {
	md := "## 短\n\n一二三\n\n## 长\n\n" + strings.Repeat("字", 20) + "\n"
	got := ParseMarkdownSectionsWith(md, SliceOptions{MaxSectionChars: 10})

	if len(got.TOC) != 2 {
		t.Fatalf("toc 行数 = %d", len(got.TOC))
	}
	if got.TOC[0].Long {
		t.Error("「短」不该标 Long")
	}
	if !got.TOC[1].Long {
		t.Error("「长」应当标 Long")
	}
	// 编号不因为超长而改变（ADR-009）。
	if got.TOC[1].No != 2 {
		t.Errorf("超长节的编号 = %d，想要 2", got.TOC[1].No)
	}
}

func TestCRLF(t *testing.T) {
	md := "导言\r\n\r\n## 标题\r\n\r\n正文\r\n"
	got := ParseMarkdownSections(md)

	if len(got.Sections) != 2 {
		t.Fatalf("节数 = %d：%+v", len(got.Sections), got.Sections)
	}
	if got.Sections[0].Content != "导言" {
		t.Errorf("导言 = %q，CR 没被规范化", got.Sections[0].Content)
	}
	if strings.ContainsRune(got.Sections[1].Content, '\r') {
		t.Errorf("正文里残留 \\r：%q", got.Sections[1].Content)
	}
}

func TestEmptyInput(t *testing.T) {
	for _, in := range []string{"", "   ", "\n\n\n"} {
		got := ParseMarkdownSections(in)
		if len(got.Sections) != 0 || got.Summary != "" {
			t.Errorf("ParseMarkdownSections(%q) = %+v，想要空结果", in, got)
		}
	}
}

func TestSkippedHeadingLevels(t *testing.T) {
	// 从 h2 起跳到 h4：Path 直接用栈里能对上的那一层，不补齐、不跳号。
	md := "### 甲\n\n### 乙\n\n##### 丙\n"
	got := ParseMarkdownSections(md)

	if len(got.Sections) != 3 {
		t.Fatalf("节数 = %d", len(got.Sections))
	}
	if got.Sections[1].PathText != "乙" {
		t.Errorf("同级标题不该嵌套：%q", got.Sections[1].PathText)
	}
	if got.Sections[2].PathText != "乙"+PathSeparator+"丙" {
		t.Errorf("h3 下面的 h5 应当挂在 h3 上：%q", got.Sections[2].PathText)
	}
	if got.Sections[0].No != 1 || got.Sections[1].No != 2 || got.Sections[2].No != 3 {
		t.Errorf("编号不连续：%d,%d,%d",
			got.Sections[0].No, got.Sections[1].No, got.Sections[2].No)
	}
}

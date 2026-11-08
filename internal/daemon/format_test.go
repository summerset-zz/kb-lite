package daemon

import (
	"testing"

	"github.com/summerset-zz/kb-lite/internal/core"
)

func TestFormatTOC(t *testing.T) {
	entries := []core.TocEntry{
		{No: 1, Level: 2, Title: "上线流程", Chars: 18},
		{No: 2, Level: 2, Title: "回滚", Chars: 0},
		{No: 3, Level: 3, Title: "快速回滚", Chars: 11},
	}
	want := "1 上线流程 · 18 字\n2 回滚 · 空\n  3 快速回滚 · 11 字"
	if got := FormatTOC(entries); got != want {
		t.Errorf("FormatTOC =\n%q\n想要\n%q", got, want)
	}
}

func TestFormatTOCEmpty(t *testing.T) {
	if got := FormatTOC(nil); got != "" {
		t.Errorf("空 toc 应当渲染成空串，实际 %q", got)
	}
}

func TestFormatTOCSkipsNothingButLeadIsAbsentByDesign(t *testing.T) {
	// 导言（No == 0）由 core 保证不出现；这里断言渲染层也不会凭空加一行。
	result := core.ParseMarkdownSections("导言文字\n\n## 甲\n\n正文\n")
	got := FormatTOC(result.TOC)
	if got != "1 甲 · 2 字" {
		t.Errorf("FormatTOC = %q，想要 %q", got, "1 甲 · 2 字")
	}
}

func TestNormalizeTitle(t *testing.T) {
	cases := []struct {
		in            string
		full, compact string
	}{
		{"外部链接", "外部链接", "外部链接"},
		{"  外部　链接 ", "外部 链接", "外部链接"},      // 全角空格 + 首尾空白
		{"ＡＢＣ", "abc", "abc"},              // 全角大写 → 半角小写
		{`a\(b\)`, "a(b)", "a(b)"},         // 还原 Markdown 残留转义
		{"%E5%A4%96%E9%83%A8", "外部", "外部"}, // 百分号解码
		{"A  B", "a b", "ab"},              // 空白折叠
		{"", "", ""},
	}
	for _, c := range cases {
		full, compact := NormalizeTitle(c.in)
		if full != c.full || compact != c.compact {
			t.Errorf("NormalizeTitle(%q) = (%q, %q)，想要 (%q, %q)",
				c.in, full, compact, c.full, c.compact)
		}
	}
}

func testSections() []core.Section {
	return []core.Section{
		{No: 0, Level: 1, Title: "导言", PathText: "导言", Content: "前言", Chars: 2},
		{No: 1, Level: 2, Title: "回滚", PathText: "回滚", Chars: 0},
		{No: 2, Level: 3, Title: "快速回滚", PathText: "回滚 / 快速回滚", Content: "x", Chars: 1},
		{No: 3, Level: 2, Title: "外部链接", PathText: "外部链接", Content: "y", Chars: 1},
		{No: 4, Level: 2, Title: "外部链接", PathText: "外部链接", Content: "z", Chars: 1},
	}
}

func TestMatchSectionsByTitle(t *testing.T) {
	sections := testSections()

	cases := []struct {
		query string
		want  []int
	}{
		{"快速回滚", []int{2}},
		{" 快速 回滚 ", []int{2}}, // 空白差异靠紧凑键吃掉
		{"外部链接", []int{3, 4}}, // 同名 → 全给出来，让调用方挑
		{"回滚_2", []int{1}},    // 剥掉 `_\d+$` 后缀再比一次
		{"不存在的标题", nil},
		{"", nil},
	}
	for _, c := range cases {
		got := MatchSectionsByTitle(sections, c.query)
		if len(got) != len(c.want) {
			t.Errorf("MatchSectionsByTitle(%q) 命中 %d 节，想要 %v", c.query, len(got), c.want)
			continue
		}
		for i := range c.want {
			if got[i].No != c.want[i] {
				t.Errorf("MatchSectionsByTitle(%q) = 编号 %d，想要 %v", c.query, got[i].No, c.want)
				break
			}
		}
	}
}

func TestMatchSectionsDoesNotFuzzyMatch(t *testing.T) {
	sections := testSections()
	// 「回」不是「回滚」—— 不做相似度、不做前缀匹配。
	if got := MatchSectionsByTitle(sections, "回"); len(got) != 0 {
		t.Errorf("不该模糊命中，实际 %v", got)
	}
}

func TestCandidatesOf(t *testing.T) {
	got := CandidatesOf(testSections())
	if len(got) != 5 {
		t.Fatalf("candidates = %d 条", len(got))
	}
	if got[1].Section != 1 || got[1].SectionTitle != "回滚" || got[1].Chars != 0 {
		t.Errorf("第二条 = %+v", got[1])
	}
	if got[2].SectionPath != "回滚 / 快速回滚" {
		t.Errorf("SectionPath = %q", got[2].SectionPath)
	}
}

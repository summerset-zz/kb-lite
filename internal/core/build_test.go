package core

import (
	"strings"
	"testing"
)

func TestTitleChain(t *testing.T) {
	cases := []struct {
		name    string
		relPath string
		src     string
		want    string
	}{
		{
			name:    "frontmatter 最高优先",
			relPath: "ops/runbook.md",
			src:     "---\ntitle: 运维手册\n---\n\n# 别的标题\n",
			want:    "运维手册",
		},
		{
			name:    "文档以标题开头",
			relPath: "ops/runbook.md",
			src:     "# 运维手册\n\n正文\n",
			want:    "运维手册",
		},
		{
			name:    "取不到就用文件名",
			relPath: "ops/runbook.md",
			src:     "只有正文，没有标题。\n",
			want:    "runbook",
		},
		{
			name:    "README 用父目录名",
			relPath: "docs/api/README.md",
			src:     "只有正文。\n",
			want:    "api",
		},
		{
			name:    "根目录 README 没有父目录",
			relPath: "README.md",
			src:     "只有正文。\n",
			want:    "README",
		},
		{
			name:    "index.md 同样处理",
			relPath: "guides/index.md",
			src:     "只有正文。\n",
			want:    "guides",
		},
		{
			name:    "有导言时标题不能取文档里第一个标题",
			relPath: "ops/runbook.md",
			src:     "前言。\n\n# 前言\n\n正文\n",
			want:    "runbook",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := BuildEntry(c.relPath, c.src, SliceOptions{})
			if got.Entry.Title != c.want {
				t.Errorf("Title = %q，想要 %q", got.Entry.Title, c.want)
			}
		})
	}
}

func TestSummaryFromFrontmatterWins(t *testing.T) {
	src := "---\nsummary: 人写的摘要\n---\n\n导言里也有字。\n"
	got := BuildEntry("a.md", src, SliceOptions{})
	if got.Entry.Summary != "人写的摘要" {
		t.Errorf("Summary = %q", got.Entry.Summary)
	}
}

func TestSummaryAutoAndClean(t *testing.T) {
	src := "| 战役 | 上古之战 |\n" +
		"| --- | --- |\n" +
		"\n" +
		"这是一场[大战](../../wiki/战争)，详见[文档](https://x.example/a_b)。[^cite1]\n" +
		"\n" +
		"第二段不该被选中。\n"

	got := BuildEntry("warcraft/ancient-war.md", src, SliceOptions{})
	want := "这是一场大战，详见文档。"
	if got.Entry.Summary != want {
		t.Errorf("Summary = %q，想要 %q", got.Entry.Summary, want)
	}
}

func TestSummarySkipsCodeBlock(t *testing.T) {
	src := "```go\nfunc main() {}\n```\n\n这才是摘要。\n"
	got := BuildEntry("a.md", src, SliceOptions{})
	if got.Entry.Summary != "这才是摘要。" {
		t.Errorf("Summary = %q", got.Entry.Summary)
	}
}

func TestSummaryLimit(t *testing.T) {
	long := strings.Repeat("字", 300)
	got := BuildEntry("a.md", long+"\n", SliceOptions{})
	if n := len([]rune(got.Entry.Summary)); n != SummaryLimit {
		t.Errorf("摘要长度 = %d，想要 %d", n, SummaryLimit)
	}
}

func TestFrontmatterSkip(t *testing.T) {
	for _, src := range []string{
		"---\ndraft: true\n---\n\n正文\n",
		"---\npublish: false\n---\n\n正文\n",
		"---\nkb_ignore: true\n---\n\n正文\n",
	} {
		got := BuildEntry("a.md", src, SliceOptions{})
		if !got.Skipped {
			t.Errorf("应当跳过：%q", src)
		}
	}

	// publish: true 不跳过。
	if got := BuildEntry("a.md", "---\npublish: true\n---\n\n正文\n", SliceOptions{}); got.Skipped {
		t.Error("publish: true 不该被跳过")
	}
}

func TestTagsNormalization(t *testing.T) {
	cases := []struct {
		src  string
		want []string
	}{
		{"---\ntags: [ops, 上线]\n---\n\n正文\n", []string{"ops", "上线"}},
		{"---\ntags: ops, 上线\n---\n\n正文\n", []string{"ops", "上线"}},
		{"---\ntags:\n  - ops\n  - 上线\n---\n\n正文\n", []string{"ops", "上线"}},
		{"---\ntags:\n---\n\n正文\n", nil},
	}
	for _, c := range cases {
		got := BuildEntry("a.md", c.src, SliceOptions{})
		if len(got.Entry.Tags) != len(c.want) {
			t.Fatalf("Tags = %v，想要 %v", got.Entry.Tags, c.want)
		}
		for i := range c.want {
			if got.Entry.Tags[i] != c.want[i] {
				t.Errorf("Tags = %v，想要 %v", got.Entry.Tags, c.want)
				break
			}
		}
	}
}

func TestBrokenFrontmatterIsKeptAsBody(t *testing.T) {
	src := "---\ntitle: [未闭合\n---\n\n正文\n"
	got := BuildEntry("a.md", src, SliceOptions{})

	if len(got.Warnings) == 0 {
		t.Error("应当有一条 warn")
	}
	// 不能被静默吞掉：那段 `---` 要留在正文里。
	if !strings.Contains(got.Entry.SearchFull, "title: [未闭合") {
		t.Errorf("正文丢了：%q", got.Entry.SearchFull)
	}
	// 标题只能走文件名兜底。
	if got.Entry.Title != "a" {
		t.Errorf("Title = %q，想要 a", got.Entry.Title)
	}
}

func TestUnclosedFrontmatterIsNotFrontmatter(t *testing.T) {
	src := "---\ntitle: 没有闭合\n\n正文\n"
	got := BuildEntry("a.md", src, SliceOptions{})

	if len(got.Warnings) != 0 {
		t.Errorf("不该有 warn：%v", got.Warnings)
	}
	if !strings.Contains(got.Entry.SearchFull, "---") {
		t.Errorf("正文丢了：%q", got.Entry.SearchFull)
	}
}

func TestSearchFieldsAreLowercased(t *testing.T) {
	src := "---\ntitle: Element.scrollTo\ntags: [Scroll]\n---\n\n调用 Element.scrollTo() 即可。\n\n## Map 与 Set\n"
	got := BuildEntry("dom/api.md", src, SliceOptions{})

	if got.Entry.SearchTitle != "element.scrollto" {
		t.Errorf("SearchTitle = %q", got.Entry.SearchTitle)
	}
	if !strings.Contains(got.Entry.SearchMeta, "scroll") {
		t.Errorf("SearchMeta 没带上标签：%q", got.Entry.SearchMeta)
	}
	if !strings.Contains(got.Entry.TocText, "map 与 set") {
		t.Errorf("TocText 没带上章节标题：%q", got.Entry.TocText)
	}
	if strings.ContainsAny(got.Entry.SearchFull, "EFS") {
		t.Errorf("SearchFull 没小写：%q", got.Entry.SearchFull)
	}
}

func TestEntryCharsExcludesFrontmatter(t *testing.T) {
	src := "---\ntitle: 甲\n---\n\n正文\n"
	got := BuildEntry("a.md", src, SliceOptions{})
	// body = "\n正文\n"
	if got.Entry.Chars != 4 {
		t.Errorf("Chars = %d，想要 4", got.Entry.Chars)
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("中文abc", 2); got != "中文" {
		t.Errorf("truncateRunes = %q", got)
	}
	if got := truncateRunes("中文abc", 10); got != "中文abc" {
		t.Errorf("truncateRunes = %q", got)
	}
	if got := truncateRunes("中文", 0); got != "" {
		t.Errorf("truncateRunes = %q", got)
	}
}

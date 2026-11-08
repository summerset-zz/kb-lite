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

func TestLenientFrontmatterRecoversUnquotedColon(t *testing.T) {
	// 实测自 fontcover：值里出现未加引号的冒号，严格 YAML 必然报错。
	src := "---\ntitle: WOFF\ndescription: The format: a TTF wrapped in compression.\nkeywords: WOFF, WOFF2, 网页字体\n---\n\n## 正文\n\n内容\n"
	got := BuildEntry("docs/en/woff.md", src, SliceOptions{})

	if got.Entry.Title != "WOFF" {
		t.Errorf("Title = %q", got.Entry.Title)
	}
	if got.Entry.Summary != "The format: a TTF wrapped in compression." {
		t.Errorf("Summary = %q", got.Entry.Summary)
	}
	if len(got.Entry.Tags) != 3 || got.Entry.Tags[0] != "WOFF" {
		t.Errorf("Tags = %v", got.Entry.Tags)
	}
	if len(got.Warnings) != 1 || !strings.Contains(got.Warnings[0], "宽松解析") {
		t.Errorf("warnings = %v", got.Warnings)
	}
}

func TestLenientFrontmatterSplitsQuotedKeywords(t *testing.T) {
	// `keywords: "@font-face", src` 也不是合法 YAML（引号后面跟逗号）。
	src := "---\ntitle: x\nkeywords: \"@font-face\", src, local\n---\n\n正文\n"
	got := BuildEntry("a.md", src, SliceOptions{})

	want := []string{"@font-face", "src", "local"}
	if len(got.Entry.Tags) != len(want) {
		t.Fatalf("Tags = %v，想要 %v", got.Entry.Tags, want)
	}
	for i := range want {
		if got.Entry.Tags[i] != want[i] {
			t.Fatalf("Tags = %v，想要 %v", got.Entry.Tags, want)
		}
	}
}

func TestFrontmatterAliases(t *testing.T) {
	// name → title / description → summary / keywords → tags（fontcover 实体卡片的用法）
	src := "---\nname: Adobe Systems\nkinds: [maker]\n---\n\n正文\n"
	got := BuildEntry("creds/adobe-systems/meta.md", src, SliceOptions{})
	if got.Entry.Title != "Adobe Systems" {
		t.Errorf("name 别名没生效：Title = %q", got.Entry.Title)
	}

	// 主键必须压过别名。
	src = "---\ntitle: 正名\nname: 别名\ndescription: 描述\nsummary: 摘要\n---\n\n正文\n"
	got = BuildEntry("a.md", src, SliceOptions{})
	if got.Entry.Title != "正名" || got.Entry.Summary != "摘要" {
		t.Errorf("主键没压过别名：Title=%q Summary=%q", got.Entry.Title, got.Entry.Summary)
	}
}

func TestBrokenFrontmatterIsKeptAsBody(t *testing.T) {
	// 连宽松解析都拿不到任何键 —— 这时才整份当正文，不吞用户内容。
	src := "---\n这里没有任何键值行\n---\n\n正文\n"
	got := BuildEntry("a.md", src, SliceOptions{})

	if len(got.Warnings) == 0 || !strings.Contains(got.Warnings[0], "解析失败") {
		t.Errorf("warnings = %v", got.Warnings)
	}
	if !strings.Contains(got.Entry.SearchFull, "这里没有任何键值行") {
		t.Errorf("正文丢了：%q", got.Entry.SearchFull)
	}
	if got.Entry.Title != "a" {
		t.Errorf("Title = %q，想要 a", got.Entry.Title)
	}
}

func TestLanguageCodeFilenameUsesParentDir(t *testing.T) {
	got := BuildEntry("creds/adobe-systems/en.md", "正文，没有标题。\n", SliceOptions{})
	if got.Entry.Title != "adobe-systems" {
		t.Errorf("Title = %q，想要 adobe-systems", got.Entry.Title)
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

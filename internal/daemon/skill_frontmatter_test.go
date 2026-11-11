package daemon

import (
	"strings"
	"testing"
)

// TestSkillsHaveFrontmatter 盯一件非常具体的事：装到用户目录的那份 SKILL.md，
// **第一行必须是 `---`**，而且 frontmatter 里有 name 与 description。
//
// 这条是 WorkBuddy 报出来的：当时 `authoring.md` 渲染出来带着一段“给维护者的说明”
// 和一个 ```markdown 围栏，frontmatter 被埋在第十几行，harness 直接说“缺失 frontmatter”，
// 整个技能加载不上。我们的测试当时只比“模板与文档一致”，两边一起错就一起绿。
func TestSkillsHaveFrontmatter(t *testing.T) {
	docs := map[string]string{
		"read":      RenderSkillText(nil, "http://127.0.0.1:7331", "", DefaultFeatures),
		"authoring": RenderAuthoringText("http://127.0.0.1:7331", "", DefaultFeatures),
		"maintain":  RenderMaintainText(`C:\Tools\kb-lite\kb-lite.exe`),
	}
	wantName := map[string]string{
		"read":      "name: kbl-query",
		"authoring": "name: kbl-authoring",
		"maintain":  "name: kbl-maintain",
	}

	for kind, text := range docs {
		lines := strings.Split(normalizeNewlines(text), "\n")
		if len(lines) == 0 || lines[0] != "---" {
			t.Fatalf("%s 的第一行是 %q，必须是 `---`（否则 harness 认不出 frontmatter）",
				kind, first(lines))
		}
		// frontmatter 必须在开头附近就闭合，而且带 name。
		closed, hasName, hasDesc := -1, false, false
		for i := 1; i < len(lines) && i < 8; i++ {
			if lines[i] == "---" {
				closed = i
				break
			}
			if strings.HasPrefix(lines[i], wantName[kind]) {
				hasName = true
			}
			if strings.HasPrefix(lines[i], "description:") {
				hasDesc = true
			}
		}
		if closed < 0 {
			t.Errorf("%s 的 frontmatter 没有在开头闭合", kind)
		}
		if !hasName {
			t.Errorf("%s 的 frontmatter 里缺 %q", kind, wantName[kind])
		}
		if !hasDesc {
			t.Errorf("%s 的 frontmatter 里缺 description", kind)
		}
		// 围栏行漏进去也会让 harness 把整份当代码块。
		if strings.HasPrefix(lines[0], "```") {
			t.Errorf("%s 以围栏行开头", kind)
		}
	}
}

func first(lines []string) string {
	if len(lines) == 0 {
		return ""
	}
	return lines[0]
}

// 渲染结果里不该残留“给维护者看的”东西：文档头、围栏、占位符。
func TestSkillsAreCleanAfterRender(t *testing.T) {
	authoring := RenderAuthoringText("http://127.0.0.1:7331", "tok", DefaultFeatures)
	for _, bad := range []string{"```markdown", "# authoring 模板", "{{"} {
		if strings.Contains(authoring, bad) {
			t.Errorf("authoring 渲染结果里还留着 %q", bad)
		}
	}
	if !strings.Contains(authoring, "token") && !strings.Contains(authoring, "tok") {
		t.Error("开着访问密码时，authoring 里应当写出密码")
	}
}

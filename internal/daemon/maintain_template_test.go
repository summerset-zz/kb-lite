package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 维护模板与 docs/maintain-template.md 里的围栏块必须逐字一致。
//
// 与 skill-template 同一个套路：文档里有说明，围栏里是真正装到用户目录的那份文本。
// 改了接口或命令行参数却忘了改这份，agent 会照着一份过期的说明去操作（包括删库）。
func TestMaintainTemplateMatchesDocs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "maintain-template.md"))
	if err != nil {
		t.Fatalf("读不到 docs/maintain-template.md：%v", err)
	}
	want := strings.TrimRight(extractFencedBlock(normalizeNewlines(string(raw)), "```markdown"), "\n")
	if want == "" {
		t.Fatal("docs/maintain-template.md 里找不到 ```markdown 围栏块")
	}
	got := strings.TrimRight(normalizeNewlines(maintainTemplate), "\n")
	if got != want {
		t.Errorf("maintain_template.md 与 docs/maintain-template.md 不一致（模板 %d 字节 / 文档 %d 字节）：%s",
			len(got), len(want), firstDiff(got, want))
	}
}

// 这份说明书的几个硬要求。少一条就可能让 agent 做出危险动作。
func TestMaintainTemplateContent(t *testing.T) {
	text := RenderMaintainText()
	for _, want := range []string{
		"name: kbl-maintain",    // 装到目录里要能被认出来
		"shell",                 // 前提写在正文里，不只写在卡片上
		"kb-lite list --json",   // 先拉清单确认
		"--yes",                 // remove 会卡在交互确认上
		"文件一个都不会动",              // 回话必须说这句
		"改完重新拉一次清单",             // 改名后旧地址失效
		"不要用 `curl` 调 `/api/*`", // 管理凭据拿不到
	} {
		if !strings.Contains(text, want) {
			t.Errorf("维护说明书里缺 %q", want)
		}
	}
	// 它不该带任何占位符：维护走 CLI，不需要地址与密码（管理凭据永远不进文档）。
	for _, bad := range []string{"{{", "Bearer", "X-KB-Admin"} {
		if strings.Contains(text, bad) {
			t.Errorf("维护说明书里不该出现 %q", bad)
		}
	}
}

// 三份 SKILL 装到三个不同目录，互不覆盖。
func TestSkillKindsGoToDifferentDirs(t *testing.T) {
	seen := map[string]string{}
	for _, kind := range []string{"read", "authoring", "maintain"} {
		root, dir, err := skillDir(kind, "dsh")
		if err != nil {
			t.Fatalf("kind=%s：%v", kind, err)
		}
		p := filepath.Join(root, dir)
		if prev, dup := seen[p]; dup {
			t.Errorf("%s 与 %s 装到同一个目录 %s", kind, prev, p)
		}
		seen[p] = kind
	}
	// 不认识的种类要报错，别默默装成 read。
	if _, _, err := skillDir("whatever", "dsh"); err == nil {
		t.Error("不认识的 kind 应当报错")
	}
}

// `GET /maintain.md` 要能取到，并且属于读取面：开了访问密码就要带密码。
func TestMaintainDocRequiresToken(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: true})

	if code, _, _ := doRequest(t, srv, "/maintain.md", nil); code != http.StatusOK {
		t.Fatalf("带密码时 /maintain.md status = %d", code)
	}
	code, _, raw := doRequest(t, srv, "/maintain.md", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if code != http.StatusUnauthorized {
		t.Errorf("不带密码时 status = %d，想要 401", code)
	}
	if strings.Contains(raw, "kbl-maintain") {
		t.Error("认证没通过还把内容给出去了")
	}
}

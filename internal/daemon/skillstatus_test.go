package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestSkillStatus 守“装没装、是不是最新”。
//
// 界面要显示的是磁盘上的现状，不是“我刚才点过按钮”。所以：
// 没装过 → 未安装；装完 → 已是最新；文件被改过（或说明书内容变了）→ 有更新。
func TestSkillStatus(t *testing.T) {
	// 别写到真用户目录里：skillRoots 读 HOME / USERPROFILE。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	items := skillStatusItems(t, srv)
	if got := pickSkill(t, items, "read", "dsh"); got["installed"] != false {
		t.Fatalf("什么都没装却说已安装：%v", got)
	}

	status, _, raw := adminReq(t, srv, http.MethodPost, "/api/skill/install",
		skillInstallReq{Kind: "read", Targets: []string{"dsh"}})
	if status != http.StatusOK {
		t.Fatalf("安装失败：%s", raw)
	}

	items = skillStatusItems(t, srv)
	got := pickSkill(t, items, "read", "dsh")
	if got["installed"] != true || got["upToDate"] != true {
		t.Fatalf("装完应当是最新的：%v", got)
	}
	if _, err := os.Stat(filepath.Join(home, ".agents", "skills", "kbl-query", "SKILL.md")); err != nil {
		t.Fatalf("文件不在预期位置：%v", err)
	}

	// 另一份没装过，不能顺带说成已安装。
	if other := pickSkill(t, items, "authoring", "dsh"); other["installed"] != false {
		t.Errorf("整理那份没装，却报已安装：%v", other)
	}

	// 手改一个字：还在，但过时了。
	p := filepath.Join(home, ".agents", "skills", "kbl-query", "SKILL.md")
	if err := os.WriteFile(p, []byte("改过的内容\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	items = skillStatusItems(t, srv)
	got = pickSkill(t, items, "read", "dsh")
	if got["installed"] != true || got["upToDate"] != false {
		t.Fatalf("内容变了应当报“有更新”：%v", got)
	}

	// 整理那份走另一条渲染路径（RenderAuthoringText）。它也要与状态检查共用
	// 同一份 features，否则装完立刻就会显示“有更新”。
	status, _, raw = adminReq(t, srv, http.MethodPost, "/api/skill/install",
		skillInstallReq{Kind: "authoring", Targets: []string{"claude"}})
	if status != http.StatusOK {
		t.Fatalf("安装整理 SKILL 失败：%s", raw)
	}
	items = skillStatusItems(t, srv)
	got = pickSkill(t, items, "authoring", "claude")
	if got["installed"] != true || got["upToDate"] != true {
		t.Fatalf("刚装完的整理 SKILL 应当是最新的（安装与状态检查用的不是同一份渲染？）：%v", got)
	}
}

// skillStatusItems 取一次 `/api/skill/status` 的 items。
func skillStatusItems(t *testing.T, srv *httptest.Server) []any {
	t.Helper()
	status, body, raw := adminReq(t, srv, http.MethodGet, "/api/skill/status", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	items, ok := body["items"].([]any)
	if !ok || len(items) == 0 {
		t.Fatalf("items 形状不对：%s", raw)
	}
	return items
}

func pickSkill(t *testing.T, items []any, kind, target string) map[string]any {
	t.Helper()
	for _, it := range items {
		m, _ := it.(map[string]any)
		if m["kind"] == kind && m["target"] == target {
			return m
		}
	}
	t.Fatalf("状态里没有 %s/%s", kind, target)
	return nil
}

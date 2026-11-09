package daemon

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

// manifestOf 用一组条目直接造一个 KBIndex 来测（不需要真的落盘）。
func manifestOf(t *testing.T, files map[string]string) *KBIndex {
	t.Helper()
	root := t.TempDir()
	for rel, body := range files {
		writeFile(t, filepath.Join(root, rel), body)
	}
	m := NewManager(&Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}},
		BuildOptions{Workers: 1})
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}
	kb, ok := m.KB("笔记")
	if !ok {
		t.Fatal("KB 不存在")
	}
	return kb
}

func TestManifestGroupsAndSorts(t *testing.T) {
	kb := manifestOf(t, map[string]string{
		"ops/runbook.md":    "---\ntitle: 运维手册\n---\n\n## 一\n\n" + strings.Repeat("字", 300),
		"ops/migrate.md":    "---\ntitle: 服务器迁移\n---\n\n## 一\n\n" + strings.Repeat("字", 100),
		"guides/a.md":       "---\ntitle: 指南甲\n---\n\n## 一\n\n" + strings.Repeat("字", 50),
		"guides/b.md":       "---\ntitle: 指南乙\n---\n\n## 一\n\n" + strings.Repeat("字", 20),
		"guides/c.md":       "---\ntitle: 指南丙\n---\n\n## 一\n\n" + strings.Repeat("字", 10),
		"loose.md":          "---\ntitle: 散篇\n---\n\n## 一\n\n" + strings.Repeat("字", 5),
		"deep/x/y/z.md":     "---\ntitle: 很深\n---\n\n## 一\n\n字",
		"deep/x/y/other.md": "---\ntitle: 也很深\n---\n\n## 一\n\n字",
	})

	m := BuildManifest(kb, ManifestOptions{Budget: 100000})
	if m.Entries != 8 {
		t.Fatalf("条目数 = %d，想要 8", m.Entries)
	}
	// 目录按条目数降序：guides(3) → deep/x/y(2) 与 ops(2) 同量，按路径升序 → deep 在前。
	if len(m.Dirs) != 4 {
		t.Fatalf("分组数 = %d，想要 4：%+v", len(m.Dirs), m.Dirs)
	}
	if m.Dirs[0].Path != "guides" {
		t.Errorf("第一组 = %q，想要 guides", m.Dirs[0].Path)
	}
	if m.Dirs[1].Path != "deep/x/y" || m.Dirs[2].Path != "ops" {
		t.Errorf("同数量时按路径升序，实际 %q, %q", m.Dirs[1].Path, m.Dirs[2].Path)
	}
	// 根目录只有一个条目，排最后，且显示为「（根目录）」。
	if m.Dirs[3].Path != "" {
		t.Errorf("最后一组 = %q，想要根目录", m.Dirs[3].Path)
	}
	var guides []ManifestEntry
	for _, it := range m.Items {
		if strings.HasPrefix(it.Path, "guides/") {
			guides = append(guides, it)
		}
	}
	if len(guides) != 3 || guides[0].Title != "指南甲" || guides[2].Title != "指南丙" {
		t.Errorf("guides 组内顺序 = %+v", guides)
	}

	text := m.RenderManifestText()
	if !strings.Contains(text, "笔记 · 8 篇") {
		t.Errorf("表头不对：\n%s", text)
	}
	if !strings.Contains(text, "▸ guides/  3 篇") {
		t.Errorf("目录行不对：\n%s", text)
	}
}

func TestManifestBudgetFolds(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < 40; i++ {
		files["docs/doc-"+string(rune('a'+i%26))+string(rune('0'+i/26))+".md"] =
			"---\ntitle: 文档" + string(rune('a'+i%26)) + "---\n\n## 一\n\n" + strings.Repeat("字", 200)
	}
	kb := manifestOf(t, files)

	full := BuildManifest(kb, ManifestOptions{Budget: 100000})
	if full.Folded != 0 {
		t.Errorf("预算充足时不该折叠：folded = %d", full.Folded)
	}
	if len(full.Items) != 40 {
		t.Errorf("条目 = %d，想要 40", len(full.Items))
	}

	tight := BuildManifest(kb, ManifestOptions{Budget: 300})
	if tight.Folded == 0 {
		t.Fatal("预算 300 字应当有折叠")
	}
	text := tight.RenderManifestText()
	if !strings.Contains(text, "还有") {
		t.Errorf("折叠了就要说明：\n%s", text)
	}
	// 预算算的是**渲染出来的字符数**，末尾那句总结也算 —— 不然按 budget 切上下文会切掉一半。
	for _, budget := range []int{200, 300, 800, 2000} {
		m := BuildManifest(kb, ManifestOptions{Budget: budget})
		if n := len([]rune(m.RenderManifestText())); n > budget {
			t.Errorf("budget=%d 渲染了 %d 字，超预算", budget, n)
		}
	}
	// 折叠数 + 列出数 = 总数。
	if got := tight.Folded + len(tight.Items); got != 40 {
		t.Errorf("折叠 %d + 列出 %d = %d，想要 40", tight.Folded, len(tight.Items), got)
	}
}

func TestManifestPrefixAndDepth(t *testing.T) {
	kb := manifestOf(t, map[string]string{
		"docs/en/a.md": "---\ntitle: A\n---\n\n## 一\n\n字",
		"docs/zh/b.md": "---\ntitle: B\n---\n\n## 一\n\n字",
		"other/c.md":   "---\ntitle: C\n---\n\n## 一\n\n字",
	})

	// prefix 过滤
	m := BuildManifest(kb, ManifestOptions{Prefix: "docs"})
	if m.Entries != 2 {
		t.Errorf("prefix=docs 时条目 = %d，想要 2", m.Entries)
	}
	for _, it := range m.Items {
		if !strings.HasPrefix(it.Path, "docs/") {
			t.Errorf("%s 不该出现", it.Path)
		}
	}

	// depth=1 把 docs/en、docs/zh 合成一组 docs
	m = BuildManifest(kb, ManifestOptions{Depth: 1})
	for _, d := range m.Dirs {
		if strings.Contains(d.Path, "/") {
			t.Errorf("depth=1 时不该有二级目录：%q", d.Path)
		}
	}
	// 默认 depth=3 时它们是两组
	m = BuildManifest(kb, ManifestOptions{})
	if len(m.Dirs) != 3 {
		t.Errorf("默认 depth 下分组 = %d，想要 3", len(m.Dirs))
	}
}

func TestStatusEndpoint(t *testing.T) {
	srv := newTestServer(t)

	// 管理面要管理凭据：CLI 走头 + Origin（同源信号）。
	admin := func(r *http.Request) {
		r.Header.Set("X-KB-Admin", testAdminToken)
		r.Header.Set("Origin", srv.URL)
	}

	status, body, raw := doRequest(t, srv, "/api/status", admin)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["version"] == "" || body["version"] == nil {
		t.Error("缺 version")
	}
	if body["dbMode"] != "memory" {
		t.Errorf("dbMode = %v（测试里没开缓存，应当是 memory）", body["dbMode"])
	}
	if body["requireToken"] != true {
		t.Errorf("requireToken = %v，测试服务器开着密码，应当是 true", body["requireToken"])
	}
	kbs, _ := body["kbs"].([]any)
	if len(kbs) != 1 {
		t.Fatalf("kbs = %v", body["kbs"])
	}
	kb := kbs[0].(map[string]any)
	if kb["name"] != "笔记" || kb["state"] != "ready" {
		t.Errorf("kb = %v", kb)
	}
	if kb["entries"].(float64) != 1 {
		t.Errorf("entries = %v，想要 1", kb["entries"])
	}
	if kb["lastIndexedAt"].(float64) <= 0 {
		t.Error("lastIndexedAt 应当是毫秒时间戳")
	}

	// 只带读取密码不行：管理面要的是另一把钥匙（ADR-015）。
	status, _, _ = doRequest(t, srv, "/api/status", nil)
	if status != http.StatusUnauthorized {
		t.Errorf("只带读取密码时 status = %d，想要 401", status)
	}

	// 有管理凭据但没有同源信号（curl 就是这样）也不行。
	status, _, _ = doRequest(t, srv, "/api/status", func(r *http.Request) {
		r.Header.Set("X-KB-Admin", testAdminToken)
	})
	if status != http.StatusUnauthorized {
		t.Errorf("没有同源信号时 status = %d，想要 401", status)
	}

	// 同源页面的 cookie 可以过。
	status, _, _ = doRequest(t, srv, "/api/status", func(r *http.Request) {
		r.Header.Del("Authorization")
		r.AddCookie(&http.Cookie{Name: AdminCookieName, Value: testAdminToken})
	})
	if status != http.StatusOK {
		t.Errorf("cookie 认证 status = %d，想要 200", status)
	}
}

func TestManifestEndpoint(t *testing.T) {
	srv := newTestServer(t)

	// 默认文本
	status, _, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/manifest", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if !strings.Contains(raw, "笔记") || !strings.Contains(raw, "▸ ops/") {
		t.Errorf("文本形态不对：\n%s", raw)
	}

	// JSON
	status, body, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/manifest?format=json&budget=500", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["ok"] != true {
		t.Errorf("ok = %v", body["ok"])
	}
	if body["text"] == "" || body["text"] == nil {
		t.Error("JSON 里应当带 text")
	}
	dirs, _ := body["dirs"].([]any)
	if len(dirs) == 0 {
		t.Errorf("dirs 为空：%s", raw)
	}

	// 坏参数不报 400，退回默认（不值得让调用方处理错误分支）。
	status, _, _ = doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/manifest?budget=abc&depth=xyz", nil)
	if status != http.StatusOK {
		t.Errorf("坏参数时 status = %d，想要 200", status)
	}
}

// TestSkillShowsManifest 保证 skill 与实现同源：路由有了，文档里那一段就得在。
func TestSkillShowsManifest(t *testing.T) {
	srv := newTestServerWithFeatures(t, DefaultFeatures)
	_, _, raw := doRequest(t, srv, "/skill.md", nil)
	if !strings.Contains(raw, "/manifest?budget=2000") {
		t.Errorf("skill.md 里应当出现 manifest 用法：\n%s", raw)
	}
}

func TestSkillHidesManifestWhenAbsent(t *testing.T) {
	srv := newTestServerWithFeatures(t, map[string]bool{})
	_, _, raw := doRequest(t, srv, "/skill.md", nil)
	if strings.Contains(raw, "/manifest?budget=2000") {
		t.Error("没有 manifest 路由时不该在 skill.md 里出现")
	}
}

// 确保 JSON 里 dirs 的字段名与 docs/04 说的一致。
func TestManifestJSONShape(t *testing.T) {
	kb := manifestOf(t, map[string]string{"ops/a.md": "---\ntitle: A\n---\n\n## 一\n\n字"})
	m := BuildManifest(kb, ManifestOptions{})
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{`"name"`, `"dirs"`, `"items"`, `"folded"`, `"sections"`} {
		if !strings.Contains(string(raw), key) {
			t.Errorf("JSON 里缺 %s：%s", key, raw)
		}
	}
}

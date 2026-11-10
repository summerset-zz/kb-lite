package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAccessRing 守环形缓冲的三件事：只留最新 N 条、seq 单调、清空不重置 seq。
//
// 最后一条最容易被写错：清空时把 seq 归零，前端手里那个 since 就会把
// 新条目全部过滤掉，界面看着像“清空之后再也不更新了”。
func TestAccessRing(t *testing.T) {
	a := newAccessRing(3)
	for i := 0; i < 5; i++ {
		a.add(AccessEntry{Action: fmt.Sprintf("a%d", i)})
	}

	got := a.list(0, 10)
	if len(got) != 3 {
		t.Fatalf("只该留 3 条，实际 %d", len(got))
	}
	if got[0].Action != "a2" || got[2].Action != "a4" {
		t.Errorf("留下的应当是 a2..a4，实际 %v", []string{got[0].Action, got[1].Action, got[2].Action})
	}
	if got[2].Seq != 5 {
		t.Errorf("最新一条的 seq = %d，想要 5", got[2].Seq)
	}
	if total, dropped := a.stats(); total != 3 || dropped != 2 {
		t.Errorf("stats = (%d, %d)，想要 (3, 2)", total, dropped)
	}

	// since 过滤
	if got := a.list(4, 10); len(got) != 1 || got[0].Seq != 5 {
		t.Errorf("since=4 只该回 seq 5，实际 %+v", got)
	}

	a.clear()
	if a.latest() != 5 {
		t.Errorf("清空后 seq 不该回退，实际 %d", a.latest())
	}
	if len(a.list(0, 10)) != 0 {
		t.Error("清空后不该还有条目")
	}
	a.add(AccessEntry{Action: "b"})
	// 前端拿着 since=5 来拉，必须能拿到刚加的那条。
	if got := a.list(5, 10); len(got) != 1 || got[0].Action != "b" {
		t.Errorf("清空后新增的条目拿不到：%+v", got)
	}
}

func TestClientKind(t *testing.T) {
	cases := map[string]string{
		"kb-lite/0.4.1":            ClientCLI,
		"curl/8.4.0":               ClientCurl,
		"Mozilla/5.0 (Windows NT)": ClientBrowser,
		"python-requests/2.31.0":   ClientOther,
		"":                         ClientUnknown,
		"Go-http-client/1.1":       ClientOther,
	}
	for ua, want := range cases {
		if got := clientKind(ua); got != want {
			t.Errorf("clientKind(%q) = %q，想要 %q", ua, got, want)
		}
	}
}

// TestShouldRecord 守“哪些请求进日志”。这一条错了不是显示问题，是日志会被自己刷满。
func TestShouldRecord(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"GET", "/kb", true},
		{"GET", "/kb/笔记/search", true},
		{"GET", "/skill.md", true},
		{"GET", "/authoring.md", true},

		// 轮询与界面本身
		{"GET", "/api/status", false},
		{"GET", "/api/kbs/笔记/index", false},
		{"GET", "/api/logs", false},
		{"GET", "/", false},
		{"GET", "/assets/index-abc.js", false},

		// 管理面的读操作不记
		{"GET", "/api/kbs", false},
		{"POST", "/api/search", false},
		{"POST", "/api/preview", false},
		{"GET", "/api/fs/list", false},

		// 有副作用的才记
		{"POST", "/api/kbs", true},
		{"PATCH", "/api/kbs/笔记", true},
		{"DELETE", "/api/kbs/笔记", true},
		{"POST", "/api/kbs/笔记/reindex", true},
		{"POST", "/api/settings", true},
		{"POST", "/api/pause", true},
		{"POST", "/api/skill/install", true},
	}
	for _, c := range cases {
		r := httptest.NewRequest(c.method, c.path, nil)
		if got := shouldRecord(r); got != c.want {
			t.Errorf("shouldRecord(%s %s) = %v，想要 %v", c.method, c.path, got, c.want)
		}
	}
}

// TestHumanAction 守“人话”。漏一个分支就会在界面上露出一串原始路径。
func TestHumanAction(t *testing.T) {
	h := NewHandler(nil, "", "", HandlerOptions{})
	cases := []struct {
		path, query string
		status      int
		want        string
	}{
		{"/kb/笔记/search", "q=部署+回滚", 200, "在“笔记”里搜了“部署 回滚”"},
		// 路径里的中文是百分号编码进来的，必须解码后再显示。
		{"/kb/%E7%AC%94%E8%AE%B0/search", "q=deploy&q=rollback", 200, "在“笔记”里搜了“deploy rollback”"},
		{"/kb/笔记/entry/ops/runbook.md/section", "n=2", 200, "读了“ops/runbook.md”的第 2 节"},
		{"/kb/笔记/entry/ops/runbook.md/toc", "", 200, "看了“ops/runbook.md”的章节目录"},
		{"/kb/笔记/entry/ops/runbook.md", "", 200, "读了“ops/runbook.md”"},
		{"/kb/笔记/manifest", "", 200, "取了一次“笔记”的概览"},
		{"/kb/笔记/report", "", 200, "核对了“笔记”的收录结果"},
		{"/kb/笔记/check", "", 200, "做了入库前检查"},
		{"/kb", "", 200, "问有哪些知识库"},
		{"/skill.md", "", 200, "取了说明书"},
		{"/kb/笔记/search", "q=x", 401, "认证失败（访问密码不对）"},
		{"/api/kbs/x", "", 401, "认证失败（管理凭据不对）"},
		{"/kb/笔记/search", "q=x", 503, "知识库处于关闭状态"},
		{"/api/kbs/笔记", "", 403, "跨站或跨源请求，被拒绝"},
		{"/api/settings", "", 429, "失败次数太多，被临时挡住"},
	}
	for _, c := range cases {
		r := httptest.NewRequest("GET", c.path+"?"+c.query, nil)
		if got := h.humanAction(r, c.status); got != c.want {
			t.Errorf("%s（%d）= %q，想要 %q", c.path, c.status, got, c.want)
		}
	}

	// 暂停时 503 要说是暂停，而不是“知识库关闭”。
	h.SetPaused(true)
	r := httptest.NewRequest("GET", "/kb/笔记/search?q=x", nil)
	if got := h.humanAction(r, 503); got != "服务暂停期间被访问" {
		t.Errorf("暂停时的 503 = %q", got)
	}
}

// TestAccessLogEndpoint 走一遍全链路：搜一次 → 日志里有它，且带得上下文。
func TestAccessLogEndpoint(t *testing.T) {
	srv := newTestServer(t)

	// 先清空，免得起服务时那些建库请求混进来。
	if status, _, raw := adminReq(t, srv, http.MethodPost, "/api/logs/clear", nil); status != http.StatusOK {
		t.Fatalf("清空失败：%s", raw)
	}

	// 一次正常的检索。
	if status, _, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=%E9%83%A8%E7%BD%B2", nil); status != http.StatusOK {
		t.Fatalf("搜索失败：%s", raw)
	}
	// 轮询与静态资源都不该进日志。
	doRequest(t, srv, "/api/status", nil)
	doRequest(t, srv, "/api/kbs/%E7%AC%94%E8%AE%B0/index", nil)
	if resp, err := srv.Client().Get(srv.URL + "/assets/nope.js"); err == nil {
		resp.Body.Close()
	}
	// 不带密码访问读取面：401 也要记，这是排查“AI 连不上”的关键一行。
	if resp, err := srv.Client().Get(srv.URL + "/skill.md"); err == nil {
		resp.Body.Close()
	}

	status, body, raw := adminReq(t, srv, http.MethodGet, "/api/logs", nil)
	if status != http.StatusOK {
		t.Fatalf("读日志失败：%s", raw)
	}
	items, _ := body["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("应当只有两条（一次搜索、一次 401），实际 %d：%s", len(items), raw)
	}

	first := items[0].(map[string]any)
	if a, _ := first["action"].(string); !strings.Contains(a, "搜了") {
		t.Errorf("第一条应当是搜索，实际 %q", a)
	}
	if first["self"] != true {
		t.Errorf("本机请求要标记成 self：%v", first["self"])
	}
	if first["status"].(float64) != 200 {
		t.Errorf("搜索的状态码 = %v", first["status"])
	}
	if p, _ := first["path"].(string); !strings.Contains(p, "/search") {
		t.Errorf("path = %q", p)
	}

	second := items[1].(map[string]any)
	if a, _ := second["action"].(string); !strings.Contains(a, "认证失败") {
		t.Errorf("第二条应当是认证失败，实际 %q", a)
	}

	// since 增量：拿 latest 再问一次，应当什么都没有。
	latest := int64(body["latest"].(float64))
	_, body2, _ := adminReq(t, srv, http.MethodGet,
		fmt.Sprintf("/api/logs?since=%d", latest), nil)
	if got := body2["items"].([]any); len(got) != 0 {
		t.Errorf("since 之后不该有新条目，实际 %d", len(got))
	}

	// 清空
	if status, _, _ := adminReq(t, srv, http.MethodPost, "/api/logs/clear", nil); status != http.StatusOK {
		t.Fatalf("清空失败")
	}
	_, body3, _ := adminReq(t, srv, http.MethodGet, "/api/logs", nil)
	if got := body3["items"].([]any); len(got) != 0 {
		t.Errorf("清空之后应当为空，实际 %d", len(got))
	}
	if body3["total"].(float64) != 0 {
		t.Errorf("total = %v", body3["total"])
	}
}

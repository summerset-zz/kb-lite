package daemon

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

const (
	testToken      = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testAdminToken = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// testServerOpts 让每个测试只关心自己那一点：要不要读取密码、有哪些可选路由。
type testServerOpts struct {
	features     map[string]bool
	requireToken bool
}

// newTestServer 起一个真的 http.Server。
//
// 端口必须**先**定下来：Host 校验比的是 `HandlerOptions.Port`，用 httptest 的随机端口
// 会让所有请求都撞 421。所以先 Listen，再拿端口建 Handler。
func newTestServer(t *testing.T) *httptest.Server {
	return newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: true})
}

func newTestServerWithFeatures(t *testing.T, features map[string]bool) *httptest.Server {
	return newTestServerWith(t, testServerOpts{features: features, requireToken: true})
}

func newTestServerWith(t *testing.T, o testServerOpts) *httptest.Server {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ops", "runbook.md"),
		"---\ntitle: 运维手册\nsummary: 线上环境的操作手册。\ntags: [ops, 上线]\n---\n\n"+
			"线上环境的操作手册。\n\n"+
			"## 上线流程\n\n先打 tag，再跑 deploy 脚本。\n\n"+
			"## 回滚\n\n### 快速回滚\n\n回滚到上一个 tag。\n")

	mgr := NewManager(&Config{KBs: []KB{
		{ID: "k1", Name: "笔记", RootPath: root, Enabled: true},
	}}, BuildOptions{Workers: 1, ConfigDir: t.TempDir()})
	if err := mgr.Rebuild(); err != nil {
		t.Fatal(err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	h := NewHandler(mgr, testToken, testAdminToken, HandlerOptions{
		Port: port, Features: o.features, RequireToken: o.requireToken,
	})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: h.Routes()}}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv
}

func doRequest(t *testing.T, srv *httptest.Server, path string, mutate func(*http.Request)) (int, map[string]any, string) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, srv.URL+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	if mutate != nil {
		mutate(req)
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(body, &parsed)
	return resp.StatusCode, parsed, string(body)
}

func TestSearchEndpoint(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q="+"%E4%B8%8A%E7%BA%BF", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	results, _ := body["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("results = %v", body["results"])
	}
	first := results[0].(map[string]any)
	if first["path"] != "ops/runbook.md" {
		t.Errorf("path = %v", first["path"])
	}
	if first["rev"] == "" || first["rev"] == nil {
		t.Error("响应必须带 rev")
	}
	toc, _ := first["toc"].(string)
	if !strings.Contains(toc, "1 上线流程") {
		t.Errorf("toc = %q", toc)
	}
}

func TestSearchFormatJSON(t *testing.T) {
	srv := newTestServer(t)
	status, body, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF&format=json", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	results := body["results"].([]any)
	toc, ok := results[0].(map[string]any)["toc"].([]any)
	if !ok {
		t.Fatalf("format=json 时 toc 应当是数组，实际 %T", results[0].(map[string]any)["toc"])
	}
	if len(toc) == 0 {
		t.Error("toc 不该为空")
	}
}

func TestEntryEndpoint(t *testing.T) {
	srv := newTestServer(t)
	path := "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md"

	status, body, raw := doRequest(t, srv, path, nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["title"] != "运维手册" {
		t.Errorf("title = %v", body["title"])
	}
	if _, has := body["content"]; has {
		t.Error("默认不该返回正文")
	}

	_, body, _ = doRequest(t, srv, path+"?content=1", nil)
	content, _ := body["content"].(string)
	if !strings.Contains(content, "先打 tag") {
		t.Errorf("content = %q", content)
	}
	if strings.Contains(content, "title: 运维手册") {
		t.Error("content 应当已经去掉 frontmatter")
	}
}

func TestEntryByQueryPath(t *testing.T) {
	srv := newTestServer(t)
	// shell 友好的写法：路径放在 ?path= 里
	status, body, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry?path=ops%2Frunbook.md", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if body["path"] != "ops/runbook.md" {
		t.Errorf("path = %v", body["path"])
	}
}

func TestTOCEndpoint(t *testing.T) {
	srv := newTestServer(t)
	status, body, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md/toc", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	if !strings.Contains(body["toc"].(string), "2 回滚 · 空") {
		t.Errorf("toc = %q", body["toc"])
	}
	if int(body["sections"].(float64)) != 4 {
		t.Errorf("sections = %v", body["sections"])
	}
}

func TestSectionByNumber(t *testing.T) {
	srv := newTestServer(t)
	status, body, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md/section?n=1", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["sectionTitle"] != "上线流程" {
		t.Errorf("sectionTitle = %v", body["sectionTitle"])
	}
	if !strings.Contains(body["content"].(string), "先打 tag") {
		t.Errorf("content = %q", body["content"])
	}
}

func TestSectionFallbackToChild(t *testing.T) {
	srv := newTestServer(t)
	// 第 2 节「回滚」自己没有正文，正文在子节里 → 应当换到第 3 节并给 note。
	_, body, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md/section?n=2", nil)
	if int(body["section"].(float64)) != 3 {
		t.Fatalf("应当换到第 3 节，实际 %v：%s", body["section"], raw)
	}
	if !strings.HasPrefix(body["note"].(string), "第 2 节「回滚」自身没有正文") {
		t.Errorf("note = %q", body["note"])
	}
}

func TestSectionByTitle(t *testing.T) {
	srv := newTestServer(t)
	_, body, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md/section?title="+"%E5%BF%AB%E9%80%9F%E5%9B%9E%E6%BB%9A", nil)
	if int(body["section"].(float64)) != 3 {
		t.Fatalf("按标题定位失败：%s", raw)
	}
}

func TestSectionMissReturnsCandidates(t *testing.T) {
	srv := newTestServer(t)
	status, body, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md/section?title=%E4%B8%8D%E5%AD%98%E5%9C%A8", nil)

	if status != http.StatusOK {
		t.Fatalf("必须是 200，不能是 4xx（ADR-010），实际 %d：%s", status, raw)
	}
	if body["ok"] != false {
		t.Errorf("ok = %v，想要 false", body["ok"])
	}
	cands, ok := body["candidates"].([]any)
	if !ok || len(cands) != 4 {
		t.Fatalf("candidates = %v", body["candidates"])
	}
	first := cands[0].(map[string]any)
	if first["sectionTitle"] != "导言" {
		t.Errorf("第一条 = %v", first)
	}
}

func TestNearbyEndpoint(t *testing.T) {
	srv := newTestServer(t)
	status, body, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md/nearby?n=2", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d", status)
	}
	items, _ := body["items"].([]any)
	if len(items) != 3 {
		t.Fatalf("items = %d，想要 3", len(items))
	}

	if status, _, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md/nearby?n=2&radius=2", nil); status != http.StatusBadRequest {
		t.Errorf("radius=2 应当被拒，实际 %d", status)
	}
}

func TestSkillEndpoint(t *testing.T) {
	srv := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/skill.md", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("Content-Type = %q", ct)
	}
	if !strings.Contains(string(body), testToken) {
		t.Error("skill.md 里应当带着 token（用户要整段粘给 agent）")
	}
	if !strings.Contains(string(body), "- 笔记（1 篇") {
		t.Errorf("KB 清单没渲染出来")
	}
	// v0.2 的 manifest 已经实装，所以文档里必须有它（路由与文档同源，见 skill.go）。
	if !strings.Contains(string(body), "/manifest") {
		t.Error("manifest 已经实现，skill.md 里不该缺这一段")
	}
}

func TestUnauthorized(t *testing.T) {
	srv := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/kb/%E7%AC%94%E8%AE%B0/search?q=x", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	var body errorResponse
	_ = json.NewDecoder(resp.Body).Decode(&body)
	if body.Error.Code != "unauthorized" {
		t.Errorf("code = %q", body.Error.Code)
	}
}

func TestHostValidation(t *testing.T) {
	srv := newTestServer(t)
	status, body, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=x", func(r *http.Request) {
		r.Host = "evil.example"
	})
	if status != http.StatusMisdirectedRequest {
		t.Fatalf("status = %d，想要 421", status)
	}
	if body["error"].(map[string]any)["code"] != "bad_host" {
		t.Errorf("body = %v", body)
	}
}

func TestCrossSiteRejected(t *testing.T) {
	srv := newTestServer(t)
	status, _, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=x", func(r *http.Request) {
		r.Header.Set("Sec-Fetch-Site", "cross-site")
	})
	if status != http.StatusForbidden {
		t.Errorf("status = %d，想要 403", status)
	}

	status, _, _ = doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=x", func(r *http.Request) {
		r.Header.Set("Origin", "http://evil.example")
	})
	if status != http.StatusForbidden {
		t.Errorf("跨源 Origin status = %d，想要 403", status)
	}
}

func TestNoCORSHeaders(t *testing.T) {
	srv := newTestServer(t)
	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/skill.md", nil)
	req.Header.Set("Authorization", "Bearer "+testToken)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	for name := range resp.Header {
		if strings.HasPrefix(strings.ToLower(name), "access-control-") {
			t.Errorf("不该发 %s 头（docs/07 第 3 条）", name)
		}
	}
	if resp.Header.Get("Cache-Control") != "no-store" {
		t.Errorf("Cache-Control = %q", resp.Header.Get("Cache-Control"))
	}
}

func TestAuthThrottle(t *testing.T) {
	srv := newTestServer(t)

	bad := func() int {
		req, _ := http.NewRequest(http.MethodGet, srv.URL+"/skill.md", nil)
		req.Header.Set("Authorization", "Bearer wrong")
		resp, err := srv.Client().Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}

	var last int
	for i := 0; i < 10; i++ {
		last = bad()
	}
	if last != http.StatusTooManyRequests {
		t.Errorf("第 10 次失败应当被节流成 429，实际 %d", last)
	}
}

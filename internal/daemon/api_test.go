package daemon

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// adminReq 发一次管理面请求：带 admin 凭据与同源信号（CLI 就是这么调的）。
func adminReq(t *testing.T, srv *httptest.Server, method, path string, body any) (int, map[string]any, string) {
	t.Helper()

	var rd *bytes.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, srv.URL+path, rd)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-KB-Admin", testAdminToken)
	req.Header.Set("Origin", srv.URL)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	_ = json.Unmarshal(buf.Bytes(), &parsed)
	return resp.StatusCode, parsed, buf.String()
}

// waitIndexed 等到某个 KB 不在整理中。
func waitIndexed(t *testing.T, srv *httptest.Server, name string) map[string]any {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		_, body, _ := adminReq(t, srv, http.MethodGet, "/api/kbs/"+name+"/index", nil)
		if body["state"] != "indexing" {
			return body
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("等 %s 整理完超时", name)
	return nil
}

func TestKBCreateIndexAndSearch(t *testing.T) {
	srv := newTestServer(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "ops", "runbook.md"),
		"---\ntitle: 部署手册\n---\n\n## 上线\n\n先打 tag 再发布。\n")

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/kbs", kbWriteReq{
		Name: "新库", RootPath: root,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["state"] != "indexing" {
		t.Errorf("新建后应当立刻进 indexing：%v", body["state"])
	}
	if body["warning"] != nil {
		t.Errorf("目录里有 md，不该有 warning：%v", body["warning"])
	}

	waitIndexed(t, srv, "新库")

	// 建完就能搜到（走读取面，不需要 admin）。
	status, _, raw = doRequest(t, srv, "/kb/%E6%96%B0%E5%BA%93/search?q=%E9%83%A8%E7%BD%B2", nil)
	if status != http.StatusOK {
		t.Fatalf("搜索 status = %d：%s", status, raw)
	}
	if !bytes.Contains([]byte(raw), []byte("部署手册")) {
		t.Errorf("新建的库搜不到内容：%s", raw)
	}
}

func TestKBCreateValidation(t *testing.T) {
	srv := newTestServer(t)
	root := t.TempDir()

	cases := []struct {
		name string
		req  kbWriteReq
		code int
	}{
		{"重名", kbWriteReq{Name: "笔记", RootPath: root}, http.StatusConflict},
		{"名字带斜杠", kbWriteReq{Name: "a/b", RootPath: root}, http.StatusBadRequest},
		{"名字超长", kbWriteReq{Name: string(make([]rune, 65)), RootPath: root}, http.StatusBadRequest},
		{"目录不存在", kbWriteReq{Name: "缺目录", RootPath: filepath.Join(root, "nope")}, http.StatusBadRequest},
		{"空名字", kbWriteReq{Name: "", RootPath: root}, http.StatusBadRequest},
	}
	for _, c := range cases {
		status, _, raw := adminReq(t, srv, http.MethodPost, "/api/kbs", c.req)
		if status != c.code {
			t.Errorf("%s：status = %d，想要 %d（%s）", c.name, status, c.code, raw)
		}
	}
}

func TestKBCreateWarnsWhenNoMarkdown(t *testing.T) {
	srv := newTestServer(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.png"), "not markdown")

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/kbs", kbWriteReq{
		Name: "空库", RootPath: root,
	})
	if status != http.StatusCreated {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["warning"] == nil {
		t.Error("目录里没有 markdown 时应当带 warning，但仍然创建成功")
	}
	waitIndexed(t, srv, "空库")
}

func TestKBRenameKeepsContent(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := adminReq(t, srv, http.MethodPatch, "/api/kbs/%E7%AC%94%E8%AE%B0",
		KBPatch{Name: strPtr("改过的名字")})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["name"] != "改过的名字" {
		t.Errorf("name = %v", body["name"])
	}

	// 新名字可用，旧名字 404。
	status, _, _ = doRequest(t, srv, "/kb/%E6%94%B9%E8%BF%87%E7%9A%84%E5%90%8D%E5%AD%97/entry/ops/runbook.md/toc", nil)
	if status != http.StatusOK {
		t.Errorf("改名后新名字应当可用，实际 %d", status)
	}
	status, _, _ = doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/entry/ops/runbook.md/toc", nil)
	if status != http.StatusNotFound {
		t.Errorf("旧名字应当 404，实际 %d", status)
	}
}

func TestKBDisableThenEnable(t *testing.T) {
	srv := newTestServer(t)

	status, _, raw := adminReq(t, srv, http.MethodPost, "/api/kbs/%E7%AC%94%E8%AE%B0/disable", nil)
	if status != http.StatusOK {
		t.Fatalf("disable status = %d：%s", status, raw)
	}
	status, body, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=x", nil)
	if status != http.StatusServiceUnavailable {
		t.Errorf("禁用后检索 status = %d，想要 503", status)
	}
	if code, _ := body["error"].(map[string]any)["code"].(string); code != "kb_disabled" {
		t.Errorf("code = %v", code)
	}

	// 重新启用：索引还在，不需要重切。
	status, _, raw = adminReq(t, srv, http.MethodPost, "/api/kbs/%E7%AC%94%E8%AE%B0/enable", nil)
	if status != http.StatusOK {
		t.Fatalf("enable status = %d：%s", status, raw)
	}
	status, _, _ = doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF", nil)
	if status != http.StatusOK {
		t.Errorf("重新启用后检索 status = %d，想要 200", status)
	}
}

func TestKBReindexAndConflict(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/kbs/%E7%AC%94%E8%AE%B0/reindex", reindexReq{})
	if status != http.StatusAccepted {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["state"] != "indexing" {
		t.Errorf("state = %v", body["state"])
	}

	// 立刻再发一次：要么 409（还在跑），要么 202（跑得很快，已经结束）。
	status, body, raw = adminReq(t, srv, http.MethodPost, "/api/kbs/%E7%AC%94%E8%AE%B0/reindex", reindexReq{})
	if status == http.StatusConflict {
		errObj, _ := body["error"].(map[string]any)
		if errObj["code"] != "indexing" {
			t.Errorf("409 的 code = %v", errObj["code"])
		}
		if _, ok := errObj["progress"]; !ok {
			t.Errorf("409 应当带 progress：%s", raw)
		}
	} else if status != http.StatusAccepted {
		t.Fatalf("第二次 status = %d：%s", status, raw)
	}

	waitIndexed(t, srv, "笔记")
}

func TestKBDeleteDoesNotTouchFiles(t *testing.T) {
	srv := newTestServer(t)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "内容")
	adminReq(t, srv, http.MethodPost, "/api/kbs", kbWriteReq{Name: "要删的", RootPath: root})
	waitIndexed(t, srv, "要删的")

	before := dirDigest(t, root)

	status, body, raw := adminReq(t, srv, http.MethodDelete, "/api/kbs/%E8%A6%81%E5%88%A0%E7%9A%84", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	removed, _ := body["removed"].(map[string]any)
	if removed["entries"].(float64) != 1 {
		t.Errorf("removed.entries = %v，想要 1", removed["entries"])
	}

	if after := dirDigest(t, root); after != before {
		t.Error("删除知识库动了用户的文件，这绝对不行")
	}

	// 已经从列表里消失。
	_, body, _ = adminReq(t, srv, http.MethodGet, "/api/kbs", nil)
	kbs, _ := body["kbs"].([]any)
	for _, item := range kbs {
		if item.(map[string]any)["name"] == "要删的" {
			t.Error("删掉的知识库还在列表里")
		}
	}
}

func TestFSList(t *testing.T) {
	srv := newTestServer(t)
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "有笔记", "a.md"), "x")
	writeFile(t, filepath.Join(root, "空的", "b.png"), "x")
	writeFile(t, filepath.Join(root, ".git", "c.md"), "x")
	writeFile(t, filepath.Join(root, "node_modules", "d.md"), "x")

	status, body, raw := adminReq(t, srv, http.MethodGet, "/api/fs/list?path="+url.QueryEscape(root), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	dirs, _ := body["dirs"].([]any)
	names := map[string]map[string]any{}
	for _, d := range dirs {
		m := d.(map[string]any)
		names[m["name"].(string)] = m
	}
	if _, ok := names[".git"]; ok {
		t.Error(".git 不该出现在候选里")
	}
	if _, ok := names["node_modules"]; ok {
		t.Error("node_modules 不该出现在候选里")
	}
	if got := names["有笔记"]["markdownCount"].(float64); got != 1 {
		t.Errorf("有笔记 的 markdownCount = %v，想要 1", got)
	}
	if names["有笔记"]["hasMarkdown"] != true {
		t.Error("有笔记 应当 hasMarkdown")
	}
	if names["空的"]["hasMarkdown"] != false {
		t.Error("空的 不该 hasMarkdown")
	}

	// 路径不存在：400，不是 500。
	status, _, _ = adminReq(t, srv, http.MethodGet, "/api/fs/list?path="+url.QueryEscape(filepath.Join(root, "nope")), nil)
	if status != http.StatusBadRequest {
		t.Errorf("不存在的目录 status = %d，想要 400", status)
	}
}

// TestFSListWithoutPath 守目录选择器的第一屏：不给 path 也要有东西可选。
//
// 早先这种情况回 400「缺少 path 参数」，界面上的表现是点了「选一个文件夹」什么都不出来，
// 下面还挂着一句用户根本看不懂的话。
func TestFSListWithoutPath(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := adminReq(t, srv, http.MethodGet, "/api/fs/list", nil)
	if status != http.StatusOK {
		t.Fatalf("不带 path 时 status = %d：%s", status, raw)
	}
	// Windows 给盘符，其它平台给家目录：两种都算给了起点。
	drives, _ := body["drives"].([]any)
	path, _ := body["path"].(string)
	if len(drives) == 0 && path == "" {
		t.Fatalf("既没有盘符也没有目录，界面还是空的：%s", raw)
	}
	if _, ok := body["dirs"].([]any); !ok {
		t.Errorf("dirs 应当是数组（哪怕是空的）：%s", raw)
	}
}

// ---- 小工具 ----

func strPtr(s string) *string { return &s }

// dirDigest 把目录下所有文件的 md5 拼在一起，用来断言“一个字节都没动”。
func dirDigest(t *testing.T, root string) string {
	t.Helper()
	h := md5.New()
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		h.Write([]byte(p))
		h.Write(b)
		return nil
	})
	return string(h.Sum(nil))
}

package daemon

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestKBReport 守“这次收录了什么、漏了什么”。
//
// 整理侧写完文件要让用户点一次「更新」，然后它得能自己核对：
// 总数变没变、自己写的那几篇在不在、没进去的是为什么。
func TestKBReport(t *testing.T) {
	srv := newTestServer(t)

	// 造一个会被跳过 + 一个会出告警的库，然后重新整理。
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "好的.md"), "# 标题\n\n正文。\n")
	// 认不出编码：整篇跳过，理由要能被 agent 读到。
	if err := os.WriteFile(filepath.Join(root, "坏.md"), []byte("\xff\xfe\x80\x81\x82"), 0o644); err != nil {
		t.Fatal(err)
	}
	// frontmatter 要求跳过。
	writeFile(t, filepath.Join(root, "草稿.md"), "---\ntitle: 草稿\nsummary: 还没写完。\ndraft: true\n---\n\n正文。\n")

	status, _, raw := adminReq(t, srv, http.MethodPost, "/api/kbs",
		kbWriteReq{Name: "报告库", RootPath: root})
	if status != http.StatusCreated {
		t.Fatalf("建库失败：%s", raw)
	}
	waitIndexed(t, srv, "报告库")

	status, body, raw := doRequest(t, srv, "/kb/"+url.PathEscape("报告库")+"/report", nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["name"] != "报告库" {
		t.Errorf("name = %v", body["name"])
	}
	if _, ok := body["updatedAt"].(float64); !ok {
		t.Errorf("应当带 updatedAt（agent 靠它判断刷新了没）：%s", raw)
	}
	if got := body["entries"].(float64); got != 1 {
		t.Errorf("entries = %v，想要 1", got)
	}

	skipped, _ := body["skipped"].([]any)
	if len(skipped) != 2 {
		t.Fatalf("skipped 应当有 2 条：%s", raw)
	}
	reasons := map[string]string{}
	for _, it := range skipped {
		m := it.(map[string]any)
		reasons[m["path"].(string)] = m["reason"].(string)
	}
	if reason, ok := reasons["坏.md"]; !ok || !strings.Contains(reason, "编码") {
		t.Errorf("坏.md 的理由应当是编码问题，实际 %q（%v）", reason, reasons)
	}
	if _, ok := reasons["草稿.md"]; !ok {
		t.Errorf("草稿.md 也该出现在 skipped 里：%v", reasons)
	}
	if body["skippedTotal"].(float64) != 2 {
		t.Errorf("skippedTotal = %v", body["skippedTotal"])
	}

	// 别的库不存在：404，不是 500。
	status, _, _ = doRequest(t, srv, "/kb/%E6%B2%A1%E6%9C%89/report", nil)
	if status != http.StatusNotFound {
		t.Errorf("不存在的库 status = %d，想要 404", status)
	}

	// 界面走管理面这条（它拿不到读取密码）：同一份数据，字段一致。
	status, adminBody, raw := adminReq(t, srv, http.MethodGet,
		"/api/kbs/"+url.PathEscape("报告库")+"/report", nil)
	if status != http.StatusOK {
		t.Fatalf("管理面 report status = %d：%s", status, raw)
	}
	if adminBody["skippedTotal"].(float64) != 2 || adminBody["entries"].(float64) != 1 {
		t.Errorf("管理面 report 与读取面不一致：%s", raw)
	}

	// 建了但还没整理过的库：空报告，不是 404（那一行的「查看入库日志」照样能点）。
	status, _, raw = adminReq(t, srv, http.MethodPost, "/api/kbs",
		kbWriteReq{Name: "空报告库", RootPath: t.TempDir()})
	if status != http.StatusCreated {
		t.Fatalf("建库失败：%s", raw)
	}
	waitIndexed(t, srv, "空报告库")
	status, emptyBody, raw := adminReq(t, srv, http.MethodGet,
		"/api/kbs/"+url.PathEscape("空报告库")+"/report", nil)
	if status != http.StatusOK {
		t.Fatalf("空库 report status = %d：%s", status, raw)
	}
	if emptyBody["skippedTotal"].(float64) != 0 {
		t.Errorf("空库不该有 skipped：%s", raw)
	}

	// /api/status 的 notIndexed 要与“真的没进库”对上：
	// 坏.md（编码）+ 草稿.md（draft）两篇，frontmatter 不规范不算。
	_, st, raw := adminReq(t, srv, http.MethodGet, "/api/status", nil)
	kbs, _ := st["kbs"].([]any)
	var notIndexed float64 = -1
	for _, it := range kbs {
		m := it.(map[string]any)
		if m["name"] == "报告库" {
			notIndexed = m["notIndexed"].(float64)
		}
	}
	if notIndexed != 2 {
		t.Errorf("notIndexed = %v，想要 2：%s", notIndexed, raw)
	}
}

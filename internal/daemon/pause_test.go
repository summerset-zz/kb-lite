package daemon

import (
	"net/http"
	"testing"
)

// 暂停只挡给 AI 的那一面：界面与 /api/* 必须还能用，否则用户点不到“继续使用”，
// `kb-lite start` 也没有可以唤醒它的通道（docs/04）。
func TestPauseBlocksOnlyReadSurface(t *testing.T) {
	srv := newTestServer(t) // requireToken = true

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/pause", nil)
	if status != http.StatusOK {
		t.Fatalf("暂停失败：status = %d：%s", status, raw)
	}
	if body["paused"] != true {
		t.Fatalf("paused = %v", body["paused"])
	}

	const searchPath = "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF"

	// 带对了访问密码也照样 503：暂停不是鉴权问题。
	code, parsed, raw := doRequest(t, srv, searchPath, nil)
	if code != http.StatusServiceUnavailable {
		t.Fatalf("暂停后检索应当 503，实际 %d：%s", code, raw)
	}
	if errBox, _ := parsed["error"].(map[string]any); errBox["code"] != "paused" {
		t.Errorf("error.code = %v，想要 paused", errBox["code"])
	}

	// /skill.md 与 /authoring.md 同样停。
	for _, p := range []string{"/skill.md", "/authoring.md"} {
		if code, _, _ := doRequest(t, srv, p, nil); code != http.StatusServiceUnavailable {
			t.Errorf("%s 暂停后应当 503，实际 %d", p, code)
		}
	}

	// 管理面照常，并且 status 里能读到 paused。
	code, st, raw := adminReq(t, srv, http.MethodGet, "/api/status", nil)
	if code != http.StatusOK {
		t.Fatalf("/api/status 在暂停时也必须可用，实际 %d：%s", code, raw)
	}
	if st["paused"] != true {
		t.Errorf("status.paused = %v，想要 true", st["paused"])
	}

	// UI 也得能打开，不然“继续使用”按钮没人送得到。
	code, _, raw = doRequest(t, srv, "/", nil)
	if code != http.StatusOK {
		t.Fatalf("暂停时 UI 必须还能打开，实际 %d：%s", code, raw)
	}

	// 恢复。
	if status, _, raw := adminReq(t, srv, http.MethodPost, "/api/resume", nil); status != http.StatusOK {
		t.Fatalf("恢复失败：%d：%s", status, raw)
	}
	code, _, raw = doRequest(t, srv, searchPath, nil)
	if code != http.StatusOK {
		t.Fatalf("恢复后检索应当能用，实际 %d：%s", code, raw)
	}
}

// 暂停不影响索引：恢复之后还是同一份结果（docs/06 第二节“不重新整理”）。
func TestPauseKeepsIndex(t *testing.T) {
	srv := newTestServer(t)
	const searchPath = "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF"

	_, before, _ := doRequest(t, srv, searchPath, nil)
	adminReq(t, srv, http.MethodPost, "/api/pause", nil)
	adminReq(t, srv, http.MethodPost, "/api/resume", nil)
	_, after, _ := doRequest(t, srv, searchPath, nil)

	b, _ := before["results"].([]any)
	a, _ := after["results"].([]any)
	if len(a) != len(b) || len(a) == 0 {
		t.Fatalf("恢复后结果条数变了：%d → %d", len(b), len(a))
	}
	first, _ := a[0].(map[string]any)
	if first["rev"] != before["results"].([]any)[0].(map[string]any)["rev"] {
		t.Error("恢复后 rev 变了，说明被重新切了一遍")
	}
}

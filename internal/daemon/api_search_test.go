package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// 管理面的检索必须与 agent 面**同源**：搜索调试页给用户看的分数，
// 必须就是 agent 拿到的那个分数，否则那页只会误导人。
func TestAdminSearchMatchesAgentSearch(t *testing.T) {
	srv := newTestServer(t)

	const kbName = "%E7%AC%94%E8%AE%B0" // 笔记
	_, agentBody, agentRaw := doRequest(t, srv, "/kb/"+kbName+"/search?q=%E4%B8%8A%E7%BA%BF&format=json&explain=1", nil)

	status, adminBody, adminRaw := adminReq(t, srv, http.MethodPost, "/api/search", map[string]any{
		"kb": "笔记",
		"q":  []string{"上线"},
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, adminRaw)
	}

	// 分数、路径、rev 必须逐条一致。
	agentHits, _ := agentBody["results"].([]any)
	adminHits, _ := adminBody["results"].([]any)
	if len(agentHits) != len(adminHits) || len(agentHits) == 0 {
		t.Fatalf("条数不一致：agent %d / admin %d\n%s", len(agentHits), len(adminHits), agentRaw)
	}
	for i := range agentHits {
		a := agentHits[i].(map[string]any)
		b := adminHits[i].(map[string]any)
		if a["score"] != b["score"] || a["path"] != b["path"] || a["rev"] != b["rev"] {
			t.Errorf("第 %d 条不一致：agent %v / admin %v", i+1, a, b)
		}
	}

	// 解释数据也要在。
	debug, _ := adminBody["debug"].([]any)
	if len(debug) == 0 {
		t.Fatal("管理面检索必须带 debug（搜索调试页就是靠它）")
	}
	row := debug[0].(map[string]any)
	if row["path"] == nil {
		t.Errorf("debug 行缺少 path：%v", row)
	}
}

// 没有这个知识库时给 404，而不是空结果（空结果会让用户以为“搜不到”）。
func TestAdminSearchUnknownKB(t *testing.T) {
	srv := newTestServer(t)
	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/search", map[string]any{
		"kb": "不存在的库",
		"q":  []string{"上线"},
	})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if errBox, _ := body["error"].(map[string]any); errBox["code"] != "kb_not_found" {
		t.Errorf("error.code = %v", errBox["code"])
	}
}

// 空查询要 400：否则用户会看到一个“没有结果”，以为是知识库里没有。
func TestAdminSearchRequiresQuery(t *testing.T) {
	srv := newTestServer(t)
	status, _, raw := adminReq(t, srv, http.MethodPost, "/api/search", map[string]any{
		"kb": "笔记",
		"q":  []string{"   "},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if !strings.Contains(raw, "至少给一个 q") {
		t.Errorf("文案不对：%s", raw)
	}
}

// 管理面检索同样要凭据：它不在读取面上，但也不是敞开的。
func TestAdminSearchNeedsAdmin(t *testing.T) {
	srv := newTestServer(t)

	// 只带读取密码不够。
	status, _, raw := doRequest(t, srv, "/api/search", func(r *http.Request) {
		r.Method = http.MethodPost
		r.Body = nopCloser(`{"kb":"笔记","q":["上线"]}`)
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("status = %d：%s", status, raw)
	}
}

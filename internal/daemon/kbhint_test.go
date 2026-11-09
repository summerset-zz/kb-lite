package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// `/kb` 没有这个路由：它必须给一条能读懂的 404，
// 不能落到 UI 兜底，否则 agent 会拿到一页 HTML 以为接口通了。
func TestKBWithoutNameIsHelpful404(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := doRequest(t, srv, "/kb", nil)
	if status != http.StatusNotFound {
		t.Fatalf("status = %d：%s", status, raw)
	}
	errBox, _ := body["error"].(map[string]any)
	if errBox["code"] != "kb_name_required" {
		t.Errorf("error.code = %v", errBox["code"])
	}
	msg, _ := errBox["message"].(string)
	if !strings.Contains(msg, "/skill.md") {
		t.Errorf("错误文案应当指向 /skill.md：%q", msg)
	}
	if ct := strings.ToLower(strings.SplitN(msg, ";", 2)[0]); strings.Contains(ct, "<html") {
		t.Errorf("不能回 HTML：%q", msg)
	}
}

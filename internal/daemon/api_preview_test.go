package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// preview 用服务端自己的读取密码代取，所以拿到的就是“带对了密码的 agent”看到的东西。
func TestPreviewSkill(t *testing.T) {
	srv := newTestServer(t) // requireToken = true

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/preview", map[string]any{
		"kind": "skill",
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["status"] != float64(http.StatusOK) {
		t.Errorf("内层 status = %v，应当是 200", body["status"])
	}
	if ct, _ := body["contentType"].(string); !strings.HasPrefix(ct, "text/markdown") {
		t.Errorf("contentType = %q", ct)
	}
	inner, _ := body["body"].(string)
	if !strings.Contains(inner, "kb-lite") {
		t.Errorf("拿回来的不是 skill.md：%s", inner)
	}
	// 服务端代发时会带上读取密码，所以文档里应该有 Authorization 那一行。
	if !strings.Contains(inner, "Authorization") {
		t.Error("开着访问密码时，skill.md 里应当带上鉴权头")
	}
}

// 白名单之外的一律拒绝：这个接口绝不能变成任意 URL 代理。
func TestPreviewRejectsUnknownKind(t *testing.T) {
	srv := newTestServer(t)

	status, body, _ := adminReq(t, srv, http.MethodPost, "/api/preview", map[string]any{
		"kind": "http://example.com/",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d", status)
	}
	if errBox, _ := body["error"].(map[string]any); errBox["code"] != "bad_kind" {
		t.Errorf("error.code = %v", errBox["code"])
	}
}

// 路径穿越要挡在发请求之前。
func TestPreviewRejectsBadPath(t *testing.T) {
	srv := newTestServer(t)

	for _, p := range []string{"../etc/passwd", "/abs/path.md", ""} {
		status, _, raw := adminReq(t, srv, http.MethodPost, "/api/preview", map[string]any{
			"kind": "section", "kb": "笔记", "path": p,
		})
		if status != http.StatusBadRequest {
			t.Errorf("path=%q 应当 400，实际 %d：%s", p, status, raw)
		}
	}
}

// 没有这个知识库给 404，而不是空内容。
func TestPreviewUnknownKB(t *testing.T) {
	srv := newTestServer(t)
	status, _, _ := adminReq(t, srv, http.MethodPost, "/api/preview", map[string]any{
		"kind": "manifest", "kb": "不存在的库",
	})
	if status != http.StatusNotFound {
		t.Fatalf("status = %d", status)
	}
}

// 检索预览要带查询词。
func TestPreviewSearchNeedsQuery(t *testing.T) {
	srv := newTestServer(t)
	status, _, _ := adminReq(t, srv, http.MethodPost, "/api/preview", map[string]any{
		"kind": "search", "kb": "笔记", "q": "  ",
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d", status)
	}
}

// 正常的 section 预览要真的取到那一节。
func TestPreviewSection(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/preview", map[string]any{
		"kind": "section", "kb": "笔记", "path": "ops/runbook.md", "n": 1,
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	inner, _ := body["body"].(string)
	if !strings.Contains(inner, "上线流程") {
		t.Errorf("没取到第一节：%s", inner)
	}
}

// 暂停时代取也要看到 503：这正是这一页的价值，它让人看见 AI 此刻收到什么。
func TestPreviewSeesPaused(t *testing.T) {
	srv := newTestServer(t)
	adminReq(t, srv, http.MethodPost, "/api/pause", nil)
	defer adminReq(t, srv, http.MethodPost, "/api/resume", nil)

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/preview", map[string]any{
		"kind": "skill",
	})
	if status != http.StatusOK {
		t.Fatalf("外层应当 200（代取本身成功了）：%d：%s", status, raw)
	}
	if body["status"] != float64(http.StatusServiceUnavailable) {
		t.Errorf("内层 status = %v，暂停时应当是 503", body["status"])
	}
	inner, _ := body["body"].(string)
	if !strings.Contains(inner, "paused") {
		t.Errorf("内层应当是 paused 的 JSON：%s", inner)
	}
}

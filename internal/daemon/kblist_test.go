package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// TestKBListEndpoint 是 agent 的入口：现在有哪些库、各自在磁盘上哪个目录。
//
// 这个接口存在的理由要写清楚：说明书（SKILL.md）会被装到用户目录里，不会随配置更新，
// 所以库列表不能写进说明书，只能让 agent 每次自己取。
func TestKBListEndpoint(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := doRequest(t, srv, "/kb", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+testToken)
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	kbs, ok := body["kbs"].([]any)
	if !ok || len(kbs) == 0 {
		t.Fatalf("应当列出至少一个知识库：%s", raw)
	}
	first := kbs[0].(map[string]any)
	if first["name"] != "笔记" {
		t.Errorf("name = %v", first["name"])
	}
	// 整理侧（写文件）要用这个路径，所以它必须在响应里。
	if root, _ := first["rootPath"].(string); root == "" {
		t.Errorf("rootPath 不能为空：%s", raw)
	}
	if _, ok := first["entries"].(float64); !ok {
		t.Errorf("entries 应当是数字：%s", raw)
	}
}

// 说明书里不再写死库列表，但必须告诉 agent 去哪取。
func TestSkillPointsAtKBList(t *testing.T) {
	srv := newTestServer(t)
	status, _, raw := doRequest(t, srv, "/skill.md", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+testToken)
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if !strings.Contains(raw, "先看有哪些知识库") {
		t.Error("说明书里应当有一段“先看有哪些知识库”")
	}
}

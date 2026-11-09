package daemon

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestAuthTokenShow 验设置页拿得到当前读取密码，且就是服务端实际在用的那把。
func TestAuthTokenShow(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/auth/token", map[string]any{"action": "show"})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["token"] != testToken {
		t.Fatalf("token = %v，应该是服务端在用的那把", body["token"])
	}
}

// TestAuthTokenRotate 验换密码之后旧密码立刻失效、新密码立刻可用、文件也落了盘。
func TestAuthTokenRotate(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "正文。\n")

	mgr := NewManager(&Config{KBs: []KB{{ID: "k1", Name: "k", RootPath: root, Enabled: true}}},
		BuildOptions{Workers: 1, ConfigDir: dir})
	if err := mgr.Rebuild(); err != nil {
		t.Fatal(err)
	}
	srv := newServerFor(t, mgr, testServerOpts{requireToken: true})
	t.Cleanup(srv.Close)

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/auth/token", map[string]any{"action": "rotate"})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	fresh, _ := body["token"].(string)
	if fresh == "" || fresh == testToken {
		t.Fatalf("换出来的 token 不对：%q", fresh)
	}

	// 旧密码立刻不能用了。
	if code, _, _ := doRequest(t, srv, "/kb/k/search?q=%E6%AD%A3%E6%96%87", nil); code != http.StatusUnauthorized {
		t.Errorf("旧密码应当失效，实际 status = %d", code)
	}
	// 新密码能用。
	code, _, raw := doRequest(t, srv, "/kb/k/search?q=%E6%AD%A3%E6%96%87", func(r *http.Request) {
		r.Header.Set("Authorization", "Bearer "+fresh)
	})
	if code != http.StatusOK {
		t.Errorf("新密码应该能用，status = %d：%s", code, raw)
	}

	// 文件也要跟着变，否则重启会退回旧密码。
	b, err := os.ReadFile(filepath.Join(dir, ReadTokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(string(b)); got != fresh {
		t.Errorf("%s 里是 %q，应该是 %q", ReadTokenFileName, got, fresh)
	}
}

// TestAuthTokenBadAction 验未知动作给 400，而不是默默成功。
func TestAuthTokenBadAction(t *testing.T) {
	srv := newTestServer(t)
	status, _, raw := adminReq(t, srv, http.MethodPost, "/api/auth/token", map[string]any{"action": "drop"})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d：%s", status, raw)
	}
}

// TestAuthoringDoc 验 `/authoring.md` 与 `/skill.md` 是两份不同的文档（ADR-017）。
func TestAuthoringDoc(t *testing.T) {
	srv := newTestServer(t)

	code, _, authoring := doRequest(t, srv, "/authoring.md", nil)
	if code != http.StatusOK {
		t.Fatalf("/authoring.md status = %d", code)
	}
	_, _, skill := doRequest(t, srv, "/skill.md", nil)
	if authoring == skill {
		t.Fatal("两份文档内容相同，说明被合并了")
	}
	if !strings.Contains(authoring, "整理成") {
		t.Errorf("/authoring.md 应该是教“怎么写”的那份，内容看起来不对")
	}
}

// TestAuthoringRequiresToken 验它和 /skill.md 一样受访问密码约束。
func TestAuthoringRequiresToken(t *testing.T) {
	srv := newTestServer(t)

	code, _, _ := doRequest(t, srv, "/authoring.md", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if code != http.StatusUnauthorized {
		t.Fatalf("没带密码时 status = %d，应该是 401", code)
	}
}

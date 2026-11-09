package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// freePort 拿一个当前没被占的端口号，然后立刻放掉（给“改端口”的测试用）。
func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// startTestServer 起一个真的 daemon.Server（带热重开），返回它和当前端口。
func startTestServer(t *testing.T) (*Server, int) {
	t.Helper()

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "# 甲\n\n正文。\n")

	mgr := NewManager(&Config{
		Host: DefaultHost,
		KBs:  []KB{{ID: "k1", Name: "k", RootPath: root, Enabled: true}},
	}, BuildOptions{Workers: 1, ConfigDir: t.TempDir()})
	if err := mgr.Rebuild(); err != nil {
		t.Fatal(err)
	}

	srv := NewServer(mgr, testToken, testAdminToken, HandlerOptions{
		Features:     DefaultFeatures,
		RequireToken: false,
	})
	port, err := srv.Start(DefaultHost, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(srv.Shutdown)
	return srv, port
}

// testClient 是直连的客户端（不走系统代理）。
//
// 这台机器上可能配了 HTTP_PROXY：默认 transport 会把请求发给代理，
// 于是“端口已经关了”也会得到一个响应，“连不上”就验不出来了。
func testClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{Proxy: nil},
	}
}

// postSettings 用新端口发一次管理请求，返回状态码与解析后的响应体。
func postSettings(t *testing.T, port int, body map[string]any) (int, map[string]any) {
	t.Helper()

	url := fmt.Sprintf("http://127.0.0.1:%d/api/settings", port)
	b, _ := json.Marshal(body)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-KB-Admin", testAdminToken)
	req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", port))
	req.Header.Set("Content-Type", "application/json")

	resp, err := testClient(5 * time.Second).Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败：%v", url, err)
	}
	defer resp.Body.Close()

	var parsed map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&parsed)
	return resp.StatusCode, parsed
}

// getStatus 探一个端口还在不在。
func getStatus(port int) error {
	req, _ := http.NewRequest(http.MethodGet, fmt.Sprintf("http://127.0.0.1:%d/api/status", port), nil)
	req.Header.Set("X-KB-Admin", testAdminToken)
	req.Header.Set("Origin", fmt.Sprintf("http://127.0.0.1:%d", port))
	resp, err := testClient(2 * time.Second).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return nil
}

// TestSettingsHotReloadPort 验改端口当场生效：旧端口立刻断，新端口立刻能用。
func TestSettingsHotReloadPort(t *testing.T) {
	srv, oldPort := startTestServer(t)
	next := freePort(t)

	status, body := postSettings(t, oldPort, map[string]any{"port": next})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%v", status, body)
	}
	if body["moved"] != true {
		t.Fatalf("moved 应为 true：%v", body)
	}
	if body["restartRequired"] != false {
		t.Errorf("改端口不该再要求重启：%v", body)
	}
	eff, _ := body["effective"].(map[string]any)
	if got := int(eff["port"].(float64)); got != next {
		t.Fatalf("effective.port = %d，想要 %d", got, next)
	}
	if srv.Port() != next {
		t.Errorf("Server.Port() = %d，想要 %d", srv.Port(), next)
	}

	// 新端口能用。
	if err := getStatus(next); err != nil {
		t.Errorf("新端口 %d 应该已经能连：%v", next, err)
	}
	// 旧端口应该已经关了。
	if err := getStatus(oldPort); err == nil {
		t.Errorf("旧端口 %d 应该已经关掉（old=%d next=%d srv=%d）", oldPort, oldPort, next, srv.Port())
	}
}

// TestSettingsReloadKeepsServingAfterFailure 验新地址绑不上时，旧端口照旧能用。
//
// “先绑新的再关旧的”就是为了这一条：反过来的话用户连改回去都做不到。
// 用一个绑不上的地址（TEST-NET-1，192.0.2.0/24）让它必然失败：
// 占一个端口是没用的，listenLoop 会自动 +1 找到下一个空闲端口。
func TestSettingsReloadKeepsServingAfterFailure(t *testing.T) {
	srv, oldPort := startTestServer(t)

	if _, err := srv.Reload("192.0.2.1", 7000); err == nil {
		t.Fatal("绑一个不存在的本机地址应该失败")
	}
	if got := srv.Port(); got != oldPort {
		t.Errorf("失败后端口不该变：%d → %d", oldPort, got)
	}
	if err := getStatus(oldPort); err != nil {
		t.Errorf("失败后旧端口必须还能用：%v", err)
	}
}

// TestServerAddrUsesLoopbackForWildcard 验绑 0.0.0.0 时报出来的地址是能点开的。
func TestServerAddrUsesLoopbackForWildcard(t *testing.T) {
	srv, port := startTestServer(t)
	if got, want := srv.Addr(), "http://127.0.0.1:"+strconv.Itoa(port); got != want {
		t.Errorf("Addr() = %q，想要 %q", got, want)
	}
}

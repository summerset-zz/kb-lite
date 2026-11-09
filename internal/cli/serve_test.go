package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// TestServeCanSaveSettings 起一个真的 `serve`，再从管理接口改一次配置。
//
// 这个测试守的是**接线**，不是逻辑：serve 建 Manager 时漏过 ConfigDir，
// 于是界面里每一次保存都回「不知道配置目录，改不了配置」—— 建库、关访问密码、
// 换记录文件位置、向导收尾全都失败，而 daemon 层的测试都自己把 ConfigDir 摆好了，
// 一个都没抓到。所以这里必须走 runServe 本身，不能自己 NewManager。
func TestServeCanSaveSettings(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	port := closedPort(t)
	cfg := daemon.DefaultConfig(dir)
	cfg.Port = port
	cfg.RequireToken = false
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}

	done := make(chan int, 1)
	go func() { done <- runServe([]string{"--no-tray", "--no-db", "--port", strconv.Itoa(port)}) }()
	t.Cleanup(func() {
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			t.Log("serve 十秒内没退出，可能卡在 Wait() 上")
		}
	})

	base := "http://127.0.0.1:" + strconv.Itoa(port)
	// 代理要关掉：这台机器上常驻系统代理，走代理连 127.0.0.1 会失败（见 daemon 里的测试）。
	hc := &http.Client{Transport: &http.Transport{Proxy: nil}, Timeout: 5 * time.Second}
	waitListening(t, base)

	admin := readAdminToken(t, dir)

	// 1. 打开访问密码（就是界面上勾那个复选框）。
	code, body := adminPost(t, hc, base+"/api/settings", admin, map[string]any{"requireToken": true})
	if code != http.StatusOK {
		t.Fatalf("保存设置失败：HTTP %d %s", code, body)
	}

	saved, err := daemon.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !saved.RequireToken {
		t.Fatalf("接口说成功了，但 config.json 里 requireToken 还是 false：%s", body)
	}

	// 2. 关掉它，确认不是单向的。
	code, body = adminPost(t, hc, base+"/api/settings", admin, map[string]any{"requireToken": false})
	if code != http.StatusOK {
		t.Fatalf("再次保存失败：HTTP %d %s", code, body)
	}
	saved, err = daemon.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if saved.RequireToken {
		t.Fatalf("关不掉了：%s", body)
	}

	// 3. 收拾干净，顺便验证退出这条路。
	if code, body := adminPost(t, hc, base+"/api/shutdown", admin, nil); code != http.StatusOK {
		t.Fatalf("退出失败：HTTP %d %s", code, body)
	}
}

// waitListening 等端口能连上（serve 起监听要一会儿）。
func waitListening(t *testing.T, base string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", strings.TrimPrefix(base, "http://"), 200*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("%s 十秒内没起来", base)
}

func readAdminToken(t *testing.T, dir string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(dir, daemon.APITokenFileName))
	if err != nil {
		t.Fatalf("读管理凭据失败：%v", err)
	}
	return strings.TrimSpace(string(b))
}

// adminPost 发一个管理面请求：CLI 用 X-KB-Admin，并且要带同源信号（docs/07）。
func adminPost(t *testing.T, hc *http.Client, url, admin string, payload any) (int, string) {
	t.Helper()
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(http.MethodPost, url, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-KB-Admin", admin)
	req.Header.Set("Origin", "http://"+req.URL.Host)
	resp, err := hc.Do(req)
	if err != nil {
		t.Fatalf("请求 %s 失败：%v", url, err)
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(out)
}

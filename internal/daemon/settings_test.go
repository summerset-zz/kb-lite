package daemon

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// nopCloser 把字符串包成请求体。
func nopCloser(s string) io.ReadCloser {
	return io.NopCloser(strings.NewReader(s))
}

// 关掉读取密码要**立刻**生效：用户点完保存不该再等重启。
func TestSettingsToggleTokenTakesEffectNow(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: true})

	// 先确认关着的时候要密码。
	status, _, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if status != http.StatusUnauthorized {
		t.Fatalf("初始应当是 401，实际 %d", status)
	}

	admin := func(r *http.Request) {
		r.Header.Set("X-KB-Admin", testAdminToken)
		r.Header.Set("Origin", srv.URL)
	}
	status, body, raw := doRequest(t, srv, "/api/settings", func(r *http.Request) {
		admin(r)
		r.Method = http.MethodPost
		r.Body = nopCloser(`{"requireToken":false}`)
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["effective"].(map[string]any)["requireToken"] != false {
		t.Errorf("effective.requireToken = %v", body["effective"])
	}

	// 立刻不用密码了。
	status, _, raw = doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if status != http.StatusOK {
		t.Errorf("关掉密码后应当直接可用，实际 %d：%s", status, raw)
	}
}

// 向导的最后一屏要把 setupCompleted 置真，否则重开界面又会回到向导。
func TestSettingsSetupCompleted(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	// 新建的配置还没走完向导。
	_, body, raw := adminReq(t, srv, http.MethodGet, "/api/status", nil)
	if body["setupCompleted"] != false {
		t.Fatalf("setupCompleted 初始应为 false，实际 %v（%s）", body["setupCompleted"], raw)
	}

	status, _, raw := adminReq(t, srv, http.MethodPost, "/api/settings", map[string]any{"setupCompleted": true})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}

	_, body, _ = adminReq(t, srv, http.MethodGet, "/api/status", nil)
	if body["setupCompleted"] != true {
		t.Errorf("保存后 setupCompleted = %v，应该是 true", body["setupCompleted"])
	}
}

// 绑非回环地址时，访问密码必须被强制打开（ADR-015 的硬不变式）。
func TestSettingsLanForcesToken(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	status, body, raw := doRequest(t, srv, "/api/settings", func(r *http.Request) {
		r.Header.Set("X-KB-Admin", testAdminToken)
		r.Header.Set("Origin", srv.URL)
		r.Method = http.MethodPost
		r.Body = nopCloser(`{"host":"0.0.0.0","requireToken":false}`)
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	eff := body["effective"].(map[string]any)
	if eff["requireToken"] != true {
		t.Error("绑 0.0.0.0 时访问密码必须强制为开")
	}
	if body["restartRequired"] != true {
		t.Error("改绑定地址要重启，应当回 restartRequired")
	}
}

// 端口被占时要 400，而不是让用户改完发现起不来。
func TestSettingsPortInUse(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	busy := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)

	status, body, raw := doRequest(t, srv, "/api/settings", func(r *http.Request) {
		r.Header.Set("X-KB-Admin", testAdminToken)
		r.Header.Set("Origin", srv.URL)
		r.Method = http.MethodPost
		r.Body = nopCloser(`{"port":` + busy + `}`)
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["error"].(map[string]any)["code"] != "port_in_use" {
		t.Errorf("code = %v", body["error"])
	}
}

// 记录文件放到不能写的地方要 400。
func TestSettingsDBPathNotWritable(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	// 拿一个普通文件当目录用：MkdirAll / CreateTemp 都会失败。
	notDir := filepath.Join(t.TempDir(), "file.txt")
	if err := os.WriteFile(notDir, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	status, body, raw := doRequest(t, srv, "/api/settings", func(r *http.Request) {
		r.Header.Set("X-KB-Admin", testAdminToken)
		r.Header.Set("Origin", srv.URL)
		r.Method = http.MethodPost
		r.Body = nopCloser(`{"dbPath":` + strconv.Quote(filepath.Join(notDir, "index.db")) + `}`)
	})
	if status != http.StatusBadRequest {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["error"].(map[string]any)["code"] != "dir_not_writable" {
		t.Errorf("code = %v", body["error"])
	}
}

// /api/shutdown 要真的调到回调。
func TestShutdownEndpoint(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "正文\n")
	mgr := NewManager(&Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}},
		BuildOptions{Workers: 1, ConfigDir: t.TempDir()})
	if err := mgr.Rebuild(); err != nil {
		t.Fatal(err)
	}

	called := make(chan struct{}, 1)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	h := NewHandler(mgr, testToken, testAdminToken, HandlerOptions{
		Port: port, RequireToken: true, Shutdown: func() { called <- struct{}{} },
	})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: h.Routes()}}
	srv.Start()
	defer srv.Close()

	status, _, raw := doRequest(t, srv, "/api/shutdown", func(r *http.Request) {
		r.Header.Set("X-KB-Admin", testAdminToken)
		r.Header.Set("Origin", srv.URL)
		r.Method = http.MethodPost
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	<-called
}

// 两份 SKILL 装到不同的目录，互不覆盖；旧文件留 .bak。
func TestSkillInstallSeparateDirs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	read, err := InstallSkill("read", "dsh", "---\nname: kb-lite\n---\n正文")
	if err != nil {
		t.Fatal(err)
	}
	auth, err := InstallSkill("authoring", "dsh", "---\nname: kb-lite-authoring\n---\n正文")
	if err != nil {
		t.Fatal(err)
	}
	if read.Path == auth.Path {
		t.Fatalf("两份 SKILL 装到同一个文件了：%s", read.Path)
	}
	if !strings.Contains(read.Path, filepath.Join(".agents", "skills", "kb-lite")) {
		t.Errorf("读取 SKILL 路径不对：%s", read.Path)
	}
	if !strings.Contains(auth.Path, filepath.Join(".agents", "skills", "kb-lite-authoring")) {
		t.Errorf("写作 SKILL 路径不对：%s", auth.Path)
	}
	if _, err := os.Stat(read.Path); err != nil {
		t.Errorf("文件没写出来：%v", err)
	}

	// 再装一次：旧内容要留 .bak。
	if _, err := InstallSkill("read", "dsh", "---\nname: kb-lite\n---\n新内容"); err != nil {
		t.Fatal(err)
	}
	bak, err := os.ReadFile(read.Path + ".bak")
	if err != nil {
		t.Fatalf("没有备份旧文件：%v", err)
	}
	if !strings.Contains(string(bak), "正文") {
		t.Errorf(".bak 内容不对：%s", bak)
	}
}

package cli

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

const testToken = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

// setup 起一个真的服务，并把配置目录/端口/token 都摆好，让 CLI 能连上。
func setup(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	kbRoot := filepath.Join(dir, "笔记源")
	mustWrite(t, filepath.Join(kbRoot, "ops", "runbook.md"),
		"---\ntitle: 运维手册\nsummary: 线上环境的操作手册。\ntags: [ops, 上线]\n---\n\n"+
			"线上环境的操作手册。\n\n"+
			"## 上线流程\n\n先打 tag，再跑 deploy 脚本。\n\n"+
			"## 回滚\n\n### 快速回滚\n\n回滚到上一个 tag。\n")

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port

	cfg := &daemon.Config{
		SchemaVersion: daemon.SchemaVersion,
		Port:          port,
		DBPath:        filepath.Join(dir, "index.db"),
		KBs: []daemon.KB{
			{ID: "k1", Name: "笔记", RootPath: kbRoot, Enabled: true},
		},
	}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, daemon.TokenFileName), []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := daemon.NewManager(cfg, daemon.BuildOptions{Workers: 1})
	if err := mgr.Rebuild(); err != nil {
		t.Fatal(err)
	}
	h := daemon.NewHandler(mgr, testToken, daemon.HandlerOptions{Port: port})
	srv := &httptest.Server{Listener: ln, Config: &http.Server{Handler: h.Routes()}}
	srv.Start()
	t.Cleanup(srv.Close)

	return kbRoot
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// capture 截获 stdout，返回输出与退出码。
func capture(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := fn()
	_ = w.Close()
	os.Stdout = old
	out, _ := io.ReadAll(r)
	return string(out), code
}

func TestSearchOutput(t *testing.T) {
	setup(t)
	out, code := capture(t, func() int { return Run([]string{"search", "笔记", "上线"}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d，输出：%s", code, out)
	}
	for _, want := range []string{"运维手册", "ops/runbook.md", "返回 1 条"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出里没有 %q：\n%s", want, out)
		}
	}
}

func TestSearchExplain(t *testing.T) {
	setup(t)
	out, code := capture(t, func() int { return Run([]string{"search", "笔记", "上线", "--explain"}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d", code)
	}
	if !strings.Contains(out, "上线:") {
		t.Errorf("--explain 应当打印每个关键词的命中位置：\n%s", out)
	}
	// 「上线」在标签（→摘要）、章节标题、全文里；**不在**条目名「运维手册」里。
	if !strings.Contains(out, "章节") || !strings.Contains(out, "摘要") {
		t.Errorf("命中位置不对：\n%s", out)
	}
}

func TestSearchJSON(t *testing.T) {
	setup(t)
	out, code := capture(t, func() int { return Run([]string{"search", "笔记", "上线", "--json"}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d", code)
	}
	if !strings.HasPrefix(strings.TrimSpace(out), "{") {
		t.Errorf("--json 应当原样输出服务端 JSON：\n%s", out)
	}
}

func TestTocOutput(t *testing.T) {
	setup(t)
	out, code := capture(t, func() int { return Run([]string{"toc", "笔记", "ops/runbook.md"}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d，输出：%s", code, out)
	}
	if !strings.Contains(out, "2 回滚 · 空") {
		t.Errorf("toc 里应当标出空节：\n%s", out)
	}
}

func TestReadOutput(t *testing.T) {
	setup(t)
	out, code := capture(t, func() int { return Run([]string{"read", "笔记", "ops/runbook.md", "--section", "1"}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d，输出：%s", code, out)
	}
	if !strings.Contains(out, "先打 tag") || !strings.Contains(out, "上线流程") {
		t.Errorf("输出不对：\n%s", out)
	}
}

func TestReadMissingSectionExitsThree(t *testing.T) {
	setup(t)
	_, code := capture(t, func() int {
		return Run([]string{"read", "笔记", "ops/runbook.md", "--title", "不存在的标题"})
	})
	if code != exitNotFound {
		t.Errorf("查无此节应当退出码 3，实际 %d", code)
	}
}

// daemon 没在跑时，search 按 docs/05 自动降级为离线索引（进程内现场切片）。
func TestSearchFallsBackOffline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	kbRoot := filepath.Join(dir, "kb")
	mustWrite(t, filepath.Join(kbRoot, "a.md"), "---\ntitle: 离线也能用\n---\n\n这里有个少见词：鲸落。\n")

	cfg := daemon.DefaultConfig(dir)
	cfg.KBs = []daemon.KB{{ID: "k1", Name: "笔记", RootPath: kbRoot, Enabled: true}}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}

	// 刻意不起服务。
	out, code := capture(t, func() int { return Run([]string{"search", "笔记", "鲸落"}) })
	if code != exitOK {
		t.Fatalf("离线 search 应当成功，退出码 = %d，输出：%s", code, out)
	}
	if !strings.Contains(out, "离线也能用") {
		t.Errorf("离线索引没命中：\n%s", out)
	}
}

// toc / read / skill 目前仍然需要 daemon（离线降级只接了 search，见下一轮的 TODO）。
func TestReadWithoutDaemonExitsFour(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()

	cfg := daemon.DefaultConfig(dir)
	cfg.Port = port
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, daemon.TokenFileName), []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	_, code := capture(t, func() int { return Run([]string{"read", "笔记", "a.md", "--section", "1"}) })
	if code != exitDaemonDown {
		t.Errorf("daemon 没跑时 read 应当退出码 4，实际 %d", code)
	}
}

func TestAddWritesConfig(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	src := filepath.Join(t.TempDir(), "新库")
	mustWrite(t, filepath.Join(src, "a.md"), "正文\n")
	mustWrite(t, filepath.Join(src, ".git", "config"), "不该被算进去\n")

	out, code := capture(t, func() int { return Run([]string{"add", src}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d，输出：%s", code, out)
	}
	if !strings.Contains(out, "扫到 1 个文件") {
		t.Errorf(".git 下的东西不该被算进来：\n%s", out)
	}

	cfg, err := daemon.Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.KBs) != 1 || cfg.KBs[0].Name != "新库" || !filepath.IsAbs(cfg.KBs[0].RootPath) {
		t.Fatalf("配置不对：%+v", cfg.KBs)
	}
}

func TestListOutput(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	cfg := daemon.DefaultConfig(dir)
	cfg.KBs = []daemon.KB{{ID: "k1", Name: "我的笔记", RootPath: `D:\notes`, Enabled: true}}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}

	out, code := capture(t, func() int { return Run([]string{"list"}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d", code)
	}
	if !strings.Contains(out, "我的笔记") || !strings.Contains(out, `D:\notes`) {
		t.Errorf("输出不对：\n%s", out)
	}
}

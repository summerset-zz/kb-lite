package cli

import (
	"bytes"
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

const (
	testToken      = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	testAdminToken = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
)

// closedPort 返回一个刚放掉的端口。
//
// 这台机器上真的可能有个 kb-lite 在 7331 上跑着。离线降级的测试如果直接用默认端口，
// 就会連到一个真服务上（还被访问密码挡住），变成假失败。
func closedPort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	return port
}

// offlineConfig 是“这台机器上没服务在跑”的配置：知识库摆好，端口指向空号。
func offlineConfig(t *testing.T, dir, kbRoot string) *daemon.Config {
	t.Helper()

	cfg := daemon.DefaultConfig(dir)
	cfg.Port = closedPort(t)
	cfg.KBs = []daemon.KB{{ID: "k1", Name: "笔记", RootPath: kbRoot, Enabled: true}}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}
	return cfg
}

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
	if err := os.WriteFile(filepath.Join(dir, daemon.ReadTokenFileName), []byte(testToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, daemon.APITokenFileName), []byte(testAdminToken+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	mgr := daemon.NewManager(cfg, daemon.BuildOptions{Workers: 1, ConfigDir: dir})
	if err := mgr.Rebuild(); err != nil {
		t.Fatal(err)
	}
	h := daemon.NewHandler(mgr, testToken, testAdminToken, daemon.HandlerOptions{Port: port})
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
//
// 两个必须这么写的地方：
//
//  1. **读端要放在另一个 goroutine 里**。匿名管道的缓冲很小（Windows 上 4 KB），
//     「等写完再读」的写法只要输出超过缓冲就会死锁 —— 表现为「测试跑几十秒然后超时、
//     没有任何 FAIL 行」。`/skill.md` 渲染出来正好 4 KB 出头，就是这么挂的。
//  2. **一定要 defer 还原**。否则被测代码一旦 panic / Goexit，os.Stdout 会一直指向
//     那个管道，后面所有测试的输出（包括失败摘要）都会消失。
func capture(t *testing.T, fn func() int) (string, int) {
	t.Helper()

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	defer func() {
		os.Stdout = old
		_ = w.Close() // panic 路径上也让读端拿到 EOF，别把 goroutine 挂在那儿
	}()

	done := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		done <- string(b)
	}()

	code := fn()

	_ = w.Close()
	return <-done, code
}

// doctor 在一切正常时退出 0，并且把每一项都写出来。
func TestDoctorAllGood(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	kbRoot := filepath.Join(dir, "kb")
	mustWrite(t, filepath.Join(kbRoot, "a.md"), "# 甲\n\n正文\n")
	offlineConfig(t, dir, kbRoot)

	out, code := capture(t, func() int { return Run([]string{"doctor"}) })
	if code != exitOK {
		t.Fatalf("一切都好时退出码应当是 0，实际 %d：\n%s", code, out)
	}
	for _, want := range []string{"配置", "端口", "记录文件", "SQLite", "知识库 笔记", "没发现问题"} {
		if !strings.Contains(out, want) {
			t.Errorf("输出里应该有 %q：\n%s", want, out)
		}
	}
}

// 知识库目录被删掉时，doctor 要说出来是哪一条，并给下一步。
func TestDoctorReportsMissingRoot(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	cfg := daemon.DefaultConfig(dir)
	cfg.Port = closedPort(t)
	cfg.KBs = []daemon.KB{{ID: "k1", Name: "笔记", RootPath: filepath.Join(dir, "这里没有了"), Enabled: true}}
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}

	out, code := capture(t, func() int { return Run([]string{"doctor"}) })
	if code != exitError {
		t.Fatalf("有问题时退出码应当是 1，实际 %d：\n%s", code, out)
	}
	if !strings.Contains(out, "找不到这个文件夹") {
		t.Errorf("应当指出目录不见了：\n%s", out)
	}
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

	offlineConfig(t, dir, kbRoot)

	// 刻意不起服务。
	out, code := capture(t, func() int { return Run([]string{"search", "笔记", "鲸落"}) })
	if code != exitOK {
		t.Fatalf("离线 search 应当成功，退出码 = %d，输出：%s", code, out)
	}
	if !strings.Contains(out, "离线也能用") {
		t.Errorf("离线索引没命中：\n%s", out)
	}
}

// skill --out 必须写出「UTF-8 无 BOM」的文件。
//
// 带 BOM 会让 YAML frontmatter 失效，skill 就读不出来了。这是踩过的坑。
func TestSkillOutWritesUTF8WithoutBOM(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	kbRoot := filepath.Join(dir, "kb")
	mustWrite(t, filepath.Join(kbRoot, "a.md"), "---\ntitle: 甲\n---\n\n正文\n")
	offlineConfig(t, dir, kbRoot)

	outPath := filepath.Join(dir, "skills", "kbl-query", "SKILL.md")
	if code := Run([]string{"skill", "--out", outPath}); code != exitOK {
		t.Fatalf("退出码 = %d", code)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatal("不能带 UTF-8 BOM，会让 frontmatter 失效")
	}
	if !bytes.HasPrefix(b, []byte("---\r\nname: kbl-query")) && !bytes.HasPrefix(b, []byte("---\nname: kbl-query")) {
		t.Fatalf("开头不是 frontmatter：%q", string(b[:40]))
	}

	// 新装默认不要密码，所以文档里不该有任何鉴权头，也不该生成凭据文件。
	if bytes.Contains(b, []byte("Authorization")) {
		t.Error("没启用访问密码时，文档里不该出现 Authorization 头")
	}
	if tok := readToken(dir); tok != "" {
		t.Errorf("没启用访问密码时不该生成 read-token，实际 %q", tok)
	}
}

// 启用访问密码后，离线渲染的 skill 必须带上那把密码，否则用户拿到手也调不通。
func TestSkillOutIncludesTokenWhenRequired(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	kbRoot := filepath.Join(dir, "kb")
	mustWrite(t, filepath.Join(kbRoot, "a.md"), "---\ntitle: 甲\n---\n\n正文\n")
	cfg := offlineConfig(t, dir, kbRoot)
	cfg.RequireToken = true
	if err := cfg.Save(dir); err != nil {
		t.Fatal(err)
	}

	outPath := filepath.Join(dir, "skills", "kb-lite", "SKILL.md")
	if code := Run([]string{"skill", "--out", outPath}); code != exitOK {
		t.Fatalf("退出码 = %d", code)
	}

	b, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	tok := readToken(dir)
	if tok == "" {
		t.Fatal("启用密码时，离线渲染应当顺手生成 read-token")
	}
	if !bytes.Contains(b, []byte(tok)) {
		t.Error("文档里应当带上访问密码")
	}
	if bytes.Contains(b, []byte("Bearer \" ")) {
		t.Error("凭证位置渲染坏了")
	}
}

// toc / read / skill 与 search 一样，daemon 没在跑时自动降级为离线索引（docs/05）。
func TestReadTOCSkillFallBackOffline(t *testing.T) {
	dir := t.TempDir()
	t.Setenv(daemon.ConfigDirEnv, dir)

	kbRoot := filepath.Join(dir, "kb")
	mustWrite(t, filepath.Join(kbRoot, "a.md"),
		"---\ntitle: 离线读\ntags: [测试]\n---\n\n## 第一节\n\n这里有鲸落两个字。\n")

	offlineConfig(t, dir, kbRoot)

	out, code := capture(t, func() int { return Run([]string{"read", "笔记", "a.md", "--section", "1"}) })
	if code != exitOK {
		t.Fatalf("离线 read 退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "鲸落") {
		t.Errorf("离线 read 内容不对：\n%s", out)
	}

	out, code = capture(t, func() int { return Run([]string{"toc", "笔记", "a.md"}) })
	if code != exitOK {
		t.Fatalf("离线 toc 退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "1 第一节") {
		t.Errorf("离线 toc 内容不对：\n%s", out)
	}

	out, code = capture(t, func() int { return Run([]string{"skill"}) })
	if code != exitOK {
		t.Fatalf("离线 skill 退出码 = %d：%s", code, out)
	}
	// 库列表不再写进说明书（它会被装到用户目录，不随配置更新），
	// 改成让 agent 每次自己取。
	if strings.Contains(out, "- 笔记（1 篇") {
		t.Errorf("离线 skill 不该再写死 KB 清单：\n%s", out)
	}
	if !strings.Contains(out, "先看有哪些知识库") {
		t.Errorf("离线 skill 少了“先看有哪些知识库”那段：\n%s", out)
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
	cfg.Port = closedPort(t)
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

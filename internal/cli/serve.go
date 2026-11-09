package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// runServe 是唯一会「真的干活」的命令：剩下的都是它的客户端。
//
// 前台运行，Ctrl+C 退出 —— **不做后台守护化**（docs/05）。需要常驻就用系统服务或桌面版，
// 静默起一个后台进程会让用户之后遇到端口占用、找不到日志、升级时进程还在之类的麻烦。
func runServe(args []string) int {
	fs := flag.NewFlagSet("serve", flag.ContinueOnError)
	portFlag := fs.Int("port", 0, "")
	host := fs.String("host", "", "")
	dbPath := fs.String("db", "", "")
	noDB := fs.Bool("no-db", false, "")
	openBrowserFlag := fs.Bool("open", false, "")
	force := fs.Bool("force", false, "")
	_ = fs.String("log-level", "info", "")
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	dir, err := daemon.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}

	cfg, err := daemon.Load(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config.json 读不了：%v\n", err)
		return exitError
	}
	if *portFlag > 0 {
		cfg.Port = *portFlag
	}
	if *host != "" {
		cfg.Host = *host
	}
	if *dbPath != "" {
		cfg.DBPath = *dbPath
	}

	// 硬不变式（ADR-015）：绑了非回环地址就必须要访问密码，否则等于把知识库开放给局域网。
	if !daemon.IsLoopbackHost(cfg.Host) && !cfg.RequireToken {
		cfg.RequireToken = true
		fmt.Fprintln(os.Stderr, "绑定到非本机地址，已自动开启访问密码（否则同网段的人都能读你的知识库）。")
	}

	readToken, err := daemon.LoadOrCreateReadToken(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成访问密码失败：", err)
		return exitError
	}
	apiToken, err := daemon.LoadOrCreateAPIToken(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "生成管理凭据失败：", err)
		return exitError
	}

	if len(cfg.KBs) == 0 {
		fmt.Fprintf(os.Stderr, "还没有配置知识库。先执行：kb-lite add <目录>\n配置目录：%s\n", dir)
		return exitError
	}

	// 缓存是**可选的加速**（ADR-003）：打不开就纯内存跑，不要因为缓存坏了就起不来。
	opts := daemon.BuildOptions{Force: *force}
	dbMode := "memory"
	if !*noDB {
		store, err := daemon.OpenStore(cfg.DBPath)
		switch {
		case err == nil:
			defer store.Close()
			opts.Store = store
			dbMode = "sqlite"
		case errors.Is(err, daemon.ErrDBUnavailable), errors.Is(err, daemon.ErrDBNeedsUpgrade):
			fmt.Fprintf(os.Stderr, "%v —— 本次不落盘\n", err)
		default:
			fmt.Fprintf(os.Stderr, "缓存打不开（%v），本次不落盘\n", err)
		}
	}

	mgr := daemon.NewManager(cfg, opts)
	started := time.Now()
	if err := mgr.Rebuild(); err != nil {
		fmt.Fprintln(os.Stderr, "建索引失败：", err)
		return exitError
	}
	printIndexStats(mgr, time.Since(started))

	ln, actualPort, err := listenLoop(cfg.Host, cfg.Port)
	if err != nil {
		fmt.Fprintln(os.Stderr, "起不来：", err)
		return exitExternal
	}
	if actualPort != cfg.Port {
		fmt.Fprintf(os.Stderr, "端口 %d 被占用，改用 %d（已写回 config.json）\n", cfg.Port, actualPort)
		cfg.Port = actualPort
		if err := cfg.Save(dir); err != nil {
			fmt.Fprintln(os.Stderr, "写回配置失败：", err)
		}
	}

	exposed := !daemon.IsLoopbackHost(cfg.Host)
	if exposed {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "!!! 警告：服务绑到了 %s，同一个网络里的人都能访问。\n", cfg.Host)
		fmt.Fprintln(os.Stderr, "!!! 这个模式下 Host 校验会被放开（docs/07 第 1 条），确认过网络环境才这么做。")
		fmt.Fprintln(os.Stderr, "")
	}

	handler := daemon.NewHandler(mgr, readToken, apiToken, daemon.HandlerOptions{
		Port:         actualPort,
		LANExposed:   exposed,
		DBMode:       dbMode,
		RequireToken: cfg.EffectiveRequireToken(),
		// 路由都存在了，所以 /skill.md 里 manifest 那一段会渲染出来（docs/09 v0.2）。
		Features: daemon.DefaultFeatures,
	})

	srv := &http.Server{
		Handler:           handler.Routes(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	base := "http://" + net.JoinHostPort(cfg.Host, strconv.Itoa(actualPort))
	printBanner(base, dir, len(mgr.Index().KBs), cfg.EffectiveRequireToken())

	if *openBrowserFlag {
		openInBrowser(base)
	}

	// Ctrl+C → 优雅退出。
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		fmt.Fprintln(os.Stderr, "\n正在退出…")
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()

	if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	return exitOK
}

// listenLoop 从 startPort 往上找第一个能绑的端口（docs/04：被占时自动 +1）。
func listenLoop(host string, startPort int) (net.Listener, int, error) {
	var lastErr error
	for p := startPort; p < startPort+20; p++ {
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(p)))
		if err == nil {
			return ln, p, nil
		}
		lastErr = err
	}
	return nil, 0, fmt.Errorf("端口 %d–%d 全被占用：%v", startPort, startPort+19, lastErr)
}

func printIndexStats(mgr *daemon.Manager, took time.Duration) {
	for _, kb := range mgr.Index().KBs {
		fmt.Printf("已索引「%s」：%d 篇 / %d 节 / %s\n",
			kb.KB.Name, len(kb.Entries), kb.Sections, formatBytes(kb.Bytes))
		// 增量要看得见：否则用户不知道「这次为什么快」。
		if kb.Stats.Reused+kb.Stats.Parsed > 0 {
			fmt.Printf("  复用缓存 %d 篇，重新切片 %d 篇\n", kb.Stats.Reused, kb.Stats.Parsed)
		}
		if len(kb.Warnings) > 0 {
			warnings := make([]warningDTO, 0, len(kb.Warnings))
			for _, w := range kb.Warnings {
				warnings = append(warnings, warningDTO{Kind: w.Kind, Message: w.Message})
			}
			printWarnings(warnings, "  ")
		}
	}
	for _, st := range mgr.States() {
		switch st.State {
		case "error":
			fmt.Fprintf(os.Stderr, "「%s」索引失败：%s\n", st.Name, st.Error)
		case "disabled":
			fmt.Printf("「%s」已关闭，跳过\n", st.Name)
		}
	}
	fmt.Printf("用时 %d ms\n\n", took.Milliseconds())
}

func printBanner(base, dir string, kbCount int, requireToken bool) {
	fmt.Printf("kb-lite %s · %s · %d 个知识库\n", daemon.Version, base, kbCount)
	if requireToken {
		fmt.Printf("访问密码：%s\n", filepath.Join(dir, daemon.ReadTokenFileName))
		fmt.Printf("把这段给你的 agent：curl -s -H \"Authorization: Bearer $TOKEN\" %s/skill.md\n", base)
	} else {
		// 密码没开就别提它：多一行用不上的东西只会让人以为自己漏配了什么。
		fmt.Printf("这个服务没有启用访问密码（只有本机可以访问）。\n")
		fmt.Printf("把这段给你的 agent：curl -s %s/skill.md\n", base)
	}
	fmt.Println("Ctrl+C 退出。")
	fmt.Println()
}

func openInBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", "", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintln(os.Stderr, "打不开浏览器：", err)
	}
}

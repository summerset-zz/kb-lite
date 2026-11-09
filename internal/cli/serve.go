package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"github.com/summerset-zz/kb-lite/internal/daemon"
	"github.com/summerset-zz/kb-lite/internal/tray"
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
	noTray := fs.Bool("no-tray", false, "")
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

	// 单实例（docs/06 第二节）：已经有 kb-lite 在跑就**只把界面打开**，然后退出自己。
	//
	// 不这么做的话：第二个进程会占住下一个端口（端口自动 +1 让它静默成功）、
	// 托盘里冒出第二个图标、配置被反复改写。双击两次、或者开机自启与手动打开
	// 撞在一起时，都会走到这里。
	if prev, running := daemon.AnotherInstance(dir); running {
		base := "http://127.0.0.1:" + strconv.Itoa(prev.Port)
		fmt.Fprintf(os.Stderr, "kb-lite 已经在跑了（pid %d，%s），这次直接打开界面。\n", prev.PID, base)
		fmt.Fprintln(os.Stderr, "要重启请先 `kb-lite quit`。")
		if prev.Port > 0 {
			openInBrowser(base)
		}
		return exitOK
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
		// 零知识库也要能起：向导的第一步就是「先起服务，再在界面里建第一个库」。
		// 这里只提示一句，不拦着用户。
		fmt.Fprintln(os.Stderr, "还没有知识库，启动后到界面里添加。")
	}

	// 缓存是**可选的加速**（ADR-003）：打不开就纯内存跑，不要因为缓存坏了就起不来。
	//
	// ConfigDir 必须给：管理接口（界面里的设置页、建库、换访问密码）改完配置要落盘，
	// 少了它每一次保存都会回「不知道配置目录，改不了配置」。daemon 层的测试都是自己
	// 把 ConfigDir 摆好的，所以这个漏项只有真的跑起 serve 才看得见（见 serve_test.go）。
	opts := daemon.BuildOptions{Force: *force, ConfigDir: dir}
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

	// 监听交给 daemon.Server：设置页改端口或绑定地址时它会当场重开（docs/06 §5.4）。
	var srv *daemon.Server
	srv = daemon.NewServer(mgr, readToken, apiToken, daemon.HandlerOptions{
		DBMode:       dbMode,
		RequireToken: cfg.EffectiveRequireToken(),
		// 路由都存在了，所以 /skill.md 里 manifest 那一段会渲染出来（docs/09 v0.2）。
		Features: daemon.DefaultFeatures,
		Shutdown: func() { srv.Shutdown() },
	})

	// 锁目录里的 pid 与端口。热重开改端口后要跟着改，否则重复启动会去敲旧端口。
	exePath, _ := os.Executable()
	srv.OnBind(func(port int) {
		_ = daemon.WriteLock(dir, daemon.LockInfo{
			PID:       os.Getpid(),
			Port:      port,
			StartedAt: time.Now().UnixMilli(),
			Exe:       exePath,
		})
	})
	defer daemon.RemoveLock(dir)

	actualPort, err := srv.Start(cfg.Host, cfg.Port)
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

	if exposed := !daemon.IsLoopbackHost(cfg.Host); exposed {
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintf(os.Stderr, "!!! 警告：服务绑到了 %s，同一个网络里的人都能访问。\n", cfg.Host)
		fmt.Fprintln(os.Stderr, "!!! 这个模式下 Host 校验会被放开（docs/07 第 1 条），确认过网络环境才这么做。")
		fmt.Fprintln(os.Stderr, "")
	}

	// shutdown 由 serve 自己实现：UI 的“退出”按钮与 `kb-lite stop` 都打到 /api/shutdown。
	base := srv.Addr()
	printBanner(base, dir, len(mgr.Index().KBs), cfg.EffectiveRequireToken())

	if *openBrowserFlag {
		openInBrowser(base)
	}

	// Windows 上顺手挂个托盘（docs/06 第四节）：暂停/启动、设置、在命令行中打开、退出。
	// `--no-tray` 给服务器/脚本用，免得在没有桌面的会话里出现奇怪的东西。
	if !*noTray {
		startTray(srv, mgr)
	}

	// Ctrl+C → 优雅退出。
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	go func() {
		<-interrupt
		fmt.Fprintln(os.Stderr, "\n正在退出…")
		srv.Shutdown()
	}()

	if err := srv.Wait(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	return exitOK
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

// startTray 挂上 Windows 托盘（其他平台什么都不做，见 tray.Available）。
//
// 托盘只是 UI 的第二个入口，**任何失败都不能影响服务本身**：起不来就打一行提示，
// 服务照常跑。用户还可以用浏览器打开界面。
func startTray(srv *daemon.Server, mgr *daemon.Manager) {
	if !tray.Available() {
		return
	}
	go func() {
		err := tray.Run(
			func() tray.State {
				st := tray.State{Address: srv.Addr(), Paused: srv.Handler().Paused()}
				// 有任何一个知识库在整理就显示进度（菜单里那一行）。
				for _, kb := range mgr.Status(srv.Port(), "", false).KBs {
					if kb.State == "indexing" && kb.Progress != nil {
						st.Indexing = true
						st.Done, st.Total = kb.Progress.Done, kb.Progress.Total
						break
					}
				}
				return st
			},
			tray.Actions{
				TogglePause: func() { srv.Handler().SetPaused(!srv.Handler().Paused()) },
				OpenBrowser: openInBrowser,
				OpenConsole: func() {
					dir, _ := os.Executable()
					cmd := exec.Command("cmd", "/c", "start", "", "cmd", "/k", "kb-lite -h")
					cmd.Dir = filepath.Dir(dir)
					if err := cmd.Start(); err != nil {
						fmt.Fprintln(os.Stderr, "打不开命令行：", err)
					}
				},
				CopyAddress: func() string {
					addr := srv.Addr()
					if tray.CopyToClipboard(addr) {
						return addr
					}
					return ""
				},
				Quit: srv.Shutdown,
			},
		)
		if err != nil {
			fmt.Fprintln(os.Stderr, "托盘没起来（服务照常可用）：", err)
		}
	}()
}

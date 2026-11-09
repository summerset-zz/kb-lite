package cli

import (
	"flag"
	"fmt"
	"os"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// runAutostart 是 `kb-lite autostart status|enable|disable`（docs/05）。
//
// 它直接在**本机**改，不走管理接口：用户完全可能在没有起服务的时候就想把自启配好，
// 而且这件事跟 daemon 在不在跑没有关系。
func runAutostart(args []string) int {
	fs := flag.NewFlagSet("autostart", flag.ContinueOnError)
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	action := "status"
	if len(rest) > 0 {
		action = rest[0]
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}

	switch action {
	case "status":
		info := daemon.StartupStatus(exe)
		if !info.Supported {
			fmt.Println("这个系统还不支持开机自启，只能开机后自己打开一次。")
			return exitOK
		}
		if info.Enabled {
			fmt.Println("开机自启：已开启")
		} else {
			fmt.Println("开机自启：未开启")
		}
		fmt.Printf("启动的命令：%s\n", info.Command)
		fmt.Printf("位置：%s\n", info.Path)
		if info.Error != "" {
			fmt.Fprintln(os.Stderr, "注意："+info.Error)
			return exitError
		}
		return exitOK

	case "enable", "disable":
		on := action == "enable"
		if err := daemon.SetStartup(exe, on); err != nil {
			return fail(err)
		}
		if on {
			fmt.Printf("已开启开机自启：登录后会跑 %s\n", daemon.AutostartCommand(exe))
		} else {
			fmt.Println("已关闭开机自启。")
		}
		return exitOK

	default:
		fmt.Fprintln(os.Stderr, "用法：kb-lite autostart [status|enable|disable]")
		return exitError
	}
}

// runShortcut 是 `kb-lite shortcut`：在桌面放一个快捷方式。
func runShortcut(args []string) int {
	fs := flag.NewFlagSet("shortcut", flag.ContinueOnError)
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	exe, err := os.Executable()
	if err != nil {
		return fail(err)
	}
	path, err := daemon.DesktopShortcut(exe)
	if err != nil {
		return fail(err)
	}
	fmt.Printf("已在桌面创建：%s\n", path)
	return exitOK
}

// runUpdate 是 `kb-lite update`：查一次有没有新版本。
//
// 这是 kb-lite 唯一会主动访问外网的命令，只在用户明确敲的时候发一次请求。
func runUpdate(args []string) int {
	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	refresh := fs.Bool("refresh", false, "")
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	info := daemon.FetchUpdate(daemon.Version, *refresh)
	fmt.Printf("当前版本  %s\n", info.Current)
	if !info.OK {
		fmt.Fprintln(os.Stderr, "这次没查到："+info.Error)
		fmt.Fprintln(os.Stderr, "可能没联网，或者 GitHub 被挡住了。稍后再试一次。")
		return exitError
	}
	fmt.Printf("最新版本  %s\n", info.Latest)
	switch {
	case info.Newer:
		fmt.Printf("有新版本了：%s\n", info.URL)
	case info.Dev:
		fmt.Println("你用的是一个开发中的版本，正式版可能还没有它新。")
	default:
		fmt.Println("已经是最新的。")
	}
	return exitOK
}

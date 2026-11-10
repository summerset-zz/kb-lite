package daemon

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ErrPathMissing 表示要打开的位置已经不在了（被删、改名、或盘符拔了）。
var ErrPathMissing = errors.New("这个位置不在了")

// RevealPath 用系统的文件管理器打开一个位置。
//
// 传目录就打开它本身，传文件就在它所在的目录里选中它。
//
// 只开窗口、不写任何文件、不删东西，所以不违反 ADR-004（只读）。
// 管理面允许它是为了界面上那两个动作：知识库那一行的「打开文件位置」，
// 以及 SKILL 那一行的同名动作。用户想自己用编辑器改一篇笔记时，
// 唯一念得出的下一步就是“打开那个文件夹”。
//
// 起的是**系统自带**的程序（explorer / open / xdg-open），不等它退出：
// explorer.exe 即使成功打开也常常回退出码 1，等它只会白等，还会在请求里挂几秒。
func RevealPath(p string) error {
	p = strings.TrimSpace(p)
	if p == "" {
		return ErrInvalidRoot
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return ErrPathMissing
	}
	// 展开符号链接：用户看到的路径与实际目录可能不是同一个写法。
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	st, err := os.Stat(abs)
	if err != nil {
		return ErrPathMissing
	}

	cmd := revealCommand(abs, st.IsDir())
	if err := cmd.Start(); err != nil {
		return err
	}
	// 一定要收尸，否则 Unix 上会留下僵尸进程。这个 goroutine 只等它退出，不做别的。
	go func() { _ = cmd.Wait() }()
	return nil
}

// revealCommand 按平台拼出“打开这个位置”的命令。
func revealCommand(abs string, isDir bool) *exec.Cmd {
	switch runtime.GOOS {
	case "windows":
		if isDir {
			return exec.Command("explorer.exe", abs)
		}
		// `/select,` 让资源管理器打开父目录并选中它。
		return exec.Command("explorer.exe", "/select,"+abs)
	case "darwin":
		// -R 在 Finder 里选中它；目录同样适用（选中那个目录本身）。
		return exec.Command("open", "-R", abs)
	default:
		// Linux / 其他 Unix：没有统一的“选中”约定，文件就退回到打开它所在的目录。
		if !isDir {
			abs = filepath.Dir(abs)
		}
		return exec.Command("xdg-open", abs)
	}
}

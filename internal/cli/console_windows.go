//go:build windows

package cli

import (
	"os"
	"syscall"
	"unsafe"
)

var (
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetConsoleWindow      = kernel32.NewProc("GetConsoleWindow")
	procGetConsoleProcessList = kernel32.NewProc("GetConsoleProcessList")
	procFreeConsole           = kernel32.NewProc("FreeConsole")
)

// detachOwnConsole 把**双击时系统给我们开的那一个**控制台窗口收掉。
//
// 为什么要动这件事：双击 exe 时 Windows 会给控制台程序开一个黑窗口，而关掉那个窗口
// 等于杀掉进程。对不懂电脑的人来说，屏幕上多一个黑框，他多半会顺手关掉，然后发现
// 服务没了、AI 也连不上了，却没有任何提示。
//
// 只在“这个控制台是我一个人的”时候才动：GetConsoleProcessList 看得到别的进程
// （cmd、PowerShell、Windows Terminal）就说明是从终端里跑的，那时窗口是用户的，
// 收掉它等于抢了人家正在用的东西，Ctrl+C 也没了。
func detachOwnConsole() {
	hwnd, _, _ := procGetConsoleWindow.Call()
	if hwnd == 0 {
		return // 本来就没有控制台（比如从 GUI 进程里拉起）
	}

	var procs [2]uint32
	n, _, _ := procGetConsoleProcessList.Call(
		uintptr(unsafe.Pointer(&procs[0])),
		uintptr(len(procs)),
	)
	if n != 1 {
		// 0 = 拿不到（没有控制台），>1 = 控制台是终端和别的进程共用的。
		return
	}

	if r, _, _ := procFreeConsole.Call(); r == 0 {
		return
	}
	// 断开之后 stdout/stderr 已经没有去处了，后面的打印会静默失败。
	// 指到 devnull，至少 Write 的返回值是正常的，不会让调用方以为出了别的事。
	if f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0); err == nil {
		os.Stdout = f
		os.Stderr = f
	}
}

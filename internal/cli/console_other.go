//go:build !windows

package cli

// detachOwnConsole 在非 Windows 上是空操作。
//
// macOS 与 Linux 双击可执行文件本来就不会开一个“关掉就杀进程”的控制台窗口，
// 这个坑是 Windows 控制台子系统特有的。
func detachOwnConsole() {}

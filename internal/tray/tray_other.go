//go:build !windows

// 托盘只在 Windows 提供（docs/06 第四节）。其他平台两件事都不做：
// Available() 为假，serve 就不会挂托盘；Run 永远不会被调用。
//
// macOS 与 Linux 用 `kb-lite serve --open` + 系统自启覆盖“开机就要能用”，
// 真需要托盘时再评估（docs/09 的 v2 条件）。
package tray

// Available 报告本平台有没有托盘。
func Available() bool { return false }

// Run 在非 Windows 上什么都不做，立即返回。
func Run(state func() State, act Actions) error { return nil }

// CopyToClipboard 在非 Windows 上不可用。
func CopyToClipboard(string) bool { return false }

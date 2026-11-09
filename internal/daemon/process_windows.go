//go:build windows

package daemon

import "os"

// processAlive 判断某个 pid 还在不在。
//
// Windows 上 os.FindProcess 走的是 OpenProcess：进程不存在就返回错误，
// 所以拿到进程句柄就算活着（不能再用 Signal(0)，Windows 不支持它）。
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	_ = p.Release()
	return true
}

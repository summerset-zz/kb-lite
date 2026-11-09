//go:build !windows

package daemon

import (
	"os"
	"syscall"
)

// processAlive 判断某个 pid 还在不在。
//
// Unix 上 os.FindProcess 永远成功（它只是查表），所以要用 signal 0 探一下：
// 能发出去说明进程在，顺带也判断了权限。
func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	defer p.Release()
	return p.Signal(syscall.Signal(0)) == nil
}

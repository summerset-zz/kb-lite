// Command kb-lite 把本地 markdown 目录变成只读 HTTP 知识库。
//
// 这里只做一件事：解析 argv、调 internal/cli、把退出码交给系统。
// 真正的逻辑在 internal/cli（薄客户端）与 internal/daemon（常驻服务）。
package main

import (
	"os"

	"github.com/summerset-zz/kb-lite/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:]))
}

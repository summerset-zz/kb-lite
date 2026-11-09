// Package cli 是 daemon 的**薄客户端**。
//
// 除了 `serve` 和几个纯配置命令（`add` / `list` / `token`），所有命令都翻译成一次 HTTP 调用。
// 不直接读写索引的理由见 docs/05：会与正在运行的 daemon 抢锁，而且等于把业务逻辑实现两遍。
package cli

import (
	"fmt"
	"os"
	"strings"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// 退出码（docs/05）。脚本要能据此分支，所以这些值不要改。
const (
	exitOK         = 0
	exitError      = 1
	exitExternal   = 2
	exitNotFound   = 3
	exitDaemonDown = 4
)

const usage = `kb-lite — 把本地 markdown 目录变成只读 HTTP 知识库

用法：kb-lite <command> [options]

  serve                     前台起服务（Ctrl+C 退出）
    --port <n>              端口；被占时自动 +1 并写回 config
    --host <addr>           默认 127.0.0.1；改成 0.0.0.0 会强制开启访问密码
    --open                  启动后打开浏览器
    --db <path>             缓存库位置（默认 <配置目录>/index.db，可以随时删）
    --no-db                 不落盘（纯内存，每次启动都重切）
    --force                 忽略缓存，全部重新切片
    --log-level <l>         error|warn|info|debug

  add <dir>                 添加知识库（重启 serve 后生效）
    --name <n>              展示名，默认取目录名
    --ignore <glob>...      额外忽略，可重复（.git / node_modules 永远忽略）
    --include <glob>...     覆盖默认的 **/*.md|markdown|txt

  list                      列出已配置的知识库

  status                    建一遍索引并打印统计（不需要 daemon）
    --entries <n>           每个库另外打印前 N 个条目
    --json                  输出 JSON

  search <name> <query>...  检索
    --limit <n>             默认 5，最大 10
    --explain               打印每个关键词命中的位置
    --json                  原样打印服务端 JSON

  toc  <name> <path>        只看目录
  manifest <name>           按预算裁剪的分层概览（不知道搜什么词时用）
    --budget <n>            默认 2000 字
    --prefix <p>            只看某个目录
    --depth <n>             目录展开层级，默认 3
    --json                  JSON 格式
  read <name> <path>        读一节
    --section <n> | --title <t>
    --offset <n>  --max-chars <n>

  skill                     打印 /skill.md（可直接重定向成 SKILL.md）
    --out <path>            写到文件（UTF-8、无 BOM），已存在的目录会自动创建
    --url <base>            覆盖服务地址
  token [--show] [--rotate] 看/换读取面的访问密码（管理凭据不提供打印）
  version
`

// Run 是 CLI 的入口，返回进程退出码。
func Run(args []string) int {
	if len(args) == 0 {
		fmt.Print(usage)
		return exitOK
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return runServe(rest)
	case "add":
		return runAdd(rest)
	case "list":
		return runList(rest)
	case "status":
		return runStatus(rest)
	case "search":
		return runSearch(rest)
	case "toc":
		return runTOC(rest)
	case "manifest":
		return runManifest(rest)
	case "read":
		return runRead(rest)
	case "skill":
		return runSkill(rest)
	case "token":
		return runToken(rest)
	case "version", "--version", "-v":
		fmt.Println("kb-lite " + daemon.Version)
		return exitOK
	case "help", "--help", "-h":
		fmt.Print(usage)
		return exitOK
	default:
		fmt.Fprintf(os.Stderr, "未知命令：%s\n\n", cmd)
		fmt.Fprint(os.Stderr, usage)
		return exitError
	}
}

// stringsFlag 是可重复的字符串参数（`--ignore a --ignore b`）。
type stringsFlag []string

func (s *stringsFlag) String() string { return fmt.Sprint([]string(*s)) }

func (s *stringsFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// boolFlagNames 是**不带值**的开关。用来把 `--limit 5` 的 5 正确地算在 flag 这一边。
var boolFlagNames = map[string]bool{
	"explain": true, "json": true, "open": true, "no-db": true, "show": true, "help": true,
	"force": true, "rotate": true,
}

// splitFlags 把参数拆成「flag」与「位置参数」两半边。
//
// Go 的 flag 包遇到第一个位置参数就停止解析，而 docs/05 的用法全部是
// `kb-lite search <name> <query…> --limit 5 --explain` —— flag 在后面。
// 所以先自己重排一遍，再交给 flag 解析。
func splitFlags(args []string, boolFlags map[string]bool) (flags, positional []string) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positional = append(positional, a)
			continue
		}

		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if strings.ContainsRune(name, '=') || boolFlags[name] {
			continue
		}
		// 带值的 flag：把下一个参数也收进 flags 这边（哪怕它长得像位置参数）。
		if i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		}
	}
	return flags, positional
}

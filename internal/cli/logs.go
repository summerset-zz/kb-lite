package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// logsRespDTO 对上 docs/04 的 `GET /api/logs`。条目直接复用 daemon 那边的类型：
// 字段一改，两边一起报错，比手抄一份 JSON 结构强。
type logsRespDTO struct {
	OK      bool                 `json:"ok"`
	Items   []daemon.AccessEntry `json:"items"`
	Latest  int64                `json:"latest"`
	Total   int                  `json:"total"`
	Dropped int                  `json:"dropped"`
	Max     int                  `json:"max"`
}

// runLogs 是界面里那一页的 CLI 对应物（docs/06 第十节的平权清单）。
//
// 默认只打最近 50 条：日志是排查用的，没人会从头翻。要看全量的给 --limit。
func runLogs(args []string) int {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	limit := fs.Int("limit", 50, "")
	asJSON := fs.Bool("json", false, "")
	clear := fs.Bool("clear", false, "")
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	c, err := newClient()
	if err != nil {
		return fail(err)
	}

	if *clear {
		if _, err := c.admin(http.MethodPost, "/api/logs/clear", nil); err != nil {
			return fail(err)
		}
		fmt.Println("日志已清空。")
		return exitOK
	}

	body, err := c.admin(http.MethodGet, "/api/logs?limit="+strconv.Itoa(*limit), nil)
	if err != nil {
		return fail(err)
	}
	if *asJSON {
		fmt.Println(string(body))
		return exitOK
	}

	var resp logsRespDTO
	if err := json.Unmarshal(body, &resp); err != nil {
		fmt.Println("服务端返回的不是合法 JSON：", err)
		return exitError
	}
	if len(resp.Items) == 0 {
		fmt.Println("还没有访问记录。")
		return exitOK
	}
	for _, e := range resp.Items {
		fmt.Printf("%s  %s  %s  %3d  %s\n",
			time.UnixMilli(e.At).Format("15:04:05"),
			padRight(logsWho(e), 16),
			padRight(logsClient(e), 10),
			e.Status,
			e.Action)
	}
	if resp.Dropped > 0 {
		fmt.Printf("\n更早的 %d 条已经被覆盖掉了。\n", resp.Dropped)
	}
	fmt.Printf("共 %d 条，上限 %d 条，重开 kb-lite 会清空。\n", resp.Total, resp.Max)
	return exitOK
}

func logsWho(e daemon.AccessEntry) string {
	if e.Self {
		return "本机"
	}
	if e.Host != "" {
		return e.Host + "(" + e.IP + ")"
	}
	return e.IP
}

// logsClient 与界面上“方式”一列同一套词。
func logsClient(e daemon.AccessEntry) string {
	switch e.Client {
	case daemon.ClientCLI:
		return "命令行"
	case daemon.ClientCurl:
		return "curl"
	case daemon.ClientBrowser:
		return "浏览器"
	case daemon.ClientOther:
		return "其它程序"
	}
	return "未知程序"
}

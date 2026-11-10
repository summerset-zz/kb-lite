package cli

import (
	"strings"
	"testing"
)

// 访问日志在 CLI 上必须也能看：界面上有的东西，命令行不能没有（docs/06 第十节）。
func TestLogsCommand(t *testing.T) {
	setup(t)

	// 先制造一条记录。
	if out, code := capture(t, func() int { return Run([]string{"search", "笔记", "回滚"}) }); code != exitOK {
		t.Fatalf("搜索失败：%s", out)
	}

	out, code := capture(t, func() int { return Run([]string{"logs"}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "搜了") {
		t.Errorf("日志里应当有那次搜索：%s", out)
	}
	if !strings.Contains(out, "本机") {
		t.Errorf("本机请求要标成本机：%s", out)
	}
	if !strings.Contains(out, "命令行") {
		t.Errorf("CLI 的请求应当认成命令行（靠 User-Agent）：%s", out)
	}

	// 清空之后应当没有记录。
	if out, code := capture(t, func() int { return Run([]string{"logs", "--clear"}) }); code != exitOK {
		t.Fatalf("清空失败：%s", out)
	}
	out, code = capture(t, func() int { return Run([]string{"logs"}) })
	if code != exitOK {
		t.Fatalf("退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "还没有访问记录") {
		t.Errorf("清空后不该还有记录：%s", out)
	}
}

package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// 管理命令必须真的打到管理接口上：CLI 里不该再有第二份业务逻辑。
func TestRemoveCommand(t *testing.T) {
	setup(t)

	out, code := capture(t, func() int {
		return Run([]string{"remove", "笔记", "--yes"})
	})
	if code != exitOK {
		t.Fatalf("退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "已移除") || !strings.Contains(out, "文件没有动") {
		t.Errorf("输出没说清做了什么：%s", out)
	}

	cfg, err := daemon.Load(os.Getenv(daemon.ConfigDirEnv))
	if err != nil {
		t.Fatal(err)
	}
	for _, kb := range cfg.KBs {
		if kb.Name == "笔记" {
			t.Error("配置里还留着被删掉的知识库")
		}
	}
}

func TestRenameCommand(t *testing.T) {
	setup(t)

	out, code := capture(t, func() int {
		return Run([]string{"rename", "笔记", "改过的名字"})
	})
	if code != exitOK {
		t.Fatalf("退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "改过的名字") {
		t.Errorf("输出不对：%s", out)
	}
}

func TestDisableThenEnableCommand(t *testing.T) {
	setup(t)

	out, code := capture(t, func() int { return Run([]string{"disable", "笔记"}) })
	if code != exitOK {
		t.Fatalf("disable 退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "索引还留着") {
		t.Errorf("关闭时要说明索引没删：%s", out)
	}

	out, code = capture(t, func() int { return Run([]string{"enable", "笔记"}) })
	if code != exitOK {
		t.Fatalf("enable 退出码 = %d：%s", code, out)
	}
}

func TestReindexCommandWait(t *testing.T) {
	setup(t)

	out, code := capture(t, func() int {
		return Run([]string{"reindex", "笔记", "--wait"})
	})
	if code != exitOK {
		t.Fatalf("退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "整理完成") {
		t.Errorf("--wait 应当等到完成再打印：%s", out)
	}
}

// daemon 在跑时 add 走管理接口，建完能立刻搜到。
func TestAddCommandUsesDaemon(t *testing.T) {
	setup(t)

	dir := os.Getenv(daemon.ConfigDirEnv)
	newRoot := filepath.Join(dir, "第二个库")
	mustWrite(t, filepath.Join(newRoot, "a.md"), "---\ntitle: 甲\n---\n\n正文\n")

	out, code := capture(t, func() int {
		return Run([]string{"add", newRoot, "--name", "第二个"})
	})
	if code != exitOK {
		t.Fatalf("退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "正在整理") {
		t.Errorf("走管理接口时应当说“正在整理”：%s", out)
	}

	// 建完等整理跑完，再搜一次。
	if _, code := capture(t, func() int { return Run([]string{"reindex", "第二个", "--wait"}) }); code != exitOK {
		t.Fatal("reindex 失败")
	}
	out, code = capture(t, func() int { return Run([]string{"search", "第二个", "正文"}) })
	if code != exitOK {
		t.Fatalf("search 退出码 = %d：%s", code, out)
	}
	if !strings.Contains(out, "甲") {
		t.Errorf("新建的库搜不到内容：%s", out)
	}
}

//go:build windows

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 自启项的读写用临时值名，跑完删干净：绝不能在开发机上留下一个真的 kb-lite 启动项。
func TestAutostartRoundTrip(t *testing.T) {
	const name = "kb-lite-test-autostart"
	t.Cleanup(func() { _ = deleteRunValue(name) })

	// 先确保它不存在。
	if err := deleteRunValue(name); err != nil {
		t.Fatalf("清理失败：%v", err)
	}
	if _, err := findRunValue(name); err == nil {
		t.Fatal("刚才删掉的启动项居然还在")
	}

	want := `"C:\Program Files\KBLite\kb-lite.exe" serve`
	if err := setRunValue(name, want); err != nil {
		t.Fatalf("写入失败：%v", err)
	}
	got, err := findRunValue(name)
	if err != nil {
		t.Fatalf("读不回来：%v", err)
	}
	if got != want {
		t.Errorf("读回来是 %q，想要 %q", got, want)
	}

	if err := deleteRunValue(name); err != nil {
		t.Fatalf("删除失败：%v", err)
	}
	if _, err := findRunValue(name); err == nil {
		t.Error("删掉之后还能读到")
	}
}

// 删一个本来就不存在的启动项要当成功（用户点两次“关闭”不该报错）。
func TestDeleteMissingAutostartIsOK(t *testing.T) {
	const name = "kb-lite-test-missing"
	if err := deleteRunValue(name); err != nil {
		t.Fatalf("删不存在的项应当成功，实际：%v", err)
	}
}

// 快捷方式：在临时目录里建，别去动真桌面。
func TestShortcutIn(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "kb-lite.exe")
	if err := os.WriteFile(exe, []byte("not really an exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	link, err := shortcutIn(dir, exe)
	if err != nil {
		t.Fatalf("建快捷方式失败：%v", err)
	}
	if !strings.HasSuffix(strings.ToLower(link), "kblite.lnk") {
		t.Errorf("文件名不对：%s", link)
	}
	info, err := os.Stat(link)
	if err != nil {
		t.Fatalf("文件不存在：%v", err)
	}
	if info.Size() == 0 {
		t.Error("建出来是个空文件")
	}
}

// 开机执行的命令必须带引号，否则路径里有空格就散架了。
func TestAutostartCommandQuotesPath(t *testing.T) {
	got := AutostartCommand(`C:\Program Files\KBLite\kb-lite.exe`)
	if !strings.HasPrefix(got, `"`) || !strings.HasSuffix(got, `" serve`) {
		t.Errorf("命令不对：%s", got)
	}
}

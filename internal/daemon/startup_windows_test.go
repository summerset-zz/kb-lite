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

// 换新版本之后，桌面那个 .lnk 很可能还指着旧的 exe。界面要能看出来。
//
// 三态：没建过 / 指向现在这份 / 指向另一份（那个文件可能已经不在了）。
func TestShortcutStatus(t *testing.T) {
	dir := t.TempDir()
	current := filepath.Join(dir, "new", "kb-lite.exe")
	old := filepath.Join(dir, "old", "kb-lite.exe")
	for _, p := range []string{current, old} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("exe"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 1. 还没建过：Exists 为假，不是错误。
	info := shortcutStatusIn(dir, current)
	if !info.Supported || info.Exists {
		t.Fatalf("还没建就报存在：%+v", info)
	}

	// 2. 建一个指向旧版本的：必须报 mismatch。
	if _, err := shortcutIn(dir, old); err != nil {
		t.Fatalf("建快捷方式失败：%v", err)
	}
	info = shortcutStatusIn(dir, current)
	if !info.Exists || !info.Mismatch {
		t.Fatalf("指向旧版本却没报出来：%+v", info)
	}
	if info.Missing {
		t.Errorf("旧文件还在，不该报 missing：%+v", info)
	}

	// 3. 旧版本被删掉：Missing 要置真（这种情况双击之后还不如报错）。
	if err := os.Remove(old); err != nil {
		t.Fatal(err)
	}
	info = shortcutStatusIn(dir, current)
	if !info.Missing {
		t.Errorf("旧文件已经删了，应当报 missing：%+v", info)
	}

	// 4. 重建成指向当前这份：既不多也不少。
	if _, err := shortcutIn(dir, current); err != nil {
		t.Fatalf("重建失败：%v", err)
	}
	info = shortcutStatusIn(dir, current)
	if !info.Exists || info.Mismatch || info.Missing {
		t.Fatalf("重建之后应当是干净的：%+v", info)
	}
	if !strings.Contains(info.Arguments, "serve") {
		t.Errorf("参数里应当有 serve：%q", info.Arguments)
	}
}

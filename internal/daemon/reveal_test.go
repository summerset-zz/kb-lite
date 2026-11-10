package daemon

import (
	"net/http"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// TestFSRevealValidation 只守错误分支：成功那条会真的拉起资源管理器，
// 在测试里跑等于给跑测试的人弹一堆窗口。
//
// 要守的是「路径不在了别当成成功」：用户点了“打开文件位置”却什么都没发生，
// 比一句“这个位置不在了”糟得多。
func TestFSRevealValidation(t *testing.T) {
	srv := newTestServer(t)

	status, body, raw := adminReq(t, srv, http.MethodPost, "/api/fs/reveal", map[string]string{})
	if status != http.StatusBadRequest {
		t.Fatalf("空路径 status = %d，想要 400：%s", status, raw)
	}
	if body["error"].(map[string]any)["code"] != "invalid_path" {
		t.Errorf("error.code = %v", body["error"])
	}

	missing := filepath.Join(t.TempDir(), "不存在的目录")
	status, _, raw = adminReq(t, srv, http.MethodPost, "/api/fs/reveal",
		map[string]string{"path": missing})
	if status != http.StatusBadRequest {
		t.Fatalf("不存在的路径 status = %d，想要 400：%s", status, raw)
	}
}

// TestRevealCommandShape 守“打开一个位置”这件事在三个平台上的具体形态。
//
// 三个平台对“选中一个文件”的支持不一样，所以断言也必须分平台写：
//
//	Windows  explorer.exe <目录>            / explorer.exe /select,<文件>
//	macOS    open -R <路径>                （-R 本身就是“在 Finder 里选中”，文件目录都适用）
//	Linux    xdg-open <目录>               （文件退回到打开它所在的目录，没有统一的选择约定）
//
// 之前这里写的是“最后一项不能等于文件路径”，在 macOS 上直接把正确实现判成了错。
func TestRevealCommandShape(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.md")

	cmdDir := revealCommand(dir, true)
	cmdFile := revealCommand(file, false)
	last := func(c *exec.Cmd) string { return c.Args[len(c.Args)-1] }

	switch runtime.GOOS {
	case "windows":
		if got := last(cmdDir); got != dir {
			t.Errorf("目录：%v，想要最后一项是目录本身", cmdDir.Args)
		}
		if want := "/select," + file; last(cmdFile) != want {
			t.Errorf("文件：%v，想要最后一项是 %q", cmdFile.Args, want)
		}
	case "darwin":
		// 目录与文件都用 `open -R`，路径原样放最后。
		for _, c := range []*exec.Cmd{cmdDir, cmdFile} {
			if len(c.Args) < 3 || c.Args[1] != "-R" {
				t.Errorf("%v：macOS 上要用 open -R", c.Args)
			}
		}
		if last(cmdFile) != file {
			t.Errorf("文件：%v，想要最后一项是文件本身", cmdFile.Args)
		}
	default:
		if got := last(cmdDir); got != dir {
			t.Errorf("目录：%v，想要最后一项是目录本身", cmdDir.Args)
		}
		if got := last(cmdFile); got != filepath.Dir(file) {
			t.Errorf("文件：%v，想要最后一项是它所在的目录", cmdFile.Args)
		}
	}
}

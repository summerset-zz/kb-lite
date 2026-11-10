package daemon

import (
	"net/http"
	"path/filepath"
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

// TestRevealCommandShape 守“目录开自己、文件选中它”这个差别。
func TestRevealCommandShape(t *testing.T) {
	dir := t.TempDir()
	cmdDir := revealCommand(dir, true)
	if len(cmdDir.Args) < 2 || cmdDir.Args[len(cmdDir.Args)-1] != dir {
		t.Errorf("目录的命令参数 = %v，想要最后一项是目录本身", cmdDir.Args)
	}
	file := filepath.Join(dir, "a.md")
	cmdFile := revealCommand(file, false)
	if len(cmdFile.Args) < 2 {
		t.Fatalf("文件的命令参数 = %v", cmdFile.Args)
	}
	last := cmdFile.Args[len(cmdFile.Args)-1]
	if last == file {
		t.Errorf("文件应当被“选中”而不是直接打开，实际参数 = %v", cmdFile.Args)
	}
}

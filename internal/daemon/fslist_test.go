package daemon

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestUnderHome 守“选文件夹时那句提醒”的判定。
//
// 提醒的文案是“这个目录别的电脑用户读不到，索引也更新不了”。它只用来决定
// 界面要不要多说一句，不能当成安全判断。
func TestUnderHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		t.Skip("这台机器上没有用户目录")
	}

	if !underHome(home) {
		t.Errorf("用户目录自己应当算在里面：%s", home)
	}
	if underHome(filepath.Dir(home)) {
		t.Errorf("用户目录的上一层不该算在里面：%s", filepath.Dir(home))
	}
	if !underHome(filepath.Join(home, "Documents", "笔记")) {
		t.Error("用户目录的子目录应当算在里面")
	}
	if underHome("") {
		t.Error("空路径不该算在里面")
	}
}

// 列目录的响应要带上这个标记，界面才有的可判断。
func TestFSListReportsUnderHome(t *testing.T) {
	srv := newTestServer(t)

	_, body, raw := adminReq(t, srv, http.MethodGet, "/api/fs/list", nil)
	if _, ok := body["path"]; !ok {
		t.Fatalf("响应形状不对：%s", raw)
	}

	// t.TempDir() 在 Windows 上落在 AppData\Local\Temp 里，也就是用户目录之内；
	// 在 Unix 上通常在 /tmp，不在里面。这里只断言“字段存在时值与判定一致”，
	// 具体真假交给 TestUnderHome 里的纯函数测试。
	root := t.TempDir()
	_, body, _ = adminReq(t, srv, http.MethodGet, "/api/fs/list?path="+url.QueryEscape(root), nil)
	got, has := body["underHome"].(bool)
	if !has {
		got = false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if got != underHome(abs) {
		t.Errorf("underHome = %v，纯函数说得是 %v", got, underHome(abs))
	}
}

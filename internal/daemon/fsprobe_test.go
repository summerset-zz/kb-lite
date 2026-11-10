package daemon

import (
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// TestFSProbe 守“添加之前先看清代价”。
//
// 磁盘根目录不能数（走一遍整盘要几分钟），只回 isRoot 让界面追问一句；
// 普通目录数一次 markdown，封顶后只说“至少这么多”。
func TestFSProbe(t *testing.T) {
	srv := newTestServer(t)

	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "一")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "二")
	writeFile(t, filepath.Join(root, "sub", "c.png"), "不是文本")

	status, body, raw := adminReq(t, srv, http.MethodGet, "/api/fs/probe?path="+url.QueryEscape(root), nil)
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if got := body["markdownCount"].(float64); got != 2 {
		t.Errorf("markdownCount = %v，想要 2（a.md + b.txt）", got)
	}
	if body["isRoot"] != false {
		t.Error("普通目录不该报成磁盘根")
	}
	if body["capped"] != false {
		t.Error("两个文件不该触顶")
	}

	// 磁盘根（或文件系统根）：只回 isRoot。
	vol := filepath.VolumeName(root) + string(filepath.Separator)
	if vol != "" {
		status, body, raw = adminReq(t, srv, http.MethodGet, "/api/fs/probe?path="+url.QueryEscape(vol), nil)
		if status != http.StatusOK {
			t.Fatalf("根目录 status = %d：%s", status, raw)
		}
		if body["isRoot"] != true {
			t.Errorf("%q 应当报成磁盘根：%s", vol, raw)
		}
	}

	// 不存在：400，不是 500。
	status, _, _ = adminReq(t, srv, http.MethodGet, "/api/fs/probe?path="+url.QueryEscape(filepath.Join(root, "没有")), nil)
	if status != http.StatusBadRequest {
		t.Errorf("不存在的目录 status = %d，想要 400", status)
	}

	// 空 path：400。
	status, _, _ = adminReq(t, srv, http.MethodGet, "/api/fs/probe", nil)
	if status != http.StatusBadRequest {
		t.Errorf("空 path status = %d，想要 400", status)
	}
}

// TestStatusCarriesUser 界面第 1 屏要说“这个目录只有 <用户名> 能访问”。
func TestStatusCarriesUser(t *testing.T) {
	srv := newTestServer(t)
	_, body, raw := adminReq(t, srv, http.MethodGet, "/api/status", nil)
	if _, ok := body["user"]; !ok {
		t.Fatalf("status 里没有 user：%s", raw)
	}
}

func TestCurrentUserNameFallsBackToHome(t *testing.T) {
	// 环境变量被清干净时用家目录的名字兜底，反正只是给人看的一个称呼。
	old := os.Getenv("USERNAME")
	t.Setenv("USERNAME", "")
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "")
	if got := currentUserName(); got != "" {
		t.Logf("这台机器上兜底拿到了 %q", got)
	}
	t.Setenv("USERNAME", old)
}

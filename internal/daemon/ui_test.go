package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// 没构建过 UI 时，`GET /` 要给出一个能看懂的占位页，而不是 404 或空白。
//
// 这个断言只能在“没构建过”的仓库上跑：`internal/daemon/static/` 一旦有产物，
// 服务会直接吐真界面。所以有产物就跳过，占位页本身由 TestUIFallbackHTML 盯。
func TestUIServesFallbackPage(t *testing.T) {
	if _, err := staticFS.ReadFile("static/index.html"); err == nil {
		t.Skip("这个工作区已经构建过界面，占位页路径不会被走到")
	}

	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: true})

	status, _, raw := doRequest(t, srv, "/", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d（UI 不该被访问密码挡住，否则用户连关掉密码都进不去）", status)
	}
	if !strings.Contains(raw, "界面还没有构建") {
		t.Errorf("占位页内容不对：%s", raw)
	}
}

// 占位页的文案与命令：用户照着这三行就能把界面构建出来。
func TestUIFallbackHTML(t *testing.T) {
	for _, want := range []string{"界面还没有构建", "pnpm install", "pnpm build", "internal/daemon/static/"} {
		if !strings.Contains(uiFallbackHTML, want) {
			t.Errorf("占位页里应该看到 %q", want)
		}
	}
}

// 打开页面要下管理 cookie：UI 之后靠它调 /api/*（ADR-015）。
func TestUISetsAdminCookie(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	req, _ := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var got *http.Cookie
	for _, c := range resp.Cookies() {
		if c.Name == AdminCookieName {
			got = c
		}
	}
	if got == nil {
		t.Fatal("没有下发 admin cookie")
	}
	if got.Value != testAdminToken {
		t.Errorf("cookie 值不对：%q", got.Value)
	}
	if !got.HttpOnly {
		t.Error("管理 cookie 必须是 HttpOnly（JS 不能读）")
	}
	if got.SameSite != http.SameSiteStrictMode {
		t.Errorf("SameSite = %v，必须是 Strict", got.SameSite)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
}

// 缺失的静态资源就是 404，不要回落到 index.html（那会让前端拿到一张 HTML 当 JS）。
func TestUIMissingAssetIs404(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	status, _, _ := doRequest(t, srv, "/assets/nope.js", nil)
	if status != http.StatusNotFound {
		t.Errorf("status = %d，想要 404", status)
	}
}

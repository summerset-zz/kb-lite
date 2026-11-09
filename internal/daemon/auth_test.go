package daemon

import (
	"net/http"
	"strings"
	"testing"
)

// 读取密码默认关时，检索接口不带任何凭据也能用（ADR-015）。
func TestReadSurfaceOpenWhenTokenOff(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	status, body, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	if body["ok"] != true {
		t.Errorf("ok = %v", body["ok"])
	}
}

// 开关打开后，不带密码就是 401。
func TestReadSurfaceRequiresTokenWhenOn(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: true})

	status, _, _ := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if status != http.StatusUnauthorized {
		t.Errorf("无密码时 status = %d，想要 401", status)
	}

	// 带上就正常。
	status, _, raw := doRequest(t, srv, "/kb/%E7%AC%94%E8%AE%B0/search?q=%E4%B8%8A%E7%BA%BF", nil)
	if status != http.StatusOK {
		t.Errorf("带密码时 status = %d：%s", status, raw)
	}
}

// skill.md 要跟着密码开关变：不能留一个空的 `Bearer `。
func TestSkillMatchesTokenMode(t *testing.T) {
	open := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})
	_, _, raw := doRequest(t, open, "/skill.md", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if strings.Contains(raw, "Authorization") || strings.Contains(raw, "Bearer") {
		t.Errorf("没启用密码时文档里不该出现鉴权头：\n%s", raw)
	}
	if !strings.Contains(raw, "没有启用访问密码") {
		t.Errorf("应当明确说明不需要密码：\n%s", raw)
	}
	// 命令示例仍然是完整可执行的（不能只剩半行 curl）。
	if !strings.Contains(raw, "curl -s \\") {
		t.Errorf("命令示例不该被拼坏：\n%s", raw)
	}

	locked := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: true})
	_, _, raw = doRequest(t, locked, "/skill.md", nil)
	if !strings.Contains(raw, `Authorization: Bearer `+testToken) {
		t.Error("启用密码时文档里必须带上那把密码")
	}
}

// 关掉读取密码**不能**顺带把管理面也打开。
func TestAdminStillLockedWhenReadOpen(t *testing.T) {
	srv := newTestServerWith(t, testServerOpts{features: DefaultFeatures, requireToken: false})

	status, _, _ := doRequest(t, srv, "/api/status", func(r *http.Request) {
		r.Header.Del("Authorization")
	})
	if status != http.StatusUnauthorized {
		t.Errorf("读取密码关着时 /api/status 仍然要 401，实际 %d", status)
	}

	status, _, raw := doRequest(t, srv, "/api/status", func(r *http.Request) {
		r.Header.Set("X-KB-Admin", testAdminToken)
		r.Header.Set("Origin", srv.URL)
	})
	if status != http.StatusOK {
		t.Errorf("带管理凭据 + 同源信号时 status = %d：%s", status, raw)
	}
}

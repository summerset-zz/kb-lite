package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// client 是到本地 daemon 的薄客户端。
//
// token 是读取面的访问密码，可能为空（没启用时）。apiToken 是管理面凭据，只在调 /api/*
// 时用，**永远不打印**（ADR-015）。
type client struct {
	base     string
	token    string
	apiToken string
	hc       *http.Client
}

// daemonDownError 对上 docs/05 的退出码 4。
type daemonDownError struct{ base string }

func (e daemonDownError) Error() string {
	return "连不上 " + e.base + " —— daemon 没在跑？先执行 kb-lite serve"
}

// httpError 是服务端回的 4xx。
type httpError struct {
	status int
	code   string
	msg    string
}

func (e httpError) Error() string {
	if e.msg == "" {
		return fmt.Sprintf("HTTP %d %s", e.status, e.code)
	}
	return e.msg
}

// readToken 读读取面的访问密码。没有文件就是“没启用访问密码”，不是错误。
func readToken(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, daemon.ReadTokenFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// readAPIToken 读管理面凭据。它不交给用户，只给 CLI 用（ADR-015）。
func readAPIToken(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, daemon.APITokenFileName))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

func newClient() (*client, error) {
	dir, err := daemon.ConfigDir()
	if err != nil {
		return nil, err
	}
	cfg, err := daemon.Load(dir)
	if err != nil {
		return nil, err
	}
	return &client{
		base:     "http://127.0.0.1:" + strconv.Itoa(cfg.Port),
		token:    readToken(dir),
		apiToken: readAPIToken(dir),
		hc:       &http.Client{Timeout: 60 * time.Second},
	}, nil
}

func (c *client) get(path string, q url.Values) ([]byte, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent())
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return nil, daemonDownError{base: c.base}
		}
		return nil, err
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode >= 400 {
		var er struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(body, &er)
		return body, httpError{status: resp.StatusCode, code: er.Error.Code, msg: er.Error.Message}
	}
	return body, nil
}

// userAgent 让服务端能认出“这次是命令行在调”。
//
// 不设的话 Go 会发 `Go-http-client/1.1`，跟任何别的 Go 程序长得一样。
// 访问日志里的“方式”一列就是按它分的（docs/04 的 `/api/logs`）。
func userAgent() string { return "kb-lite/" + daemon.Version }

// kbPath 拼 `/kb/<name>/...`。KB 名字里可能有中文和空格，必须按段转义。
func kbPath(name string, tail string) string {
	return "/kb/" + url.PathEscape(name) + "/" + tail
}

// admin 发一次管理面请求（`/api/*`）。
//
// 管理面要 admin 凭据，还要求同源信号：CLI **主动**带 `Origin`。
// curl 默认两个都不带，所以 agent 手里那把读取密码打不开管理面（ADR-015）。
func (c *client) admin(method, path string, payload any) ([]byte, error) {
	if c.apiToken == "" {
		// 管理凭据只有 serve 会生成。没有它就等于“服务还没起来过”，
		// 按 docs/05 的退出码 4 处理，而不是报一个泛泛的错误。
		return nil, daemonDownError{base: c.base}
	}
	var body io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-KB-Admin", c.apiToken)
	req.Header.Set("Origin", c.base)
	req.Header.Set("User-Agent", userAgent())
	if payload != nil {
		req.Header.Set("Content-Type", "application/json; charset=utf-8")
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			return nil, daemonDownError{base: c.base}
		}
		return nil, err
	}
	defer resp.Body.Close()

	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, readErr
	}
	if resp.StatusCode >= 400 {
		var er struct {
			Error struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &er)
		return raw, httpError{status: resp.StatusCode, code: er.Error.Code, msg: er.Error.Message}
	}
	return raw, nil
}

// escapeRelPath 把相对路径按段转义（保留 `/` 作为分隔）。
func escapeRelPath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(p, "/")
	for i, s := range parts {
		parts[i] = url.PathEscape(s)
	}
	return strings.Join(parts, "/")
}

// fail 把错误翻译成 docs/05 的退出码。
func fail(err error) int {
	var down daemonDownError
	if errors.As(err, &down) {
		fmt.Fprintln(os.Stderr, down.Error())
		return exitDaemonDown
	}
	var he httpError
	if errors.As(err, &he) {
		fmt.Fprintln(os.Stderr, he.Error())
		if he.status == http.StatusNotFound {
			return exitNotFound
		}
		return exitError
	}
	fmt.Fprintln(os.Stderr, err)
	return exitError
}

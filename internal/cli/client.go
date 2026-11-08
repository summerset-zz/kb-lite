package cli

import (
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
type client struct {
	base  string
	token string
	hc    *http.Client
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

func readToken(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, daemon.TokenFileName))
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
	tok := readToken(dir)
	if tok == "" {
		return nil, errors.New("还没有 token —— 先运行一次 kb-lite serve，它会生成")
	}
	return &client{
		base:  "http://127.0.0.1:" + strconv.Itoa(cfg.Port),
		token: tok,
		hc:    &http.Client{Timeout: 60 * time.Second},
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
	req.Header.Set("Authorization", "Bearer "+c.token)

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

// kbPath 拼 `/kb/<name>/...`。KB 名字里可能有中文和空格，必须按段转义。
func kbPath(name string, tail string) string {
	return "/kb/" + url.PathEscape(name) + "/" + tail
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

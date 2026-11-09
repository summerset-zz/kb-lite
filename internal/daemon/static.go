package daemon

import (
	"embed"
	"net/http"
	"path"
	"strings"
)

// 静态资源来自 `web/dist`（构建时由 `make` / 脚本拷进来）。
//
// 用 `all:` 前缀是为了让「只有 .gitkeep」的目录也能编过 —— 仓库里不提交构建产物，
// 所以别人 clone 下来直接 `go build` 时这里必须是合法的。
//
//go:embed all:static
var staticFS embed.FS

const uiFallbackHTML = `<!doctype html>
<html lang="zh-CN">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>KBLite</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 15px/1.7 system-ui, "Segoe UI", sans-serif; margin: 0; padding: 10vh 8vw; }
  code { background: color-mix(in oklab, currentColor 10%, transparent); padding: 2px 6px; border-radius: 4px; }
  pre { background: color-mix(in oklab, currentColor 8%, transparent); padding: 12px 16px; border-radius: 8px; overflow: auto; }
  h1 { font-size: 22px; font-weight: 600; margin: 0 0 12px; }
  p { margin: 0 0 12px; max-width: 60ch; }
  .muted { opacity: .7; }
</style>
</head>
<body>
  <h1>界面还没有构建</h1>
  <p>服务本身已经在运行。这个页面是给开发者看的占位页，正式界面需要先构建一次。</p>
  <pre>cd web
pnpm install
pnpm build</pre>
  <p class="muted">构建产物会被拷到 <code>internal/daemon/static/</code>，再重新编译 <code>kb-lite</code> 就会被打进二进制里。
  用 <code>curl &lt;地址&gt;/skill.md</code> 可以先让 AI 用起来。</p>
</body>
</html>
`

// handleUI 托管 UI：`/` 与它的静态资源。
//
// 同源页面靠这里下发的 `kb-admin` cookie 调 `/api/*`（ADR-015）。页面本身不含任何凭据：
// 密码关着时没什么可放，开着时那把也不该出现在 HTML 里。
func (h *Handler) handleUI(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimPrefix(r.URL.Path, "/")
	if name == "" {
		name = "index.html"
	}
	// 只允许读嵌入文件里的普通名字，挡住 `..` 之类。
	if strings.Contains(name, "..") || strings.HasPrefix(name, "/") {
		http.NotFound(w, r)
		return
	}

	data, err := staticFS.ReadFile("static/" + name)
	if err != nil {
		if name == "index.html" {
			h.serveFallback(w)
			return
		}
		http.NotFound(w, r)
		return
	}

	if name == "index.html" {
		// 每次打开页面都刷一次 cookie：用户可能刚换过端口或清过 cookie。
		h.SetAdminCookie(w)
		w.Header().Set("Cache-Control", "no-store")
	} else {
		// 带哈希的资源可以长缓存；不带哈希的保守一点。
		if strings.Contains(path.Base(name), ".") && len(path.Base(name)) > 20 {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
	}
	w.Header().Set("Content-Type", contentTypeOf(name))
	_, _ = w.Write(data)
}

func (h *Handler) serveFallback(w http.ResponseWriter) {
	h.SetAdminCookie(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(uiFallbackHTML))
}

func contentTypeOf(name string) string {
	switch strings.ToLower(path.Ext(name)) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".ico":
		return "image/x-icon"
	case ".woff2":
		return "font/woff2"
	case ".json", ".webmanifest":
		return "application/json; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

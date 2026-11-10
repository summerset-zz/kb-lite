package daemon

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// 访问日志（docs/04 的 `/api/logs`）。
//
// 记的是“谁、用什么方式、干了什么”，只存在内存里，进程重启就空。
// 为什么不落盘：写盘要处理轮转、权限与并发，而界面上要回答的问题只有一个，
// “刚才 AI 到底有没有来查”，一次会话内的环形缓冲足够。
//
// 三条必须守住的规则：
//  1. **不记请求体、不记任何凭据**。搜索词可以进日志（前端会提示隐私），
//     但 token 与 frontmatter 之类的内容一个字节都不进。
//  2. **轮询不记**。界面在整理期间每秒拉 `/api/status`，记了会把日志刷成一片噪声。
//  3. **管理面只记有副作用的**。打开一次设置页会产生五六个 GET，那些没有记录价值。

// maxAccessEntries 是环形缓冲的容量。
//
// 一条大约 200 字节，2 万条约 4 MB。这个量级能装下“一次整理 + 几百次检索”的全过程，
// 而不会把内存当缓存用。
const maxAccessEntries = 20000

// 客户端类型。other 时详情由 UA 字段表达。
const (
	ClientCLI     = "cli"
	ClientCurl    = "curl"
	ClientBrowser = "browser"
	ClientOther   = "other"
	ClientUnknown = "unknown"
)

// AccessEntry 是访问日志的一行。
type AccessEntry struct {
	Seq int64 `json:"seq"`
	// At 是毫秒时间戳。
	At   int64  `json:"at"`
	IP   string `json:"ip"`
	Self bool   `json:"self"`
	// Host 是反查到的设备名，尽力而为，拿不到就是空。
	Host string `json:"host,omitempty"`
	// Client 是 cli / curl / browser / other / unknown。
	Client string `json:"client"`
	// UA 是原始 User-Agent，截断到 80 字符。Client 为 other 时界面直接显示它。
	UA     string `json:"ua,omitempty"`
	Method string `json:"method"`
	Path   string `json:"path"`
	Status int    `json:"status"`
	// MS 是处理耗时（毫秒）。
	MS int64 `json:"ms"`
	// Action 是一句人话，例如“在“笔记”里搜了“部署 回滚””。
	Action string `json:"action"`
}

// accessRing 是定长的环形缓冲。满了就覆盖最旧的，并记一次 dropped。
type accessRing struct {
	mu   sync.Mutex
	buf  []AccessEntry
	head int   // 下一个写入位置
	n    int   // 已有条数
	seq  int64 // 单调递增，**清空也不重置**（否则前端拿着的 since 会把新条目全过滤掉）
	// dropped 是被覆盖掉的老条目数，前端可以据此说“更早的已经看不到了”。
	dropped int64
}

func newAccessRing(capacity int) *accessRing {
	if capacity <= 0 {
		capacity = maxAccessEntries
	}
	return &accessRing{buf: make([]AccessEntry, capacity)}
}

// add 写入一条，返回它的序号。
func (a *accessRing) add(e AccessEntry) int64 {
	a.mu.Lock()
	defer a.mu.Unlock()

	a.seq++
	e.Seq = a.seq
	if a.n == len(a.buf) {
		a.dropped++
	}
	a.buf[a.head] = e
	a.head = (a.head + 1) % len(a.buf)
	if a.n < len(a.buf) {
		a.n++
	}
	return e.Seq
}

// list 返回日志。
//
// since <= 0：最近 limit 条（首次打开时用）。
// since > 0：序号比它新的，最多 limit 条；新条目超过 limit 时保留最新的那部分。
func (a *accessRing) list(since int64, limit int) []AccessEntry {
	a.mu.Lock()
	defer a.mu.Unlock()

	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	if a.n == 0 {
		return nil
	}

	out := make([]AccessEntry, 0, limit)
	// 从最旧的开始走，满了就丢自己最前面那条，最终留下最新的 limit 条。
	// 最旧那条在下标 (head - n) 处（head 是“下一个要写的位置”），往回绕一格要加 len。
	for i := 0; i < a.n; i++ {
		idx := (a.head - a.n + i + len(a.buf)) % len(a.buf)
		e := a.buf[idx]
		if since > 0 && e.Seq <= since {
			continue
		}
		out = append(out, e)
		if len(out) > limit {
			out = out[1:]
		}
	}
	return out
}

// latest 返回当前最大序号（前端拿它做下次的 since）。
func (a *accessRing) latest() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.seq
}

// stats 返回容量与被覆盖的条数。
func (a *accessRing) stats() (total int, dropped int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.n, a.dropped
}

// clear 清空。序号不重置，理由见 seq 的注释。
func (a *accessRing) clear() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.n = 0
	a.head = 0
	a.dropped = 0
	for i := range a.buf {
		a.buf[i] = AccessEntry{}
	}
}

// ---- 客户端识别 ----

// clientKind 从 User-Agent 判断“用什么方式”。这是启发式，不是安全判断。
func clientKind(ua string) string {
	l := strings.ToLower(strings.TrimSpace(ua))
	switch {
	case l == "":
		return ClientUnknown
	case strings.HasPrefix(l, "kb-lite/"):
		return ClientCLI
	case strings.HasPrefix(l, "curl/"):
		return ClientCurl
	case strings.Contains(l, "mozilla/"), strings.Contains(l, "applewebkit"):
		return ClientBrowser
	default:
		return ClientOther
	}
}

// ---- 设备名（尽力而为） ----

// hostResolver 缓存 IP 到设备名的反查。查不到就永远是空，不重试打扰。
type hostResolver struct {
	mu      sync.Mutex
	names   map[string]string
	pending map[string]bool
}

func newHostResolver() *hostResolver {
	return &hostResolver{names: make(map[string]string), pending: make(map[string]bool)}
}

// name 取缓存里的设备名，没有就返回空串。**不会阻塞**。
func (hr *hostResolver) name(ip string) string {
	hr.mu.Lock()
	defer hr.mu.Unlock()
	return hr.names[ip]
}

// lookup 异步查一次，同一个 IP 只查一次。
//
// 为什么必须异步：反向 DNS 在没有配 DNS 的局域网里会卡满超时，
// 放在请求路径上等于给每一次 AI 的检索都加一秒。
func (hr *hostResolver) lookup(ip string) {
	hr.mu.Lock()
	if _, ok := hr.names[ip]; ok {
		hr.mu.Unlock()
		return
	}
	if hr.pending[ip] {
		hr.mu.Unlock()
		return
	}
	hr.pending[ip] = true
	hr.mu.Unlock()

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
		defer cancel()
		names, err := net.DefaultResolver.LookupAddr(ctx, ip)

		hr.mu.Lock()
		defer hr.mu.Unlock()
		delete(hr.pending, ip)
		if err != nil || len(names) == 0 {
			// 记空串：下次不再查，免得每次都等一遍超时。
			hr.names[ip] = ""
			return
		}
		hr.names[ip] = strings.TrimSuffix(names[0], ".")
	}()
}

// ---- 中间件 ----

// statusRecorder 记下状态码，其余行为原样透传。
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// logAccess 是最外层的中间件：**被 guard 拒掉的请求也要进日志**。
// “AI 连不上”的排查全靠这一条，401 与 503 恰恰是最有价值的两行。
func (h *Handler) logAccess(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !shouldRecord(r) {
			next.ServeHTTP(w, r)
			return
		}

		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)

		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		ip := clientKey(r)
		self := isLoopbackIP(ip)
		if !self {
			h.hosts.lookup(ip)
		}
		ua := r.UserAgent()
		if len(ua) > 80 {
			ua = ua[:80]
		}

		h.access.add(AccessEntry{
			At:     start.UnixMilli(),
			IP:     ip,
			Self:   self,
			Client: clientKind(ua),
			UA:     ua,
			Method: r.Method,
			Path:   r.URL.RequestURI(),
			Status: status,
			MS:     time.Since(start).Milliseconds(),
			Action: h.humanAction(r, status),
		})
	})
}

// shouldRecord 决定一条请求进不进日志。
func shouldRecord(r *http.Request) bool {
	p := r.URL.Path
	switch p {
	case "/skill.md", "/authoring.md", "/kb":
		return true
	}
	if strings.HasPrefix(p, "/kb/") {
		return true
	}
	// 到这里剩下的都是 UI 静态资源或管理面。UI 本身不记（打开一次页面会拉一串）。
	if !isAdminPath(p) {
		return false
	}
	return adminSideEffect(r.Method, p)
}

// adminSideEffect 判断一个管理面请求值不值得记。
//
// 只记会改变状态的：建库、删库、改设置、装 SKILL、暂停、建目录之类。
// 读操作（列库、取状态、搜索调试、预览、检查更新）全部跳过，
// 否则打开一次设置页就能刷出五六行没有信息量的东西。
func adminSideEffect(method, p string) bool {
	switch method {
	case http.MethodPost, http.MethodPatch, http.MethodDelete:
	default:
		return false
	}
	switch p {
	case "/api/search", "/api/preview", "/api/update", "/api/logs", "/api/logs/clear":
		return false
	}
	return true
}

func isLoopbackIP(ip string) bool {
	parsed := net.ParseIP(ip)
	return parsed != nil && parsed.IsLoopback()
}

// ---- 人话 ----

// humanAction 把一次请求翻成一句能直接给人看的话。
//
// 状态码优先：认证失败、暂停、跨站这几种情况，说“他在查什么”没有意义，
// 说“为什么被挡”才是用户要的。
//
// 文案按 docs/11：不用角括号，引号用“”，一句话说清一件事。
func (h *Handler) humanAction(r *http.Request, status int) string {
	switch status {
	case http.StatusUnauthorized:
		if isAdminPath(r.URL.Path) {
			return "认证失败（管理凭据不对）"
		}
		return "认证失败（访问密码不对）"
	case http.StatusForbidden:
		return "跨站或跨源请求，被拒绝"
	case http.StatusMisdirectedRequest:
		return "Host 不对，被拒绝"
	case http.StatusTooManyRequests:
		return "失败次数太多，被临时挡住"
	case http.StatusServiceUnavailable:
		if h.paused.Load() {
			return "服务暂停期间被访问"
		}
		return "知识库处于关闭状态"
	}

	segs := pathSegments(r.URL.Path)
	switch {
	case len(segs) == 0:
		return "打开了界面"
	case segs[0] == "skill.md":
		return "取了说明书"
	case segs[0] == "authoring.md":
		return "取了整理说明"
	case segs[0] == "kb":
		return humanKBAction(segs[1:], r.URL.Query())
	case segs[0] == "api":
		return humanAdminAction(segs[1:], r)
	}
	return r.Method + " " + r.URL.Path
}

// quote 把名字包进中文引号。名字里本身带引号时退回原样，免得括号配对看起来崩。
func quote(s string) string {
	if strings.ContainsAny(s, "“”") {
		return s
	}
	return "“" + s + "”"
}

// humanKBAction 处理 `/kb…`。
func humanKBAction(segs []string, q url.Values) string {
	if len(segs) == 0 {
		return "问有哪些知识库"
	}
	kb := quote(segs[0])
	rest := segs[1:]
	if len(rest) == 0 {
		return "访问了" + kb + "的根路径"
	}
	switch rest[0] {
	case "search":
		terms := q["q"]
		if len(terms) == 0 {
			return "在" + kb + "里搜了一次"
		}
		return "在" + kb + "里搜了" + quote(truncateRunesSafe(joinQuery(terms), 60))
	case "manifest":
		return "取了一次" + kb + "的概览"
	case "report":
		return "核对了" + kb + "的收录结果"
	case "check":
		return "做了入库前检查"
	case "list":
		return "列了" + kb + "的条目"
	case "entry":
		path := strings.Join(rest[1:], "/")
		switch {
		case path == "":
			return "读了" + kb + "里的一篇"
		case strings.HasSuffix(path, "/toc"):
			return "看了" + quote(strings.TrimSuffix(path, "/toc")) + "的章节目录"
		case strings.HasSuffix(path, "/section"):
			n := q.Get("n")
			if n == "" {
				return "读了" + quote(strings.TrimSuffix(path, "/section")) + "的一节"
			}
			return "读了" + quote(strings.TrimSuffix(path, "/section")) + "的第 " + n + " 节"
		case strings.HasSuffix(path, "/nearby"):
			return "看了" + quote(strings.TrimSuffix(path, "/nearby")) + "附近的几节"
		default:
			return "读了" + quote(path)
		}
	}
	return "访问了" + kb + "的 " + rest[0]
}

// humanAdminAction 处理管理面。能走到这里的只有有副作用的请求。
func humanAdminAction(segs []string, r *http.Request) string {
	if len(segs) == 0 {
		return "调用了管理接口"
	}
	switch segs[0] {
	case "kbs":
		if len(segs) == 1 {
			return "新建了知识库"
		}
		kb := quote(segs[1])
		if len(segs) == 2 {
			switch r.Method {
			case http.MethodDelete:
				return "移除了知识库" + kb
			case http.MethodPatch:
				return "改了知识库" + kb + "的设置"
			}
			return "改了知识库" + kb
		}
		switch segs[2] {
		case "reindex":
			return "让" + kb + "重新整理"
		case "enable":
			return "启用了" + kb
		case "disable":
			return "关闭了" + kb
		}
		return "对" + kb + "做了一次操作"
	case "settings":
		return "改了设置"
	case "pause":
		return "暂停了服务"
	case "resume":
		return "恢复了服务"
	case "skill":
		return "安装了说明书"
	case "auth":
		return "换了访问密码"
	case "startup":
		return "改了开机自启"
	case "shortcut":
		return "建了桌面快捷方式"
	case "fs":
		if len(segs) > 1 && segs[1] == "mkdir" {
			return "新建了一个文件夹"
		}
		return "打开了文件管理器"
	case "shutdown":
		return "退出了 kb-lite"
	}
	return r.Method + " " + r.URL.Path
}

// pathSegments 拆路径并把每一段解码。中文知识库名在 URL 里是百分号编码的，
// 直接塞给人看会是一串 %E7%AC%94。
func pathSegments(p string) []string {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return nil
	}
	parts := strings.Split(p, "/")
	for i, s := range parts {
		if u, err := url.PathUnescape(s); err == nil {
			parts[i] = u
		}
	}
	return parts
}

// joinQuery 把多值查询拼成一句，用于搜索词显示。
func joinQuery(values []string) string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return strings.Join(out, " ")
}

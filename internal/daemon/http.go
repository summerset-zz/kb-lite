package daemon

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// 响应体积上限（docs/04）。
const (
	maxQueryLen       = 200
	maxQueries        = 5
	maxLimit          = 10
	defaultLimit      = 5
	maxSectionCharsC  = 20000
	maxEntryContent   = 200 * 1024
	authFailureWindow = time.Minute
	authFailureMax    = 10
)

// HandlerOptions 是 Handler 的构造参数。
type HandlerOptions struct {
	// Port 是**实际**监听端口（端口被占时会 +1，所以不能从配置里读）。
	Port int
	// LANExposed 表示用户显式绑到了非回环地址。
	LANExposed bool
	// Features 控制哪些可选路由存在（目前只有 "manifest"）。
	// /skill.md 的样子由它决定 —— 没实现的东西不能在文档里出现（docs/04 末段）。
	Features map[string]bool
	// DBMode 是 "sqlite" | "memory"，只用于 /api/status 报告。
	DBMode string
}

// Handler 是全部 HTTP 行为的入口。
type Handler struct {
	mgr      *Manager
	token    string
	opts     HandlerOptions
	failures *throttle
}

// NewHandler 建一个 Handler。
func NewHandler(mgr *Manager, token string, opts HandlerOptions) *Handler {
	if opts.Port == 0 {
		opts.Port = DefaultPort
	}
	if opts.DBMode == "" {
		// 没显式说就是内存模式（测试与 --no-db 都是这样）。
		opts.DBMode = "memory"
	}
	return &Handler{
		mgr:      mgr,
		token:    token,
		opts:     opts,
		failures: newThrottle(authFailureWindow, authFailureMax),
	}
}

// Routes 返回挂好中间件的 handler。
func (h *Handler) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /skill.md", h.handleSkill)
	mux.HandleFunc("GET /api/status", h.handleStatus)
	// 只注册「一个子树」再由自己分发：Go 的 `{path...}` 只能出现在模式末尾，
	// 而我们要同时有 `/entry/{path}` 和 `/entry/{path}/section` 两种形态。
	mux.HandleFunc("GET /kb/{name}/", h.handleKB)
	return h.guard(mux)
}

// ---- 中间件（docs/07）----

func (h *Handler) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 1. Host 校验：这是 DNS rebinding 的**唯一**有效防线（Origin 可以伪造，Host 不行）。
		if !h.hostAllowed(r.Host) {
			writeError(w, http.StatusMisdirectedRequest, "bad_host",
				"Host 不在允许列表里（只接受 127.0.0.1 / localhost / [::1] + 本服务端口）。")
			return
		}
		// 2. 廉价的跨站加固（有这个头时）。
		if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
			writeError(w, http.StatusForbidden, "cross_site", "不接受跨站请求。")
			return
		}
		// 3. Origin 校验：带了 Origin 且不是自己 → 跨源。
		if origin := r.Header.Get("Origin"); origin != "" && !h.originAllowed(origin) {
			writeError(w, http.StatusForbidden, "bad_origin", "不接受跨源请求。")
			return
		}
		// 4. token。
		if !h.tokenOK(r) {
			if retry := h.failures.fail(clientKey(r)); retry > 0 {
				w.Header().Set("Retry-After", strconv.Itoa(retry))
				writeError(w, http.StatusTooManyRequests, "too_many_failures",
					"认证失败次数过多，稍后再试。")
				return
			}
			writeError(w, http.StatusUnauthorized, "unauthorized", "缺少或错误的 token。")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// hostAllowed 只认本机的三种写法。
//
// ⚠️ `--host 0.0.0.0`（LANExposed）时这里**放行任意 Host** —— 用户已经显式打开了局域网访问，
// 再卡死 Host 会让这个开关变成没用的摆设；代价是失去 DNS rebinding 防线，所以那种模式下
// 启动时会打印醒目警告（docs/07 第 1 条）。
func (h *Handler) hostAllowed(host string) bool {
	if h.opts.LANExposed {
		return true
	}
	for _, ok := range h.selfHosts() {
		if strings.EqualFold(host, ok) {
			return true
		}
	}
	return false
}

func (h *Handler) selfHosts() []string {
	port := strconv.Itoa(h.opts.Port)
	return []string{"127.0.0.1:" + port, "localhost:" + port, "[::1]:" + port}
}

func (h *Handler) originAllowed(origin string) bool {
	if h.opts.LANExposed {
		return true
	}
	for _, host := range h.selfHosts() {
		if strings.EqualFold(origin, "http://"+host) {
			return true
		}
	}
	return false
}

func (h *Handler) tokenOK(r *http.Request) bool {
	tok := ""
	if v := r.Header.Get("Authorization"); strings.HasPrefix(v, "Bearer ") {
		tok = strings.TrimSpace(v[len("Bearer "):])
	}
	if tok == "" {
		tok = strings.TrimSpace(r.Header.Get("X-KB-Token"))
	}
	if tok == "" || h.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(tok), []byte(h.token)) == 1
}

func clientKey(r *http.Request) string {
	addr := r.RemoteAddr
	if i := strings.LastIndexByte(addr, ':'); i >= 0 {
		return addr[:i]
	}
	return addr
}

// throttle 是认证失败的节流（docs/07 第 6 条）。
//
// 成功的请求**永不计数**，所以正常使用不会被误伤。
type throttle struct {
	mu     sync.Mutex
	window time.Duration
	max    int
	hits   map[string][]time.Time
}

func newThrottle(window time.Duration, max int) *throttle {
	return &throttle{window: window, max: max, hits: make(map[string][]time.Time)}
}

// fail 记一次失败，返回非 0 表示当前应当回 429（值是 Retry-After 秒）。
func (t *throttle) fail(key string) int {
	t.mu.Lock()
	defer t.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-t.window)

	kept := t.hits[key][:0]
	for _, ts := range t.hits[key] {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	kept = append(kept, now)
	t.hits[key] = kept

	if len(kept) < t.max {
		return 0
	}
	retry := kept[0].Add(t.window).Sub(now)
	if retry < time.Second {
		retry = time.Second
	}
	return int(retry/time.Second) + 1
}

// ---- 响应helper ----

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorResponse struct {
	Error errorBody `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	// 不发任何 Access-Control-Allow-*（docs/07 第 3 条）。
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorResponse{Error: errorBody{Code: code, Message: message}})
}

// atoiDefault 解析查询参数里的整数，坏值退回默认（不因此报 400：
// 一个写错的 ?budget= 不值得让调用方去处理错误分支）。
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	if n, err := strconv.Atoi(s); err == nil {
		return n
	}
	return def
}

// ---- 路由分发 ----

func (h *Handler) handleKB(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	rest, ok := remainderAfterKB(r)
	if !ok || rest == "" {
		writeError(w, http.StatusBadRequest, "invalid_path", "路径不完整。")
		return
	}

	kb, ok := h.mgr.KB(name)
	if !ok {
		// 禁用的 KB 单独给一个 code，调用方能据此提示用户开回去。
		for _, st := range h.mgr.States() {
			if st.Name == name && st.State == "disabled" {
				writeError(w, http.StatusServiceUnavailable, "kb_disabled", "这个知识库当前是关闭的。")
				return
			}
		}
		writeError(w, http.StatusNotFound, "kb_not_found", "没有这个知识库："+name)
		return
	}

	seg, tail := splitFirstSegment(rest)
	switch seg {
	case "search":
		h.handleSearch(w, r, kb)
	case "manifest":
		h.handleManifest(w, r, kb)
	case "entry":
		h.handleEntryFamily(w, r, kb, tail)
	default:
		writeError(w, http.StatusNotFound, "not_found", "没有这个接口："+seg)
	}
}

// handleStatus 是 `GET /api/status`（docs/04）。
//
// 它**也要 token**：除了 `/skill.md`（它本身就是把 token 递出去的那张纸）之外，
// 这个服务没有兔认证的路由。
func (h *Handler) handleStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, h.mgr.Status(h.opts.Port, h.opts.DBMode))
}

// handleManifest 是 `GET /kb/{name}/manifest`（docs/04）。
//
// 默认返回**文本**：这个接口存在的意义就是「便宜」，JSON 的括号与键名都是冤枉钱。
func (h *Handler) handleManifest(w http.ResponseWriter, r *http.Request, kb *KBIndex) {
	q := r.URL.Query()
	budget := atoiDefault(q.Get("budget"), 0)
	depth := atoiDefault(q.Get("depth"), 0)
	m := BuildManifest(kb, ManifestOptions{Budget: budget, Prefix: q.Get("prefix"), Depth: depth})

	if q.Get("format") == "json" {
		// text 一并给出：UI 与 CLI 要的就是同一份东西，别让它们各写一遍渲染。
		type jsonManifest struct {
			OK bool `json:"ok"`
			Manifest
			Text string `json:"text"`
		}
		writeJSON(w, http.StatusOK, jsonManifest{OK: true, Manifest: m, Text: m.RenderManifestText()})
		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(m.RenderManifestText()))
}

// remainderAfterKB 返回 `/kb/<name>/` 之后的**仍处于转义状态**的路径。
func remainderAfterKB(r *http.Request) (string, bool) {
	escaped := r.URL.EscapedPath()
	const prefix = "/kb/"
	if !strings.HasPrefix(escaped, prefix) {
		return "", false
	}
	tail := escaped[len(prefix):]
	i := strings.IndexByte(tail, '/')
	if i < 0 {
		return "", false
	}
	return tail[i+1:], true
}

func splitFirstSegment(rest string) (head, tail string) {
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		head, tail = rest[:i], rest[i+1:]
	} else {
		head = rest
	}
	if dec, err := url.PathUnescape(head); err == nil {
		head = dec
	}
	return head, tail
}

// handleEntryFamily 处理 `/entry/{path}` 与它的三个子资源。
//
// 子资源用**后缀**判定：被索引的文件只可能是 `.md` / `.markdown` / `.txt`，
// 所以一个以 `/section` 结尾的路径不可能是文件名本身，不会误判。
func (h *Handler) handleEntryFamily(w http.ResponseWriter, r *http.Request, kb *KBIndex, tail string) {
	if tail == "" {
		// `GET /kb/{name}/entry?path=ops/runbook.md` 这种 shell 友好的写法。
		raw := r.URL.Query().Get("path")
		if raw == "" {
			writeError(w, http.StatusBadRequest, "invalid_path", "缺少条目路径。")
			return
		}
		h.handleEntry(w, r, kb, raw)
		return
	}

	for _, sub := range []string{"section", "nearby", "toc"} {
		if strings.HasSuffix(tail, "/"+sub) {
			rel, err := unescapeSegments(strings.TrimSuffix(tail, "/"+sub))
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid_path", "路径解码失败。")
				return
			}
			switch sub {
			case "section":
				h.handleSection(w, r, kb, rel)
			case "nearby":
				h.handleNearby(w, r, kb, rel)
			default:
				h.handleTOC(w, r, kb, rel)
			}
			return
		}
	}

	rel, err := unescapeSegments(tail)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "路径解码失败。")
		return
	}
	h.handleEntry(w, r, kb, rel)
}

func unescapeSegments(s string) (string, error) {
	parts := strings.Split(s, "/")
	for i, p := range parts {
		dec, err := url.PathUnescape(p)
		if err != nil {
			return "", err
		}
		parts[i] = dec
	}
	return strings.Join(parts, "/"), nil
}

func (h *Handler) lookupEntry(w http.ResponseWriter, kb *KBIndex, rel string) (*core.Entry, bool) {
	e, ok := kb.Entry(rel)
	if !ok {
		writeError(w, http.StatusNotFound, "entry_not_found", "这个知识库里没有这个条目："+rel)
		return nil, false
	}
	return e, true
}

// ---- /search ----

type tocEntryDTO struct {
	Section  int    `json:"section"`
	Level    int    `json:"level"`
	Title    string `json:"title"`
	PathText string `json:"pathText"`
	Chars    int    `json:"chars"`
	Long     bool   `json:"long,omitempty"`
}

type searchResultDTO struct {
	Title   string   `json:"title"`
	Path    string   `json:"path"`
	Rev     string   `json:"rev"`
	Score   int      `json:"score"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
	Chars   int      `json:"chars"`
	TOC     any      `json:"toc"`
}

type searchResponse struct {
	OK      bool              `json:"ok"`
	Query   []string          `json:"query"`
	Results []searchResultDTO `json:"results"`
	Debug   any               `json:"debug,omitempty"`
}

func (h *Handler) handleSearch(w http.ResponseWriter, r *http.Request, kb *KBIndex) {
	q := r.URL.Query()

	raw := q["q"]
	if len(raw) > maxQueries {
		writeError(w, http.StatusBadRequest, "invalid_param",
			"q 最多 5 个（超出的会被丢弃，但这里先拒绝，免得你以为都生效了）。")
		return
	}
	queries := make([]*core.Query, 0, len(raw))
	shown := make([]string, 0, len(raw))
	for _, s := range raw {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		if len([]rune(s)) > maxQueryLen {
			writeError(w, http.StatusBadRequest, "invalid_param", "每个 q 不能超过 200 个字符。")
			return
		}
		queries = append(queries, core.Compile(s))
		shown = append(shown, s)
	}
	if len(queries) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_param", "至少给一个 q。")
		return
	}

	limit := defaultLimit
	if v := q.Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			limit = clamp(n, 1, maxLimit)
		}
	}

	format := q.Get("format")
	if format == "" {
		format = "text"
	}
	if format != "text" && format != "json" {
		writeError(w, http.StatusBadRequest, "invalid_param", "format 只能是 text 或 json。")
		return
	}

	withExplain := q.Get("explain") == "1"
	hits := core.Rank(kb.Entries, queries, core.RankOptions{Limit: limit, WithDetails: withExplain})

	resp := searchResponse{OK: true, Query: shown, Results: make([]searchResultDTO, 0, len(hits))}
	for _, hit := range hits {
		e := hit.Entry
		dto := searchResultDTO{
			Title: e.Title, Path: e.RelPath, Rev: e.Rev,
			Score: hit.Score, Summary: e.Summary, Tags: e.Tags, Chars: e.Chars,
		}
		if format == "json" {
			dto.TOC = tocDTOs(e.TOC)
		} else {
			dto.TOC = FormatTOC(e.TOC)
		}
		resp.Results = append(resp.Results, dto)
	}
	if withExplain {
		// 只给 UI 与 CLI 用：它会让回包膨胀几倍，不要出现在给 agent 的调用里。
		type explainRow struct {
			Path    string        `json:"path"`
			Details []core.Detail `json:"details"`
			Hits    [][]core.Hit  `json:"hits"`
		}
		rows := make([]explainRow, 0, len(hits))
		for _, hit := range hits {
			hitsPerQuery := make([][]core.Hit, 0, len(hit.Details))
			for _, d := range hit.Details {
				hitsPerQuery = append(hitsPerQuery, d.Hits)
			}
			rows = append(rows, explainRow{Path: hit.Entry.RelPath, Details: hit.Details, Hits: hitsPerQuery})
		}
		resp.Debug = rows
	}
	writeJSON(w, http.StatusOK, resp)
}

func tocDTOs(entries []core.TocEntry) []tocEntryDTO {
	out := make([]tocEntryDTO, 0, len(entries))
	for _, e := range entries {
		out = append(out, tocEntryDTO{
			Section: e.No, Level: e.Level, Title: e.Title,
			PathText: e.PathText, Chars: e.Chars, Long: e.Long,
		})
	}
	return out
}

// ---- /entry ----

type entryResponse struct {
	OK       bool     `json:"ok"`
	Title    string   `json:"title"`
	Path     string   `json:"path"`
	Rev      string   `json:"rev"`
	Chars    int      `json:"chars"`
	Tags     []string `json:"tags"`
	Summary  string   `json:"summary"`
	Sections int      `json:"sections"`
	TOC      string   `json:"toc"`
	Content  string   `json:"content,omitempty"`
}

func (h *Handler) handleEntry(w http.ResponseWriter, r *http.Request, kb *KBIndex, rel string) {
	e, ok := h.lookupEntry(w, kb, rel)
	if !ok {
		return
	}

	resp := entryResponse{
		OK: true, Title: e.Title, Path: e.RelPath, Rev: e.Rev,
		Chars: e.Chars, Tags: e.Tags, Summary: e.Summary,
		Sections: len(e.Sections), TOC: FormatTOC(e.TOC),
	}

	// 默认**不返回正文**：`?content=1` 才给，而且现读现给，不在内存里存第二份。
	if r.URL.Query().Get("content") == "1" {
		content, err := h.readSource(kb, e.RelPath)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "read_failed", "读不到源文件："+err.Error())
			return
		}
		resp.Content = truncateBytes(content, maxEntryContent)
	}
	writeJSON(w, http.StatusOK, resp)
}

// readSource 从磁盘现读原文并剥掉 frontmatter。
//
// 路径必须过 ResolveEntryPath（docs/07 的越界防护）；文件可能已被删掉，所以 404 是正常的。
func (h *Handler) readSource(kb *KBIndex, rel string) (string, error) {
	abs, err := ResolveEntryPath(kb.KB.RootPath, rel)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return "", err
	}
	text, warn := Decode(data)
	if warn != "" {
		return "", errors.New(warn)
	}
	_, body, _ := core.SplitFrontmatter(text)
	return body, nil
}

func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// 不要切在多字节字符中间。
	cut := max
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// ---- /toc ----

type tocResponse struct {
	OK       bool   `json:"ok"`
	TOC      string `json:"toc"`
	Sections int    `json:"sections"`
	Rev      string `json:"rev"`
}

func (h *Handler) handleTOC(w http.ResponseWriter, r *http.Request, kb *KBIndex, rel string) {
	e, ok := h.lookupEntry(w, kb, rel)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, tocResponse{OK: true, TOC: FormatTOC(e.TOC), Sections: len(e.Sections), Rev: e.Rev})
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

package daemon

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// preview 自己的两个错误。它们的共同点是“用户能自己改”：换个词、换个路径就行。
var (
	errPreviewNoQuery = errors.New("给一个查询词")
	errPreviewBadPath = errors.New("path 必须是知识库里的相对路径，不能以 / 开头，也不能有 ..")
)

// maxPreviewBytes 是“原样给用户看”的上限。超过就截断并明确标注，
// 免得有人点一下就把几 MB 的 manifest 塞进浏览器。
const maxPreviewBytes = 200 * 1024

// previewKinds 是允许代取的路径白名单（docs/06 §5.4）。
//
// 没有这张表，这个接口就是“任意 URL 代理”，等于把 ADR-015 分两把钥匙的意义抹掉了。
var previewKinds = map[string]bool{
	"skill":     true,
	"authoring": true,
	"manifest":  true,
	"search":    true,
	"section":   true,
}

type previewReq struct {
	// Kind 决定取哪一类，见 previewKinds。
	Kind string `json:"kind"`
	// KB 是知识库名（skill / authoring 之外都要）。
	KB string `json:"kb"`
	// Q 是 search 的查询词。
	Q string `json:"q"`
	// Path 是 section 的条目相对路径。
	Path string `json:"path"`
	// N 是 section 的节号，默认 1。
	N int `json:"n"`
}

type previewResp struct {
	OK          bool   `json:"ok"`
	Kind        string `json:"kind"`
	URL         string `json:"url"`
	Status      int    `json:"status"`
	ContentType string `json:"contentType"`
	Body        string `json:"body"`
	Truncated   bool   `json:"truncated,omitempty"`
}

// handlePreview 是 `POST /api/preview`：以 agent 的身份取一次，把原样响应回给界面。
//
// 为什么由服务端代取：界面手上只有管理凭据，拿不到读取密码（ADR-015），
// 自己发一定 401。代取才能让人看到**真的**响应，包括 Content-Type、状态码、暂停时的 503。
func (h *Handler) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req previewReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if !previewKinds[req.Kind] {
		writeError(w, http.StatusBadRequest, "bad_kind",
			"kind 只能是 skill / authoring / manifest / search / section。")
		return
	}

	target, err := h.previewTarget(req)
	if err != nil {
		writePreviewError(w, err)
		return
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
		// 不走系统代理：这台机器上可能配了 HTTP_PROXY，那会把本机回环请求也带走。
		Transport: &http.Transport{Proxy: nil},
	}
	outReq, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	// 用服务端自己那把读取密码：这样拿到的就是“带对了密码的 agent”看到的东西。
	if tok := h.ReadToken(); tok != "" {
		outReq.Header.Set("Authorization", "Bearer "+tok)
	}

	resp, err := client.Do(outReq)
	if err != nil {
		writeError(w, http.StatusBadGateway, "preview_failed", "取不到："+err.Error())
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxPreviewBytes+1))
	truncated := len(body) > maxPreviewBytes
	if truncated {
		body = body[:maxPreviewBytes]
	}

	writeJSON(w, http.StatusOK, previewResp{
		OK:          true,
		Kind:        req.Kind,
		URL:         target,
		Status:      resp.StatusCode,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        string(body),
		Truncated:   truncated,
	})
}

// previewTarget 把 kind 翻成具体路径，顺便把参数校验干净。
func (h *Handler) previewTarget(req previewReq) (string, error) {
	base := "http://127.0.0.1:" + strconv.Itoa(h.Port())

	switch req.Kind {
	case "skill":
		return base + "/skill.md", nil
	case "authoring":
		return base + "/authoring.md", nil
	}

	name := strings.TrimSpace(req.KB)
	if name == "" {
		return "", ErrInvalidName
	}
	if _, ok := h.mgr.KB(name); !ok {
		return "", ErrKBNotFound
	}
	kbPath := "/kb/" + url.PathEscape(name)

	switch req.Kind {
	case "manifest":
		return base + kbPath + "/manifest?budget=800", nil
	case "search":
		q := strings.TrimSpace(req.Q)
		if q == "" {
			return "", errPreviewNoQuery
		}
		return base + kbPath + "/search?q=" + url.QueryEscape(q) + "&format=json", nil
	default: // section
		rel := strings.TrimSpace(req.Path)
		if rel == "" || strings.HasPrefix(rel, "/") || strings.Contains(rel, "..") {
			return "", errPreviewBadPath
		}
		n := req.N
		if n <= 0 {
			n = 1
		}
		esc := strings.Join(escapeSegments(rel), "/")
		return base + kbPath + "/entry/" + esc + "/section?n=" + strconv.Itoa(n), nil
	}
}

// escapeSegments 逐段转义路径。整段 PathEscape 会把 `/` 也转掉，服务端就认不出层级了。
func escapeSegments(rel string) []string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return parts
}

// writePreviewError 把 preview 的参数错误翻成 4xx，并指出该改哪个字段。
func writePreviewError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_param", "先选一个知识库。")
	case errors.Is(err, ErrKBNotFound):
		writeError(w, http.StatusNotFound, "kb_not_found", "没有这个知识库。")
	case errors.Is(err, errPreviewNoQuery):
		writeError(w, http.StatusBadRequest, "invalid_param", "想看检索结果就得给一个查询词。")
	case errors.Is(err, errPreviewBadPath):
		writeError(w, http.StatusBadRequest, "invalid_param", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

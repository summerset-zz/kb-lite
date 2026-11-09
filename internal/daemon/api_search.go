package daemon

import (
	"net/http"
	"strings"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// adminSearchReq 是 `POST /api/search` 的请求体（搜索调试页专用，docs/04）。
//
// 字段与 `/kb/{name}/search` 的查询参数一一对应，只是 `explain` 恒为真：
// 这一页存在的意义就是看解释。
type adminSearchReq struct {
	KB    string   `json:"kb"`
	Q     []string `json:"q"`
	Limit int      `json:"limit"`
}

// handleAdminSearch 是 `POST /api/search`：与 agent 面同一套打分，走管理面。
//
// 为什么不让 UI 直接打 `/kb/{name}/search`：那条要**读取**密码，而界面手上只有
// **管理**凭据（ADR-015 把两把钥匙分开了）。共用 searchPayload，所以两边不会漂。
func (h *Handler) handleAdminSearch(w http.ResponseWriter, r *http.Request) {
	var req adminSearchReq
	if !decodeJSON(w, r, &req) {
		return
	}

	name := strings.TrimSpace(req.KB)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_param", "要看哪个知识库？给一个 kb。")
		return
	}
	kb, ok := h.mgr.KB(name)
	if !ok {
		writeError(w, http.StatusNotFound, "kb_not_found", "没有这个知识库："+name)
		return
	}
	if st, ok := h.mgr.stateOf(name); ok && st.State == "disabled" {
		writeError(w, http.StatusServiceUnavailable, "kb_disabled", "这个知识库当前是关闭的。")
		return
	}

	if len(req.Q) > maxQueries {
		writeError(w, http.StatusBadRequest, "invalid_param", "q 最多 5 个。")
		return
	}
	queries := make([]*core.Query, 0, len(req.Q))
	shown := make([]string, 0, len(req.Q))
	for _, s := range req.Q {
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
	if req.Limit > 0 {
		limit = clamp(req.Limit, 1, maxLimit)
	}

	writeJSON(w, http.StatusOK, searchPayload(kb, queries, shown, limit, true, "json"))
}

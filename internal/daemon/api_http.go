package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// 管理接口的请求与响应（docs/04「管理接口」）。
//
// 这些接口只有 UI 与 CLI 用，**不在 agent 的契约里**：skill.md 一个字都不提它们，
// 而且它们要 admin 凭据（ADR-015）。

type kbListResp struct {
	OK  bool       `json:"ok"`
	KBs []StatusKB `json:"kbs"`
}

type kbWriteReq struct {
	Name     string   `json:"name"`
	RootPath string   `json:"rootPath"`
	Include  []string `json:"include"`
	Ignore   []string `json:"ignore"`
}

type kbCreateResp struct {
	OK bool `json:"ok"`
	StatusKB
	// Warning 是“建成了但有点不对”的情况，比如目录里一个 markdown 都没有。
	Warning string `json:"warning,omitempty"`
}

type reindexReq struct {
	Force bool `json:"force"`
}

type reindexResp struct {
	OK    bool   `json:"ok"`
	State string `json:"state"`
}

type indexResp struct {
	OK        bool            `json:"ok"`
	State     string          `json:"state"`
	Error     string          `json:"error,omitempty"`
	Progress  *StatusProgress `json:"progress,omitempty"`
	StartedAt int64           `json:"startedAt,omitempty"`
}

type removeResp struct {
	OK      bool `json:"ok"`
	Removed struct {
		Entries  int `json:"entries"`
		Sections int `json:"sections"`
	} `json:"removed"`
}

// handleKBList 是 `GET /api/kbs`。
func (h *Handler) handleKBList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, kbListResp{OK: true, KBs: h.mgr.kbStatuses()})
}

// handleKBCreate 是 `POST /api/kbs`：建关联，然后立刻开始首次整理（异步）。
func (h *Handler) handleKBCreate(w http.ResponseWriter, r *http.Request) {
	var req kbWriteReq
	if !decodeJSON(w, r, &req) {
		return
	}
	kb, err := h.mgr.CreateKB(KB{
		Name: req.Name, RootPath: req.RootPath, Include: req.Include, Ignore: req.Ignore,
	})
	if err != nil {
		writeManagerError(w, err)
		return
	}

	resp := kbCreateResp{OK: true, StatusKB: StatusKB{
		Name: kb.Name, ID: kb.ID, RootPath: kb.RootPath, Enabled: kb.Enabled, State: "indexing",
	}}
	// 目录里一个 markdown 都没有：建成功，但要提醒（docs/04）。
	if n := countMarkdown(kb.RootPath, 1); n == 0 {
		resp.Warning = "这个目录里没有找到 markdown 文件"
	}
	writeJSON(w, http.StatusCreated, resp)
}

// handleKBPatch 是 `PATCH /api/kbs/{name}`。
func (h *Handler) handleKBPatch(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var patch KBPatch
	if !decodeJSON(w, r, &patch) {
		return
	}
	needReindex, err := h.mgr.PatchKB(name, patch)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	if needReindex {
		_, _ = h.mgr.ReindexAsync(newNameOr(name, patch), false)
	}
	writeJSON(w, http.StatusOK, h.mgr.kbStatusByName(newNameOr(name, patch)))
}

// handleKBDelete 是 `DELETE /api/kbs/{name}`。
//
// **只删关联与索引条目，绝不碰用户的目录**（ADR-004）。有个测试直接比目录的 md5。
func (h *Handler) handleKBDelete(w http.ResponseWriter, r *http.Request) {
	entries, sections, err := h.mgr.RemoveKB(r.PathValue("name"))
	if err != nil {
		writeManagerError(w, err)
		return
	}
	var resp removeResp
	resp.OK = true
	resp.Removed.Entries = entries
	resp.Removed.Sections = sections
	writeJSON(w, http.StatusOK, resp)
}

// handleKBEnable 处理 `POST /api/kbs/{name}/enable` 与 `/disable`。
func (h *Handler) handleKBEnable(w http.ResponseWriter, r *http.Request, enabled bool) {
	name := r.PathValue("name")
	if err := h.mgr.SetEnabled(name, enabled); err != nil {
		writeManagerError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, h.mgr.kbStatusByName(name))
}

// handleKBReindex 是 `POST /api/kbs/{name}/reindex`。
//
// 已有任务在跑就 409，并把进度带上（调用方据此显示“已经有一个在跑了”）。
func (h *Handler) handleKBReindex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	var req reindexReq
	if r.ContentLength > 0 && !decodeJSON(w, r, &req) {
		return
	}
	already, err := h.mgr.ReindexAsync(name, req.Force)
	if err != nil {
		writeManagerError(w, err)
		return
	}
	if already {
		st, _ := h.mgr.stateOf(h.mgr.Running())
		writeJSON(w, http.StatusConflict, errorResponse{Error: errorBody{
			Code:     "indexing",
			Message:  "已经有一个整理任务在跑了。",
			Progress: &StatusProgress{Done: st.Done, Total: st.Total},
		}})
		return
	}
	writeJSON(w, http.StatusAccepted, reindexResp{OK: true, State: "indexing"})
}

// handleKBIndex 是 `GET /api/kbs/{name}/index`：查询进度。
func (h *Handler) handleKBIndex(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	st, ok := h.mgr.stateOf(name)
	if !ok {
		if _, exists := h.mgr.cfgKB(name); !exists {
			writeManagerError(w, ErrKBNotFound)
			return
		}
		st = KBState{Name: name, State: "ready"}
	}
	resp := indexResp{OK: true, State: st.State, Error: st.Error, StartedAt: st.StartedAt}
	if st.State == "indexing" {
		resp.Progress = &StatusProgress{Done: st.Done, Total: st.Total}
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleFSList 是 `GET /api/fs/list?path=`：给 UI 的目录选择器。
func (h *Handler) handleFSList(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	if p == "" {
		writeError(w, http.StatusBadRequest, "invalid_path", "缺少 path 参数。")
		return
	}
	resp, err := ListDirs(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", "这个目录打不开："+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- 小工具 ----

// decodeJSON 解析请求体。失败时已经写好 400，调用方直接 return。
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_json", "请求体不是合法 JSON："+err.Error())
		return false
	}
	return true
}

// writeManagerError 把 Manager 的业务错误翻成状态码（docs/04 的错误形态）。
func writeManagerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrKBNotFound):
		writeError(w, http.StatusNotFound, "kb_not_found", err.Error())
	case errors.Is(err, ErrKBDuplicate):
		writeError(w, http.StatusConflict, "kb_duplicate", err.Error()+"，换一个名字。")
	case errors.Is(err, ErrInvalidName):
		writeError(w, http.StatusBadRequest, "invalid_name",
			"名字要 1 到 64 个字，且不能含斜杠或控制字符。")
	case errors.Is(err, ErrInvalidRoot):
		writeError(w, http.StatusBadRequest, "invalid_root", "这个目录不存在，或者不是目录。")
	case errors.Is(err, ErrNoConfigDir):
		writeError(w, http.StatusInternalServerError, "no_config_dir", err.Error())
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
}

// newNameOr 返回改名后的名字（没改名就是原名）。
func newNameOr(name string, p KBPatch) string {
	if p.Name != nil && *p.Name != "" {
		return *p.Name
	}
	return name
}

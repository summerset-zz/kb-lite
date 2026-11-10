package daemon

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
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

// authTokenReq 是 `POST /api/auth/token` 的请求体。
//
// action：
//   - `show`（默认）：把当前读取密码回给 UI。它只在管理面上出现，所以不需要额外口令；
//   - `rotate`：换一把新的，旧的立刻失效。
//
// 这两个动作都只碰**读取**密码。管理凭据（api-token）界面上不显示也不可换。
type authTokenReq struct {
	Action string `json:"action"`
}

// handleAuthToken 是 `POST /api/auth/token`（docs/04）。
func (h *Handler) handleAuthToken(w http.ResponseWriter, r *http.Request) {
	var req authTokenReq
	if !decodeJSON(w, r, &req) {
		return
	}
	switch req.Action {
	case "", "show":
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": h.ReadToken()})
	case "rotate":
		dir := h.mgr.ConfigDirPath()
		tok, err := RotateReadToken(dir)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "token_write_failed",
				"新密码没能存下来："+err.Error())
			return
		}
		h.SetReadToken(tok)
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "token": tok, "rotated": true})
	default:
		writeError(w, http.StatusBadRequest, "bad_action", "action 只能是 show 或 rotate。")
	}
}

// handleFSList 是 `GET /api/fs/list?path=`：给 UI 的目录选择器。
//
// path 为空**不是错误**：选择器第一次打开时手上没有任何路径，那时候给它起点
// （Windows 是盘符列表，其它平台是家目录）。早先这里回 400「缺少 path 参数」，
// 界面上的表现就是点了按钮什么都不出来，下面还挂着一句看不懂的话。
func (h *Handler) handleFSList(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	resp, err := ListDirs(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", fsErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleFSProbe 是 `GET /api/fs/probe?path=`：把目录加进知识库之前探一眼。
//
// 不数目录时数（那样每换一层都要走整棵子树），只在用户点“添加”时数一次。
func (h *Handler) handleFSProbe(w http.ResponseWriter, r *http.Request) {
	p := strings.TrimSpace(r.URL.Query().Get("path"))
	probe, err := ProbeDir(p)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_path", fsErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, probe)
}

// handleFSMkdir 是 `POST /api/fs/mkdir`：在指定目录下建一个空文件夹。
//
// 管理面上**唯一**会写用户磁盘的接口，而且只建空目录（docs/04）。
// 界面上的那一幕：用户浏览到 D:\notes，发现想放进去的文件夹还不存在，
// 这时候让他切到资源管理器建完再回来，是最容易放弃的一步。
func (h *Handler) handleFSMkdir(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Path string `json:"path"`
		Name string `json:"name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	full, err := CreateDir(req.Path, req.Name)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "path": full})
	case errors.Is(err, ErrInvalidDirName):
		writeError(w, http.StatusBadRequest, "invalid_name",
			"名字要 1 到 64 个字，且不能含斜杠。")
	case errors.Is(err, ErrPathExists):
		writeError(w, http.StatusConflict, "already_exists", "这个位置已经有同名的文件夹了。")
	case errors.Is(err, ErrInvalidRoot):
		writeError(w, http.StatusBadRequest, "invalid_path", "这个目录不在了，刷新一下再试。")
	default:
		// Windows 的保留名（CON、PRN…）、权限不足之类都走这里，把系统的话原样带回去。
		writeError(w, http.StatusBadRequest, "mkdir_failed", "没能建出来："+err.Error())
	}
}

// handleSettings 是 `POST /api/settings`。
//
// 两种改动的生效方式不一样（docs/04）：
//   - 访问密码：当场生效；
//   - 端口与绑定地址：**当场重开监听**（Reload），响应里回 moved 与新端口，UI 据此跳转。
//
// 没有“换记录文件位置”这回事：它永远住在配置目录里（ADR-018）。
func (h *Handler) handleSettings(w http.ResponseWriter, r *http.Request) {
	var patch SettingsPatch
	if !decodeJSON(w, r, &patch) {
		return
	}

	before := *h.mgr.Config()
	restart, err := h.mgr.ApplySettings(patch)
	if err != nil {
		writeSettingsError(w, err)
		return
	}
	after := *h.mgr.Config()

	// 密码开关当场应用到 Handler：用户点完保存就该生效，不该等重启。
	effective := after.EffectiveRequireToken()
	h.SetRequireToken(effective)

	moved := false
	if after.Host != before.Host || after.Port != before.Port {
		if h.opts.Reload == nil {
			restart = true
		} else {
			actual, err := h.opts.Reload(after.Host, after.Port)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "reload_failed",
					"新端口监听不上："+err.Error()+"。已经改回原来的地址，服务没停。")
				return
			}
			moved = true
			// 已经当场重开成功，就不需要用户再重启了。
			restart = false
			// 端口被占时自动 +1，写回配置，否则下次启动还是撞同一个端口。
			if actual != after.Port {
				_ = h.mgr.UpdateConfig(func(c *Config) error {
					c.Port = actual
					return nil
				})
			}
		}
	}

	cfg := h.mgr.Config()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":              true,
		"restartRequired": restart,
		"moved":           moved,
		"effective": map[string]any{
			"port":         cfg.Port,
			"host":         cfg.Host,
			"requireToken": effective,
			"dbPath":       cfg.DBPath,
		},
	})
}

// handlePaused 处理 `POST /api/pause` 与 `/api/resume`（docs/04）。
//
// 只切换“给 AI 的那一面”：监听、索引、进程、界面全都不动，恢复不需要重新整理。
func (h *Handler) handlePaused(w http.ResponseWriter, r *http.Request, paused bool) {
	h.SetPaused(paused)
	msg := "已恢复，AI 现在可以查了。"
	if paused {
		msg = "服务已经暂停，AI 现在连不上。"
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "paused": paused, "message": msg})
}

// handleShutdown 是 `POST /api/shutdown`：优雅退出（UI 的“退出”按钮与 `kb-lite stop`）。
func (h *Handler) handleShutdown(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	if h.opts.Shutdown != nil {
		// 先回响应再退：否则客户端看到的是连接被断开，以为失败了。
		go func() {
			time.Sleep(50 * time.Millisecond)
			h.opts.Shutdown()
		}()
	}
}

// skillInstallReq 是 `POST /api/skill/install` 的请求体。
type skillInstallReq struct {
	// Kind 是 read（教 agent 怎么查）或 authoring（教 AI 怎么写）。
	Kind string `json:"kind"`
	// Targets 是 dsh / claude，默认只装 dsh。
	Targets []string `json:"targets"`
}

// handleSkillInstall 是 `POST /api/skill/install`。
//
// 两份 SKILL 分开装（ADR-017），落在不同的目录里，互不覆盖。
// skillBase 是这个请求对应的对外地址。
func (h *Handler) skillBase(r *http.Request) string {
	if r.Host != "" {
		return "http://" + r.Host
	}
	return "http://127.0.0.1:" + strconv.Itoa(h.Port())
}

// skillContent 渲染一份要装到用户目录的文档。安装与状态检查共用它，
// 否则“装完立刻显示已是最新”会靠运气。
func (h *Handler) skillContent(kind, base string) string {
	tok := ""
	if h.requireToken.Load() {
		tok = h.ReadToken()
	}
	if kind == "authoring" {
		return RenderAuthoringText(base, tok)
	}
	return RenderSkillText(h.mgr.Index(), base, tok, h.opts.Features)
}

// handleSkillStatus 是 `GET /api/skill/status`：装没装、是不是最新的。
//
// 界面上“安装”的反馈以前离按钮很远（写在下面一段里），而且不装完不知道装没装过。
// 这个接口把四个目标（两种说明书 × 两个目录）的现状一次性给它。
func (h *Handler) handleSkillStatus(w http.ResponseWriter, r *http.Request) {
	base := h.skillBase(r)
	items := make([]SkillState, 0, 4)
	for _, kind := range []string{"read", "authoring"} {
		content := h.skillContent(kind, base)
		for _, target := range []string{"dsh", "claude"} {
			st, err := SkillStateFor(kind, target, content)
			if err != nil {
				continue // 读不了就当没装，不把整个列表拖垮
			}
			items = append(items, st)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "items": items})
}

func (h *Handler) handleSkillInstall(w http.ResponseWriter, r *http.Request) {
	var req skillInstallReq
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Kind == "" {
		req.Kind = "read"
	}
	if req.Kind != "read" && req.Kind != "authoring" {
		writeError(w, http.StatusBadRequest, "bad_kind", "kind 只能是 read 或 authoring。")
		return
	}
	if len(req.Targets) == 0 {
		req.Targets = []string{"dsh"}
	}

	base := h.skillBase(r)
	var content string
	if req.Kind == "authoring" {
		tok := ""
		if h.requireToken.Load() {
			tok = h.ReadToken()
		}
		content = RenderAuthoringText(base, tok)
	} else {
		tok := ""
		if h.requireToken.Load() {
			tok = h.ReadToken()
		}
		content = RenderSkillText(h.mgr.Index(), base, tok, h.opts.Features)
	}

	res := SkillInstallResult{Installed: make([]SkillTarget, 0, len(req.Targets))}
	for _, target := range req.Targets {
		got, err := InstallSkill(req.Kind, target, content)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "install_failed", err.Error())
			return
		}
		got.Kind, got.Target = req.Kind, target
		res.Installed = append(res.Installed, got)
	}
	writeJSON(w, http.StatusOK, res)
}

// writeSettingsError 把设置校验失败翻成 400，并指向具体字段。
func writeSettingsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrInvalidPort):
		writeError(w, http.StatusBadRequest, "invalid_port", "端口要在 1 到 65535 之间。")
	case errors.Is(err, ErrPortInUse):
		writeError(w, http.StatusBadRequest, "port_in_use", "这个端口被别的软件占用了，换一个。")
	default:
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
	}
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

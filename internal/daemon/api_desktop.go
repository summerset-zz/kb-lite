package daemon

import (
	"net/http"
	"os"
)

// ExePath 返回当前进程的可执行文件路径。
//
// 自启项与快捷方式都要记“到底启动哪个文件”，所以必须是真实的 exe 路径，
// 不能是 `go run` 那种临时目录（开发时看到的会是临时路径，这是正常的）。
func ExePath() (string, error) {
	p, err := os.Executable()
	if err != nil {
		return "", err
	}
	return p, nil
}

// handleStartup 是 `GET /api/startup` 与 `POST /api/startup`（docs/04）。
//
// GET 只读；POST 带 `{"enabled": true|false}`。界面上的复选框就是这两条。
func (h *Handler) handleStartup(w http.ResponseWriter, r *http.Request) {
	exe, err := ExePath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}

	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, StartupStatus(exe))
		return
	}

	var req struct {
		Enabled *bool `json:"enabled"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Enabled == nil {
		writeError(w, http.StatusBadRequest, "invalid_param", "给一个 enabled：true 或 false。")
		return
	}
	if err := SetStartup(exe, *req.Enabled); err != nil {
		writeError(w, http.StatusBadRequest, "startup_failed", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, StartupStatus(exe))
}

// shortcutResp 是快捷方式接口的响应：ok 加现状。
type shortcutResp struct {
	OK bool `json:"ok"`
	ShortcutInfo
}

// handleShortcut 是 `GET /api/shortcut` 与 `POST /api/shortcut`。
//
// GET：桌面那个快捷方式现在指向谁（用户换了新版本之后，它很可能还指着旧的）。
// POST：建/重建成指向当前这份 exe。
func (h *Handler) handleShortcut(w http.ResponseWriter, r *http.Request) {
	exe, err := ExePath()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	if r.Method == http.MethodGet {
		writeJSON(w, http.StatusOK, shortcutResp{OK: true, ShortcutInfo: ShortcutStatus(exe)})
		return
	}
	path, err := DesktopShortcut(exe)
	if err != nil {
		writeError(w, http.StatusBadRequest, "shortcut_failed", err.Error())
		return
	}
	info := ShortcutStatus(exe)
	if info.Path == "" {
		info.Path = path
	}
	writeJSON(w, http.StatusOK, shortcutResp{OK: true, ShortcutInfo: info})
}

// handleUpdate 是 `GET /api/update`（docs/04）。
//
// `?refresh=1` 忽略 10 分钟的缓存。查不到时**仍然回 200**，只是 `ok:false` 并带一句
// 人话错误：这不是接口错误，用户要看到的是“这次没查到，可能没联网”。
func (h *Handler) handleUpdate(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("refresh") == "1"
	writeJSON(w, http.StatusOK, FetchUpdate(Version, force))
}

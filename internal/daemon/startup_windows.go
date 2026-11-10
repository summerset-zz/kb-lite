//go:build windows

package daemon

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// autostartKey 是 Windows 上“登录时自动运行”的标准位置（HKCU，不需要管理员）。
const autostartKey = `HKCU\Software\Microsoft\Windows\CurrentVersion\Run`

// AutostartValueName 是写进注册表的值的名字。测试会换成临时名字，避免动到真配置。
const AutostartValueName = "kb-lite"

// StartupInfo 是 `GET /api/startup` 的响应。
type StartupInfo struct {
	// Supported 为假时界面把开关置灰并说明原因。
	Supported bool `json:"supported"`
	Enabled   bool `json:"enabled"`
	// Command 是写进注册表的那条命令，界面上要能看见：
	// “开机自启”到底启动的是什么，用户有权知道。
	Command string `json:"command,omitempty"`
	// Path 是这个开关落在哪（Windows 上是注册表路径），排查时有用。
	Path  string `json:"path,omitempty"`
	Error string `json:"error,omitempty"`
	// ExePath 是注册表里记的那个 exe（与当前进程不一定相同）。
	ExePath string `json:"exePath,omitempty"`
	// Mismatch 为真表示自启指向的是另一份 kb-lite：用户把文件挪了位置、
	// 或者又解压了一份新的。这种时候开机跑的是旧的那个。
	Mismatch bool `json:"mismatch"`
	// Fragile 为真表示当前 exe 在下载/临时目录里：这里的东西随时会被清掉，
	// 开自启很可能下次开机就静默失效。
	Fragile bool `json:"fragile"`
}

// AutostartCommand 是开机时要跑的命令。
//
// 不开 `--open`：开机自动弹一个浏览器标签页属于打扰。托盘会自己出来。
func AutostartCommand(exePath string) string {
	return `"` + exePath + `" serve`
}

// exeFromCommand 从 `"C:\…\kb-lite.exe" serve` 里把路径拿出来。
func exeFromCommand(cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if strings.HasPrefix(cmd, `"`) {
		if i := strings.Index(cmd[1:], `"`); i >= 0 {
			return cmd[1 : 1+i]
		}
		return strings.Trim(cmd, `"`)
	}
	if i := strings.IndexByte(cmd, ' '); i > 0 {
		return cmd[:i]
	}
	return cmd
}

// inVolatileDir 判断 exe 是不是放在下载/临时这类会被清掉的目录里。
//
// 用户从压缩包里直接双击是很常见的用法；这种位置开自启，过一阵子文件被删、
// 系统清理临时目录，开机就静默什么都不发生了。所以要提前提醒一句。
func inVolatileDir(exePath string) bool {
	lower := strings.ToLower(exePath)
	for _, marker := range []string{`\downloads\`, `\temp\`, `\tmp\`, `\appdata\local\temp\`, `/downloads/`, `/tmp/`} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// StartupStatus 读当前是否已经开启开机自启。
func StartupStatus(exePath string) StartupInfo {
	info := StartupInfo{
		Supported: true,
		Path:      autostartKey,
		Command:   AutostartCommand(exePath),
		Fragile:   inVolatileDir(exePath),
	}
	value, err := findRunValue(AutostartValueName)
	if err != nil {
		if errors.Is(err, errRunValueMissing) {
			return info
		}
		info.Error = err.Error()
		return info
	}
	info.Enabled = true
	info.ExePath = exeFromCommand(value)
	info.Mismatch = info.ExePath != "" && !samePath(info.ExePath, exePath)
	return info
}

// samePath 比较两个 exe 路径是不是同一个文件。
//
// 不能只比字符串：Windows 的 8.3 短名（`C:\Users\HUIJI-~1\…`）与长名指的是同一个文件，
// 注册表与 .lnk 里通常存长名，而 `os.Executable()` 可能拿到短名。
// 比之前先各自 realpath 一遍（它会把短名展开成长名）。
func samePath(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	ra, errA := filepath.EvalSymlinks(a)
	rb, errB := filepath.EvalSymlinks(b)
	if errA == nil {
		a = ra
	}
	if errB == nil {
		b = rb
	}
	return strings.EqualFold(a, b)
}

// SetStartup 打开或关闭开机自启。
func SetStartup(exePath string, on bool) error {
	if !on {
		return deleteRunValue(AutostartValueName)
	}
	return setRunValue(AutostartValueName, AutostartCommand(exePath))
}

// ---- 下面是可测试的那一层：值名当参数传进来 ----

var errRunValueMissing = errors.New("没有这个启动项")

// findRunValue 查一个值。用 reg.exe 而不是再引一个注册表库：
// 注册表 API 要么进 x/sys（多一个依赖），要么手搓 syscall（容易出错），
// 而这台机器上一定有 reg.exe。
func findRunValue(name string) (string, error) {
	out, err := runReg("query", autostartKey, "/v", name)
	if err != nil {
		if isRegNotFound(err) {
			return "", errRunValueMissing
		}
		return "", err
	}
	// 输出形如：`    kb-lite    REG_SZ    "C:\…\kb-lite.exe" serve`
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], name) {
			continue
		}
		if i := strings.Index(strings.ToUpper(line), "REG_SZ"); i >= 0 {
			return strings.TrimSpace(line[i+len("REG_SZ"):]), nil
		}
	}
	return "", errRunValueMissing
}

func setRunValue(name, value string) error {
	_, err := runReg("add", autostartKey, "/v", name, "/t", "REG_SZ", "/d", value, "/f")
	return err
}

func deleteRunValue(name string) error {
	_, err := runReg("delete", autostartKey, "/v", name, "/f")
	if err != nil && isRegNotFound(err) {
		return nil // 本来就没有，当成成功
	}
	return err
}

func runReg(args ...string) (string, error) {
	cmd := exec.Command("reg.exe", args...)
	// Windows 下别弹黑框：这个进程本身就没有控制台窗口时尤其明显。
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("reg %s 失败：%s", args[0], strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// isRegNotFound 认 reg.exe 的“找不到”输出（它不一定给非零退出码，但都给这句）。
func isRegNotFound(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unable to find") ||
		strings.Contains(msg, "找不到") ||
		strings.Contains(msg, "no match")
}

// ---- 桌面快捷方式 ----

// ShortcutInfo 是 `GET /api/shortcut` 的响应：桌面那个快捷方式现在指向谁。
//
// 为什么要有它：用户换新版本时，常常只是把新的 exe 解压到另一个文件夹，
// 桌面上那个 .lnk 还指着旧的（或者旧的已经被删掉了）。双击之后跑起来的是旧版本，
// 用户却以为“新版没生效”。把 target 读出来比一比，界面就能直接说清楚。
type ShortcutInfo struct {
	Supported bool `json:"supported"`
	Exists    bool `json:"exists"`
	// Path 是 .lnk 的路径（排查时用）。
	Path      string `json:"path,omitempty"`
	Target    string `json:"target,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	// Mismatch 为真表示它指向另一份 kb-lite。
	Mismatch bool `json:"mismatch"`
	// Missing 为真表示它指向的文件已经不存在了（旧版本被删了）。
	Missing bool   `json:"missing"`
	Error   string `json:"error,omitempty"`
}

// DesktopShortcut 在桌面建一个 KBLite 的快捷方式，返回它的完整路径。
func DesktopShortcut(exePath string) (string, error) {
	desktop, err := desktopDir()
	if err != nil {
		return "", err
	}
	return shortcutIn(desktop, exePath)
}

// ShortcutStatus 读桌面快捷方式的现状。
func ShortcutStatus(exePath string) ShortcutInfo {
	desktop, err := desktopDir()
	if err != nil {
		return ShortcutInfo{Supported: true, Error: err.Error()}
	}
	return shortcutStatusIn(desktop, exePath)
}

// shortcutStatusIn 是 ShortcutStatus 的可测试版：目录当参数传。
func shortcutStatusIn(dir, exePath string) ShortcutInfo {
	info := ShortcutInfo{Supported: true, Path: filepath.Join(dir, "KBLite.lnk")}
	if _, err := os.Stat(info.Path); err != nil {
		return info // 没建过，不是错误
	}
	info.Exists = true

	// 读 .lnk 的 target 要走 COM，与创建时同一个理由：不自己手捣 IShellLink。
	out, err := runPowerShell(strings.Join([]string{
		"$w = New-Object -ComObject WScript.Shell",
		"$s = $w.CreateShortcut(" + psQuote(info.Path) + ")",
		"Write-Output $s.TargetPath",
		"Write-Output $s.Arguments",
	}, "; "))
	if err != nil {
		info.Error = err.Error()
		return info
	}
	lines := make([]string, 0, 2)
	for _, ln := range strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n") {
		if t := strings.TrimSpace(ln); t != "" {
			lines = append(lines, t)
		}
	}
	if len(lines) > 0 {
		info.Target = lines[0]
	}
	if len(lines) > 1 {
		info.Arguments = lines[1]
	}
	if info.Target == "" {
		return info
	}
	info.Mismatch = !samePath(info.Target, exePath)
	if _, err := os.Stat(info.Target); err != nil {
		info.Missing = true
	}
	return info
}

// desktopDir 问系统要桌面目录。
//
// **不能**用 `~/Desktop`：OneDrive 接管桌面后真实路径会变成
// `C:\Users\x\OneDrive\桌面`，写死家目录会建到一个用户看不见的地方。
func desktopDir() (string, error) {
	out, err := runPowerShell(`[Environment]::GetFolderPath('Desktop')`)
	if err != nil {
		return "", err
	}
	dir := strings.TrimSpace(out)
	if dir == "" {
		return "", errors.New("系统没有给出桌面目录")
	}
	return dir, nil
}

// shortcutIn 在指定目录建 .lnk。测试用临时目录，不去污染真桌面。
func shortcutIn(dir, exePath string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	link := filepath.Join(dir, "KBLite.lnk")

	script := strings.Join([]string{
		"$w = New-Object -ComObject WScript.Shell",
		"$s = $w.CreateShortcut(" + psQuote(link) + ")",
		"$s.TargetPath = " + psQuote(exePath),
		"$s.Arguments = 'serve --open'",
		"$s.WorkingDirectory = " + psQuote(filepath.Dir(exePath)),
		"$s.IconLocation = " + psQuote(exePath+",0"),
		"$s.Description = 'KBLite：把 markdown 目录变成 AI 能查的知识库'",
		"$s.Save()",
		"Write-Output " + psQuote(link),
	}, "; ")

	if _, err := runPowerShell(script); err != nil {
		return "", err
	}
	if _, err := os.Stat(link); err != nil {
		return "", errors.New("快捷方式没有出现在桌面上（可能被安全软件拦了）")
	}
	return link, nil
}

// runPowerShell 跑一段脚本。用 PowerShell 是因为建 .lnk 要走 COM（IShellLink），
// 手搓那些接口的代价远高于借用系统自带的一个命令行工具。
func runPowerShell(script string) (string, error) {
	cmd := exec.Command("powershell.exe",
		"-NoProfile", "-NonInteractive", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("桌面操作失败：%s", strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// psQuote 把字符串包成 PowerShell 单引号字面量。
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

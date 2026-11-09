//go:build !windows

// 开机自启与桌面快捷方式这一版只做 Windows（docs/06 §5.5）。
//
// macOS 与 Linux 要分别写 LaunchAgent 与 systemd --user 单元文件，
// 没有实机验证之前宁可不做 —— 建一个“看起来成功但其实没生效”的自启项，
// 比明确说“这个系统还不支持”更糟。
package daemon

import "errors"

var errNotSupported = errors.New("这个系统还不支持，暂时只能开机后自己打开一次")

// AutostartValueName 在非 Windows 上保留名字，方便调用方统一写日志。
const AutostartValueName = "kb-lite"

// StartupInfo 与 Windows 版同形，只是 Supported 恒为假。
type StartupInfo struct {
	Supported bool   `json:"supported"`
	Enabled   bool   `json:"enabled"`
	Command   string `json:"command,omitempty"`
	Path      string `json:"path,omitempty"`
	Error     string `json:"error,omitempty"`
	ExePath   string `json:"exePath,omitempty"`
	Mismatch  bool   `json:"mismatch"`
	Fragile   bool   `json:"fragile"`
}

// AutostartCommand 只是为了两边签名一致。
func AutostartCommand(exePath string) string { return exePath + " serve" }

// StartupStatus 在非 Windows 上永远报告“不支持”。
func StartupStatus(exePath string) StartupInfo {
	return StartupInfo{Supported: false, Error: errNotSupported.Error()}
}

// SetStartup 在非 Windows 上直接报不支持。
func SetStartup(exePath string, on bool) error { return errNotSupported }

// DesktopShortcut 在非 Windows 上直接报不支持。
func DesktopShortcut(exePath string) (string, error) { return "", errNotSupported }

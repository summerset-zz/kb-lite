package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

// RotateReadToken 换一把新的读取密码并落盘（设置页的“换一个密码”）。
//
// 与 loadOrCreateToken 分开写：那边是“没有才生成”，这里必须**覆盖**，
// 而且换密码这件事要让旧密码当场作废，不能走“先删再建”（中间失败就没密码了）。
func RotateReadToken(dir string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	p := filepath.Join(dir, ReadTokenFileName)
	if err := os.WriteFile(p, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	// Windows 上 Chmod 基本是空操作，非 Windows 上是真的收紧权限。
	_ = os.Chmod(p, 0o600)
	return tok, nil
}

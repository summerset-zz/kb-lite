//go:build windows

package daemon

import "testing"

// 从注册表值里把 exe 路径抠出来：路径带空格、有引号时最容易错。
func TestExeFromCommand(t *testing.T) {
	cases := []struct{ in, want string }{
		{`"C:\Program Files\KBLite\kb-lite.exe" serve`, `C:\Program Files\KBLite\kb-lite.exe`},
		{`"C:\a\kb-lite.exe"`, `C:\a\kb-lite.exe`},
		{`C:\a\kb-lite.exe serve`, `C:\a\kb-lite.exe`},
		{`C:\a\kb-lite.exe`, `C:\a\kb-lite.exe`},
	}
	for _, c := range cases {
		if got := exeFromCommand(c.in); got != c.want {
			t.Errorf("exeFromCommand(%q) = %q，想要 %q", c.in, got, c.want)
		}
	}
}

// 下载目录 / 临时目录要认出来：这种位置开自启，下次开机很可能静默失效。
func TestInVolatileDir(t *testing.T) {
	volatile := []string{
		`C:\Users\me\Downloads\kb-lite-win-x64.exe`,
		`C:\Users\me\AppData\Local\Temp\kb-lite.exe`,
		`/home/me/Downloads/kb-lite`,
		`/tmp/kb-lite`,
	}
	for _, p := range volatile {
		if !inVolatileDir(p) {
			t.Errorf("%q 应当判成易失目录", p)
		}
	}
	stable := []string{
		`C:\Program Files\KBLite\kb-lite.exe`,
		`C:\Users\me\Tools\kb-lite.exe`,
		`D:\tools\kb-lite.exe`,
	}
	for _, p := range stable {
		if inVolatileDir(p) {
			t.Errorf("%q 不该判成易失目录", p)
		}
	}
}

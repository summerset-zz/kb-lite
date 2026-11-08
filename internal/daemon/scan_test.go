package daemon

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestScanBasics(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "正文")
	writeFile(t, filepath.Join(root, "sub", "b.txt"), "正文")
	writeFile(t, filepath.Join(root, ".env.md"), "以点开头的文件要收录")
	writeFile(t, filepath.Join(root, ".git", "config"), "不该被收录")
	writeFile(t, filepath.Join(root, "node_modules", "x", "README.md"), "不该被收录")
	writeFile(t, filepath.Join(root, ".obsidian", "workspace.md"), "不该被收录")
	writeFile(t, filepath.Join(root, "_drafts", "d.md"), "被 ignore 掉")
	writeFile(t, filepath.Join(root, "img.png"), "不是文本")

	res, err := Scan(ScanOptions{Root: root, Ignore: []string{"_drafts"}})
	if err != nil {
		t.Fatal(err)
	}

	got := make([]string, len(res.Files))
	for i, f := range res.Files {
		got[i] = f.RelPath
	}
	want := []string{".env.md", "a.md", "sub/b.txt"}
	if len(got) != len(want) {
		t.Fatalf("收录 = %v，想要 %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("收录 = %v，想要 %v", got, want)
		}
	}
}

func TestScanMaxFileBytes(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "big.md"), strings.Repeat("字", 100))
	writeFile(t, filepath.Join(root, "small.md"), "短")

	res, err := Scan(ScanOptions{Root: root, MaxFileBytes: 50})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].RelPath != "small.md" {
		t.Fatalf("文件 = %+v", res.Files)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "超过上限") {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestScanIncludePatterns(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "x")
	writeFile(t, filepath.Join(root, "b.rst"), "x")
	writeFile(t, filepath.Join(root, "docs", "c.md"), "x")

	res, err := Scan(ScanOptions{Root: root, Include: []string{"**/*.rst"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Files) != 1 || res.Files[0].RelPath != "b.rst" {
		t.Fatalf("文件 = %+v", res.Files)
	}
}

func TestMatchGlob(t *testing.T) {
	cases := []struct {
		pattern, name string
		want          bool
	}{
		{"**/*.md", "a.md", true},
		{"**/*.md", "x/y/z.md", true},
		{"**/*.md", "x/y/z.txt", false},
		{"*.md", "a.md", true},
		{"*.md", "x/a.md", false}, // 单星不跨目录
		{"_drafts", "_drafts", true},
		{"x?y.md", "xay.md", true},
	}
	for _, c := range cases {
		if got := matchGlob(c.pattern, c.name); got != c.want {
			t.Errorf("matchGlob(%q, %q) = %v，想要 %v", c.pattern, c.name, got, c.want)
		}
	}
}

func TestDecode(t *testing.T) {
	// UTF-8 + BOM
	text, warn := Decode(append([]byte("\xef\xbb\xbf"), []byte("中文\r\n换行")...))
	if warn != "" || text != "中文\n换行" {
		t.Errorf("BOM/CRLF: text=%q warn=%q", text, warn)
	}

	// GB18030
	gb, err := simplifiedchinese.GB18030.NewEncoder().Bytes([]byte("国标编码内容"))
	if err != nil {
		t.Fatal(err)
	}
	text, warn = Decode(gb)
	if warn != "" || text != "国标编码内容" {
		t.Errorf("GB18030: text=%q warn=%q", text, warn)
	}

	// 既不是 UTF-8 也不是 GB18030
	if _, warn := Decode([]byte{0xff, 0xff, 0xff}); warn == "" {
		t.Error("应当记一条 encoding warn")
	}
}

func TestResolveEntryPath(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "x")
	writeFile(t, filepath.Join(root, "sub", "b.md"), "x")
	writeFile(t, filepath.Join(root, ".git", "config"), "secret")

	cases := []struct {
		rel  string
		want error
	}{
		{"a.md", nil},
		{"sub/b.md", nil},
		{"sub/../a.md", nil}, // 清洗后仍在根内
		{"../a.md", ErrPathEscapes},
		{"sub/../../a.md", ErrPathEscapes},
		{"/etc/passwd", ErrPathEscapes},
		{"", ErrPathEscapes},
		{".", ErrPathEscapes},
		{".git/config", ErrPathForbidden},
		{"node_modules/x.js", ErrPathForbidden},
		{"nope.md", ErrPathNotFound},
	}
	for _, c := range cases {
		_, err := ResolveEntryPath(root, c.rel)
		if c.want == nil {
			if err != nil {
				t.Errorf("ResolveEntryPath(%q) = %v，想要成功", c.rel, err)
			}
			continue
		}
		if !errors.Is(err, c.want) {
			t.Errorf("ResolveEntryPath(%q) = %v，想要 %v", c.rel, err, c.want)
		}
	}
}

func TestResolveEntryPathRejectsSymlinkEscape(t *testing.T) {
	outside := t.TempDir()
	writeFile(t, filepath.Join(outside, "secret.md"), "根目录之外的内容")

	root := t.TempDir()
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("这台机器不允许建软链接（Windows 需要开发者模式）：%v", err)
	}

	if _, err := ResolveEntryPath(root, "link/secret.md"); !errors.Is(err, ErrPathEscapes) {
		t.Errorf("指向根目录之外的软链接应当被拒，实际 err = %v", err)
	}
}

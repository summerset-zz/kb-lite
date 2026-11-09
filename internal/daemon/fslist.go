package daemon

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// FSListResp 是 `GET /api/fs/list` 的结果（docs/04）。
//
// 浏览器拿不到真实路径，所以目录选择器只能由 daemon 代劳。
// 这比系统对话框好在能顺带告诉用户“这里有几个 markdown 文件”。
type FSListResp struct {
	Path   string   `json:"path"`
	Parent string   `json:"parent,omitempty"`
	Dirs   []FSDir  `json:"dirs"`
	Drives []string `json:"drives,omitempty"`
}

// FSDir 是一个候选子目录。
type FSDir struct {
	Name string `json:"name"`
	Path string `json:"path"`
	// HasMarkdown 表示这个目录（含子目录）里至少有一个 markdown。
	HasMarkdown bool `json:"hasMarkdown"`
	// MarkdownCount 是数到的数量，超过上限时等于上限（不要再报精确值）。
	MarkdownCount int `json:"markdownCount"`
}

const (
	// MaxFSDirs 是单层最多列多少个子目录（docs/04）。
	MaxFSDirs = 500
	// maxMarkdownProbe 是数 markdown 时的上限，防止在大盘根目录上扫半天。
	maxMarkdownProbe = 2000
)

// statDir 判断路径是不是目录。
func statDir(p string) (bool, error) {
	st, err := os.Stat(p)
	if err != nil {
		return false, err
	}
	return st.IsDir(), nil
}

// ListDirs 列出 `p` 下的子目录。
//
// 只列目录不列文件；跳过 `.` 开头的目录与 node_modules 这类（用户不会想把它们加进来）。
func ListDirs(p string) (*FSListResp, error) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return nil, err
	}
	// 展开符号链接再比较/列目录：避免用户被 `\\?\` 与短名绕进奇怪的状态。
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	dirs, err := os.ReadDir(abs)
	if err != nil {
		return nil, err
	}

	resp := &FSListResp{Path: abs, Dirs: make([]FSDir, 0, len(dirs))}
	if parent := filepath.Dir(abs); parent != abs {
		resp.Parent = parent
	}
	if abs == filepath.VolumeName(abs)+string(filepath.Separator) {
		resp.Drives = listDrives()
	}

	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		name := d.Name()
		if strings.HasPrefix(name, ".") || alwaysIgnoredDirs[name] {
			continue
		}
		full := filepath.Join(abs, name)
		count := countMarkdown(full, maxMarkdownProbe)
		resp.Dirs = append(resp.Dirs, FSDir{
			Name: name, Path: full,
			HasMarkdown:   count > 0,
			MarkdownCount: count,
		})
		if len(resp.Dirs) >= MaxFSDirs {
			break
		}
	}
	sort.Slice(resp.Dirs, func(i, j int) bool { return resp.Dirs[i].Name < resp.Dirs[j].Name })
	return resp, nil
}

// countMarkdown 递归数 markdown，超过 limit 就停（返回 limit）。
func countMarkdown(root string, limit int) int {
	n := 0
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // 没权限的子目录直接跳过，不要中断整个统计
		}
		if d.IsDir() {
			name := d.Name()
			if p != root && (strings.HasPrefix(name, ".") || alwaysIgnoredDirs[name]) {
				return filepath.SkipDir
			}
			return nil
		}
		if isMarkdownName(d.Name()) {
			n++
			if n >= limit {
				return filepath.SkipAll
			}
		}
		return nil
	})
	return n
}

func isMarkdownName(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".md", ".markdown", ".txt":
		return true
	}
	return false
}

// listDrives 只在 Windows 上返回盘符。
func listDrives() []string {
	out := make([]string, 0, 4)
	for c := 'A'; c <= 'Z'; c++ {
		p := string(c) + ":\\"
		if st, err := statDir(p); err == nil && st {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

package daemon

import (
	"errors"
	"io/fs"
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
	// UnderHome 表示这个位置在当前用户的用户目录里（含子目录）。
	//
	// 界面据此提醒一句：放在自己用户目录里的资料，换一个电脑用户登录就读不到，
	// 索引也更新不了。不说清楚的话，用户会以为是程序坏了。
	UnderHome bool `json:"underHome,omitempty"`
}

// FSDir 是一个候选子目录。
//
// 只给名字和路径。**不数里面有多少 markdown**：那是递归走整棵子树，
// 在大盘根目录上换一层就要等好几秒，而“有多少篇”对“选哪个文件夹”帮助有限。
type FSDir struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

const (
	// MaxFSDirs 是单层最多列多少个子目录（docs/04）。
	MaxFSDirs = 500
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
//
// `p` 为空时返回**起点**（见 startList）：目录选择器第一次打开时手上没有任何路径，
// 回一句“缺少 path 参数”只会让界面空着不动。
func ListDirs(p string) (*FSListResp, error) {
	if strings.TrimSpace(p) == "" {
		return startList()
	}
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

	resp := &FSListResp{Path: abs, Dirs: make([]FSDir, 0, len(dirs)), UnderHome: underHome(abs)}
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
		resp.Dirs = append(resp.Dirs, FSDir{Name: name, Path: full})
		if len(resp.Dirs) >= MaxFSDirs {
			break
		}
	}
	sort.Slice(resp.Dirs, func(i, j int) bool { return resp.Dirs[i].Name < resp.Dirs[j].Name })
	return resp, nil
}

// ---- 新建空文件夹 ----

// 建目录的错误（docs/04 的 `POST /api/fs/mkdir`）。
var (
	ErrInvalidDirName = errors.New("文件夹名字不合法")
	ErrPathExists     = errors.New("已经有同名的文件夹了")
)

// CreateDir 在 `parent` 下面建一个空文件夹。
//
// 这是管理面上**唯一**会写用户磁盘的接口，而且只建空目录：不写文件、不改内容、不删东西。
// 用它的理由是界面上的那一步：用户浏览到 `D:\notes`，发现想放进去的文件夹还不存在，
// 这时候让他切到资源管理器建完再回来，是最容易放弃的地方。
func CreateDir(parent, name string) (string, error) {
	name = strings.TrimSpace(name)
	// 借用知识库名字的校验：1 到 64 字，不含 `/` `\` 与控制字符，不是 `.` / `..`。
	if ValidateKBName(name) != nil {
		return "", ErrInvalidDirName
	}
	if strings.TrimSpace(parent) == "" {
		return "", ErrInvalidRoot
	}
	abs, err := filepath.Abs(parent)
	if err != nil {
		return "", ErrInvalidRoot
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	if ok, err := statDir(abs); err != nil || !ok {
		return "", ErrInvalidRoot
	}
	full := filepath.Join(abs, name)
	// 名字里已经禁掉分隔符，这里再确认一次没跑到上一层去。
	if filepath.Dir(full) != abs {
		return "", ErrInvalidDirName
	}
	if _, err := os.Stat(full); err == nil {
		return "", ErrPathExists
	}
	if err := os.Mkdir(full, 0o755); err != nil {
		return "", err
	}
	return full, nil
}

// underHome 判断这个位置是不是在当前用户的用户目录里。
//
// 只用来决定界面要不要提一句“这个目录别的电脑用户读不到”，
// 不当成任何安全判断：真正能不能读由系统权限决定。
func underHome(p string) bool {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return false
	}
	if resolved, err := filepath.EvalSymlinks(home); err == nil {
		home = resolved
	}
	return within(home, p)
}

// fsErrorMessage 把系统错误翻成一句用户能懂的话。
//
// 权限问题必须单独说：它的解决办法和“路径打错了”完全不同。用户看到
// `Access is denied.` 只会以为程序坏了；告诉他“可能是另一个用户的文件”，
// 他才知道该干什么。
func fsErrorMessage(err error) string {
	switch {
	case errors.Is(err, fs.ErrPermission):
		return "没有访问权限。可能是这个文件夹属于另一个用户，或者被系统保护着（比如 Windows 的系统目录）。"
	case errors.Is(err, fs.ErrNotExist):
		return "这个位置不存在，可能被删掉或者改名了。"
	default:
		return "这个位置打不开：" + err.Error()
	}
}

// startList 是「手上还没有路径」时的起点。
// Windows 先给盘符（用户最可能想说 D:\ 这种），其它平台从家目录开始。
// 两个都没有才退回文件系统根。
func startList() (*FSListResp, error) {
	if drives := listDrives(); len(drives) > 0 {
		return &FSListResp{Dirs: []FSDir{}, Drives: drives}, nil
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		if resp, err := ListDirs(home); err == nil {
			return resp, nil
		}
	}
	return ListDirs(string(filepath.Separator))
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

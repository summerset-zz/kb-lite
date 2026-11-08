package daemon

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

// DefaultMaxFileBytes 是单文件大小上限，超过就跳过并记一条 warn（docs/02）。
const DefaultMaxFileBytes int64 = 2 * 1024 * 1024

// DefaultInclude 是默认的收录规则。
var DefaultInclude = []string{"**/*.md", "**/*.markdown", "**/*.txt"}

// alwaysIgnoredDirs 是**永远**忽略的目录，不可通过配置放开。
var alwaysIgnoredDirs = map[string]bool{
	".git": true, "node_modules": true, ".svn": true, ".hg": true,
}

// defaultIgnoredDirs 是默认忽略的目录（docs/02 允许放开，但 UI 不暴露）。
var defaultIgnoredDirs = map[string]bool{
	".obsidian": true, ".trash": true, ".vscode": true, ".idea": true,
}

var defaultIgnoredFiles = map[string]bool{
	".DS_Store": true, "Thumbs.db": true,
}

// 路径相关的错误。上层据此返回 4xx，而不是 500。
var (
	ErrPathEscapes   = errors.New("路径越界：解析结果不在知识库根目录之内")
	ErrPathForbidden = errors.New("路径落在被排除的目录里（.git / node_modules / .obsidian 等）")
	ErrPathNotFound  = errors.New("路径不存在")
)

// ScanOptions 是一次目录扫描的参数。
type ScanOptions struct {
	Root         string
	Include      []string // 空 = DefaultInclude
	Ignore       []string // 在默认忽略之上叠加
	MaxFileBytes int64    // <= 0 = DefaultMaxFileBytes
}

// ScannedFile 是一个命中收录规则的文件。
type ScannedFile struct {
	// RelPath 是相对 KB 根目录的 POSIX 路径，同时是条目的对外 ID。
	RelPath string
	// AbsPath 是真实绝对路径（由根目录拼出来，未做 symlink 展开）。
	AbsPath string
	Size    int64
}

// ScanResult 是一次扫描的结果。
type ScanResult struct {
	Files    []ScannedFile
	Warnings []string
}

// Scan 遍历目录，按 docs/02「文件发现与忽略」的规则给出文件清单。
//
// 几条刻意的行为：
//   - **不跟随目录软链接**：`filepath.WalkDir` 本来就不进 symlink 目录，正好是我们要的；
//   - 文件软链接会被收录，真正读的时候才展开（越界由 ResolveEntryPath 兜）；
//   - 以 `.` 开头的**目录**跳过，但以 `.` 开头的**文件**不跳过（`.env.md` 是用户的真实内容）。
func Scan(opts ScanOptions) (ScanResult, error) {
	root, err := filepath.Abs(opts.Root)
	if err != nil {
		return ScanResult{}, err
	}
	info, err := os.Stat(root)
	if err != nil {
		return ScanResult{}, err
	}
	if !info.IsDir() {
		return ScanResult{}, errors.New("不是目录：" + root)
	}

	include := opts.Include
	if len(include) == 0 {
		include = DefaultInclude
	}
	maxBytes := opts.MaxFileBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxFileBytes
	}

	var res ScanResult
	walkErr := filepath.WalkDir(root, func(p string, d fs.DirEntry, walkErr error) error {
		rel, relErr := filepath.Rel(root, p)
		if relErr != nil {
			return nil
		}
		if rel == "." {
			return nil
		}
		relPosix := filepath.ToSlash(rel)
		name := d.Name()

		if walkErr != nil {
			res.Warnings = append(res.Warnings, relPosix+": 无法读取（"+walkErr.Error()+"）")
			if d != nil && d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			if alwaysIgnoredDirs[name] || defaultIgnoredDirs[name] || strings.HasPrefix(name, ".") {
				return fs.SkipDir
			}
			if ignored(relPosix, name, opts.Ignore) {
				return fs.SkipDir
			}
			return nil
		}

		if defaultIgnoredFiles[name] {
			return nil
		}
		if ignored(relPosix, name, opts.Ignore) {
			return nil
		}
		if !matchedAny(include, relPosix) {
			return nil
		}

		fi, statErr := d.Info()
		if statErr != nil {
			res.Warnings = append(res.Warnings, relPosix+": 读不到文件信息（"+statErr.Error()+"）")
			return nil
		}
		if fi.Size() > maxBytes {
			res.Warnings = append(res.Warnings, relPosix+": 文件超过上限，已跳过")
			return nil
		}

		res.Files = append(res.Files, ScannedFile{RelPath: relPosix, AbsPath: p, Size: fi.Size()})
		return nil
	})
	if walkErr != nil {
		return res, walkErr
	}

	// 固定顺序：切片的编号、内存索引、以及 search 的排序都依赖这个顺序是确定的。
	sort.Slice(res.Files, func(i, j int) bool { return res.Files[i].RelPath < res.Files[j].RelPath })
	sort.Strings(res.Warnings)
	return res, nil
}

func ignored(relPosix, name string, patterns []string) bool {
	for _, pattern := range patterns {
		if pattern == "" {
			continue
		}
		if pattern == name || matchGlob(pattern, relPosix) {
			return true
		}
		// `_drafts` 这种写法既可能指目录本身，也可能指它下面的东西。
		if strings.HasPrefix(relPosix, pattern+"/") {
			return true
		}
	}
	return false
}

func matchedAny(patterns []string, relPosix string) bool {
	for _, pattern := range patterns {
		if matchGlob(pattern, relPosix) {
			return true
		}
	}
	return false
}

// matchGlob 按 `/` 分段匹配 posix 相对路径，支持 `**`（跨任意层）、`*`、`?`。
//
// 只用标准库的 `path.Match` 做单段匹配 —— 它不支持 `**`，所以我们自己拆段。
func matchGlob(pattern, name string) bool {
	pat := strings.Split(strings.TrimPrefix(pattern, "./"), "/")
	seg := strings.Split(name, "/")

	var match func(pi, si int) bool
	match = func(pi, si int) bool {
		if pi == len(pat) {
			return si == len(seg)
		}
		if pat[pi] == "**" {
			for k := si; k <= len(seg); k++ {
				if match(pi+1, k) {
					return true
				}
			}
			return false
		}
		if si == len(seg) {
			return false
		}
		ok, err := path.Match(pat[pi], seg[si])
		if err != nil || !ok {
			return false
		}
		return match(pi+1, si+1)
	}
	return match(0, 0)
}

// Decode 是 docs/02「解码」的逐条实现。
//
// 顺序很重要：**严格 UTF-8 → GB18030 → 放弃**。中文用户的 TXT 大多是 GBK/GB18030，
// 而 GB18030 是全字节编码，几乎不会失败 —— 所以「先 UTF-8 严格解码」这一步必须真的严格
// （`utf8.Valid`），不能用「有坏字节就替换」的宽松模式，否则所有 GBK 文件都会被误判成 UTF-8。
func Decode(data []byte) (string, string) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")) // UTF-8 BOM

	var text string
	if utf8.Valid(data) {
		text = string(data)
	} else {
		decoded, err := simplifiedchinese.GB18030.NewDecoder().Bytes(data)
		if err != nil {
			return "", "encoding: 既不是 UTF-8 也不是 GB18030"
		}
		// x/text 的解码器对非法字节是**容错**的：吐 U+FFFD 而不报错。
		// 所以还得自己看有没有替换字符，否则任何二进制文件都会被当成 GB18030 收进来。
		if bytes.ContainsRune(decoded, utf8.RuneError) {
			return "", "encoding: 既不是 UTF-8 也不是 GB18030（解码出了替换字符）"
		}
		text = string(decoded)
	}

	// CRLF / CR 统一成 LF。
	if strings.ContainsRune(text, '\r') {
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
	}
	return text, ""
}

// ResolveEntryPath 把对外相对路径解析成真实绝对路径，并确认它没跑出 KB 根目录。
//
// 这是 docs/07「路径越界」那一段的实现，也是整个服务唯一的攻击面：
//
//   - 拒绝绝对路径、`..`、以及清洗后为空的路径；
//   - 拒绝落在 `.git` / `node_modules` / `.obsidian` 这类目录里的路径（即便它们在根目录之内）——
//     返回值里不能出现 `.git/config` 这种带凭据的文件；
//   - **比较的是 realpath 之后的路径**，不是用户输入：Windows 的 8.3 短名（`PROGRA~1`）
//     和大小写不敏感的文件系统都能绕过朴素的前缀检查；
//   - 文件必须存在（读接口只读索引里有的东西）。
func ResolveEntryPath(root, rel string) (string, error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if rel == "" || strings.HasPrefix(rel, "/") {
		return "", ErrPathEscapes
	}

	clean := path.Clean(rel)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", ErrPathEscapes
	}
	for _, seg := range strings.Split(clean, "/") {
		if alwaysIgnoredDirs[seg] || defaultIgnoredDirs[seg] {
			return "", ErrPathForbidden
		}
	}

	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	full := filepath.Join(realRoot, filepath.FromSlash(clean))
	realFull, err := filepath.EvalSymlinks(full)
	if err != nil {
		if os.IsNotExist(err) {
			return "", ErrPathNotFound
		}
		return "", ErrPathEscapes
	}
	if !within(realRoot, realFull) {
		return "", ErrPathEscapes
	}
	return realFull, nil
}

// within 判断 p 是否在 root 之内（按 realpath 之后的路径比较）。
func within(root, p string) bool {
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if rel == "." {
		return true
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

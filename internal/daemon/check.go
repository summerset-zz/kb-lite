package daemon

import (
	"path"
	"strings"
	"unicode/utf8"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// 内容检查（docs/04 的 `POST /kb/{name}/check`）。
//
// 整理侧的 AI 在**写文件之前**想知道“我生成的东西会不会被收录”。这条接口纯计算：
// 不读磁盘、不确认文件存在、不关心父目录建没建，所以它没有任何攻击面，
// 也不会把服务变成一个“任意文件探测器”。
//
// 为什么不收全文：一篇 10 KB 的中文 markdown 约 5k token，一次检查几十篇就上万。
// 而决定“能不能入库”的只有路径规则、frontmatter、正文是否为空、文件大小，
// 前三条全在文件开头里。全文唯一多给的是分节数与字数，那两个数字调用方自己数得出来。
const (
	// MaxCheckItems 是一次最多检查多少条。
	MaxCheckItems = 20
	// MaxCheckHead 是单条 head 的上限（字节）。
	//
	// 2 KB 够放下 frontmatter 与第一个标题。给更多只是让调用方多花 token，
	// 判定的结论不会因此改变。
	MaxCheckHead = 2048
)

// CheckItem 是一条检查输入。
type CheckItem struct {
	// Path 是**拟定**的写入位置，相对知识库根目录的 POSIX 路径。
	// 只用来跑位置规则与标题取值链，不要求文件已经存在。
	Path string `json:"path"`
	// Head 是文件开头的文本，含第一行的 `---`，最多 MaxCheckHead 字节。
	Head string `json:"head"`
	// Chars 是整个文件的字符数（`[...text].length`），用来判空。
	Chars int `json:"chars"`
	// Bytes 是整个文件的字节数，用来判单文件上限。为 0 时不判。
	Bytes int64 `json:"bytes"`
}

// CheckWarning 是一条不影响收录的提醒。
type CheckWarning struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// CheckResult 是一条检查的结论。
type CheckResult struct {
	Path string `json:"path"`
	// Included 为真表示“写到这个位置就会被收录”。
	Included bool   `json:"included"`
	Reason   string `json:"reason,omitempty"`
	Message  string `json:"message,omitempty"`
	// Title 与 Summary 是**服务端算出来的**，它们才需要这份接口：
	// `docs/api/README.md` 的标题会变成 `api`，写之前就该看见。
	Title    string         `json:"title,omitempty"`
	Summary  string         `json:"summary,omitempty"`
	Warnings []CheckWarning `json:"warnings,omitempty"`
}

// 不受欢迎的原因（docs/04）。取值固定，调用方据此分支。
const (
	CheckOutside      = "outside"        // 路径不合法，或跑到根目录外面
	CheckIgnored      = "ignored"        // 落在忽略的目录 / 文件里
	CheckNotMatching  = "not-matching"   // 扩展名不在 include 里
	CheckTooLarge     = "too-large"      // 超过单文件上限
	CheckDraft        = "draft"          // frontmatter 要求跳过
	CheckEmpty        = "empty"          // 没有正文
	CheckHeadTooShort = "head-too-short" // 开头被截断，判不准
)

// CheckContent 判断“把这段文本写到这个位置，会不会被收录”。
//
// 判定与真正入库走同一条路：位置规则用 Scan 的同一组函数，切片用 core.BuildEntry。
// 两处只要有一处不同，就会出现“检查说行、点更新却被跳过”，那比没有这条接口更糟。
func CheckContent(kb KB, maxFileBytes int64, item CheckItem) CheckResult {
	res := CheckResult{Path: item.Path}

	rel, why := normalizeCheckPath(item.Path)
	if why != "" {
		res.Reason = CheckOutside
		res.Message = why
		return res
	}
	res.Path = rel

	// 位置规则。rejectDir 与 Scan 的跳过条件一致：永远忽略的目录、以 `.` 开头的目录。
	if seg := rejectDir(rel); seg != "" {
		res.Reason = CheckIgnored
		res.Message = "这个位置在会被跳过的目录里（" + seg + "），写进去也不会被收录。"
		return res
	}
	name := path.Base(rel)
	if defaultIgnoredFiles[name] {
		res.Reason = CheckIgnored
		res.Message = "这个文件名是系统垃圾文件，不会进知识库。"
		return res
	}
	if ignored(rel, name, kb.Ignore) {
		res.Reason = CheckIgnored
		res.Message = "这个位置在你给这个知识库设的忽略列表里。"
		return res
	}
	include := kb.Include
	if len(include) == 0 {
		include = DefaultInclude
	}
	if !matchedAny(include, rel) {
		res.Reason = CheckNotMatching
		res.Message = "扩展名不在收录范围内。默认只收 .md、.markdown、.txt。"
		return res
	}

	limit := maxFileBytes
	if limit <= 0 {
		limit = DefaultMaxFileBytes
	}
	if item.Bytes > limit {
		res.Reason = CheckTooLarge
		res.Message = "文件超过了单个文件的大小上限，会被跳过。"
		return res
	}

	if item.Chars == 0 {
		res.Reason = CheckEmpty
		res.Message = "正文是空的。"
		return res
	}

	// 开头截到设置中间时，任何结论都可能是错的（最糟的是把 draft 放过去）。
	// 只有“文件还有内容没送来”才算截断：chars 说明 head 已经是全文时，
	// 没闭合的 `---` 是作者真的写坏了，交给 core 当作正文收录并提醒。
	if headCutInFrontmatter(item.Head) && item.Chars > utf8.RuneCountInString(item.Head) {
		res.Reason = CheckHeadTooShort
		res.Message = "文件开头的设置超过了这次送来的长度，判不准。把 head 发大一点再试。"
		return res
	}

	built := core.BuildEntry(rel, item.Head, core.SliceOptions{})
	for _, w := range built.Warnings {
		res.Warnings = append(res.Warnings, CheckWarning{Kind: "frontmatter", Message: w})
	}

	if built.Skipped {
		switch built.SkipReason {
		case core.SkipFrontmatter:
			res.Reason = CheckDraft
			res.Message = "文件开头写着要跳过（draft: true、publish: false 或 kb_ignore: true）。"
			return res
		case core.SkipEmptyBody:
			// head 里正文为空，但整个文件还有内容：说明正文在 head 之后。
			// 这时标题取值链也算不准，同样是“判不准”。
			if item.Chars > utf8.RuneCountInString(item.Head) {
				res.Reason = CheckHeadTooShort
				res.Message = "开头这段只看到了文件设置，正文还没开始，判不准。把 head 发大一点再试。"
				return res
			}
			res.Reason = CheckEmpty
			res.Message = "去掉文件开头的设置之后没有正文，这篇会被跳过。"
			return res
		}
	}

	res.Included = true
	res.Title = built.Entry.Title
	res.Summary = built.Entry.Summary

	// 内容模式才做得了的检查：替换字符。从 PDF / docx 转出来的文本常带 U+FFFD，
	// 那说明转换那一步已经把字丢了，这种文件进库以后怎么都搜不到。
	if strings.ContainsRune(item.Head, utf8.RuneError) {
		res.Warnings = append(res.Warnings, CheckWarning{
			Kind:    "replacement-char",
			Message: "文本里有替换字符（U+FFFD），说明之前那次转换丢了字。",
		})
	}
	return res
}

// normalizeCheckPath 把拟定的路径收紧成 rel_path 的形状。
//
// 只做词法检查，**不碰磁盘**：这条接口的价值就是零文件访问。
func normalizeCheckPath(p string) (string, string) {
	s := strings.ReplaceAll(strings.TrimSpace(p), "\\", "/")
	if s == "" {
		return "", "没给 path。"
	}
	if strings.HasPrefix(s, "/") || hasDriveLetter(s) {
		return "", "只收相对知识库根目录的路径，不要绝对路径。"
	}
	clean := path.Clean(s)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", "路径不能跑到知识库目录外面。"
	}
	return clean, ""
}

// hasDriveLetter 判断 `C:/x` 这种 Windows 绝对路径。
func hasDriveLetter(p string) bool {
	if len(p) < 2 || p[1] != ':' {
		return false
	}
	c := p[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z')
}

// rejectDir 返回路径里那个会让文件被跳过的目录名，没有就返回空串。
//
// 与 Scan 的规则保持一致：永远忽略的目录（`.git` 等）、默认忽略的目录（`.obsidian` 等）、
// 以及**任何以 `.` 开头的目录**。最后一段是文件名，不参与这条判断（`.env.md` 是正文）。
func rejectDir(rel string) string {
	segs := strings.Split(rel, "/")
	for i, seg := range segs {
		if alwaysIgnoredDirs[seg] || defaultIgnoredDirs[seg] {
			return seg
		}
		if i < len(segs)-1 && strings.HasPrefix(seg, ".") {
			return seg
		}
	}
	return ""
}

// headCutInFrontmatter 判断 head 是不是截在了 frontmatter 中间。
//
// 判据：第一行是 `---`，但后面没有第二行 `---`。真正“没有 frontmatter”的文件
// 第一行就不是 `---`，不会被误判。
func headCutInFrontmatter(head string) bool {
	if !strings.HasPrefix(head, "---") {
		return false
	}
	lines := strings.Split(head, "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		return false
	}
	for _, ln := range lines[1:] {
		if strings.TrimSpace(ln) == "---" {
			return false
		}
	}
	return true
}

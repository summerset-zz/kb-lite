package daemon

// Warning 是一条可展示的问题记录。
//
// 带 Kind 而不是只有一句人话：调用方要能按类别汇总。
// 「8 条都是 frontmatter 宽松解析」和「8 条编码失败」对用户的含义完全不同 ——
// 前者说明内容仍然被索引了，后者说明有文件根本没进去。
//
// 这条是 v0.1 验收时被真实 agent 逼出来的：serve 里原来写死了
// 「跳过的大文件 / 编码失败之类」这句话，结果 agent 以为索引不全，
// 绕开服务自己去 grep 了整个目录。
type Warning struct {
	Kind    string
	Message string
}

// 告警类别。
const (
	// WarnTooLarge：单文件超过 maxFileBytes，被跳过（内容没进索引）。
	WarnTooLarge = "too-large"
	// WarnEncoding：既不是 UTF-8 也不是 GB18030，被跳过（内容没进索引）。
	WarnEncoding = "encoding"
	// WarnFrontmatter：frontmatter 不是合法 YAML，但正文已按行恢复并索引。
	WarnFrontmatter = "frontmatter"
	// WarnRead：读文件失败（权限、文件消失）。
	WarnRead = "read"
	// WarnCache：SQLite 缓存读/写失败。不影响正确性，只影响下次启动要不要重切。
	WarnCache = "cache"
)

// WarningLabel 是给人看的类别名。
func WarningLabel(kind string) string {
	switch kind {
	case WarnTooLarge:
		return "文件过大"
	case WarnEncoding:
		return "编码失败"
	case WarnFrontmatter:
		return "frontmatter 不合法"
	case WarnRead:
		return "读取失败"
	case WarnCache:
		return "缓存不可用"
	default:
		return kind
	}
}

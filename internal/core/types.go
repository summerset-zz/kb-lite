// Package core 是 kb-lite 的切片器与检索打分器。
//
// 这里只有纯函数：输入字符串，输出对象。不碰文件系统、不碰网络、不读时钟，
// 也没有任何三方依赖 —— 见 docs/01-architecture.md 与 ADR-011。
//
// 行为规格在 docs/02-slicing.md（切片）与 docs/03-search.md（打分）。
// **改行为先改文档**：两边的契约由 testdata 下的快照锁住，不是靠注释。
package core

// Section 是一节。
//
// 「节」的边界只由标题行决定，与正文里有没有内容无关：一个标题下面没有正文时，
// 仍然产生一个 Chars == 0 的节（docs/02-slicing.md 第 8 步）。父标题无正文、
// 内容全在子节里，在真实笔记里非常常见。
type Section struct {
	// No 是章节编号：0 = 导言，其余从 1 起按出现顺序全局扁平连续（不出现 1.1）。
	// 这个编号是 agent 与人之间唯一稳定的引用物（toc → section?n=），
	// 任何会随内容变化的设计都是错的，见 ADR-009。
	No int
	// Level 是标题层级 1..6；导言（No == 0）固定为 1。
	Level int
	// Title 是去掉行内 markdown 修饰后的标题纯文本。
	Title string
	// Path 是标题栈（面包屑），例如 ["上线与回滚", "回滚"]。
	Path []string
	// PathText 是 Path 用 " / " 拼起来的结果。
	PathText string
	// Content 是本节**自己的**正文，不含子节。原始 markdown，未清洗。
	Content string
	// Chars 是 Content 的字符数（按 rune 计，"字数" 就是人眼看到的字数）。
	Chars int
}

// TocEntry 是搜索响应里 toc 文本的每一行。
//
// 它由 Section 派生。注意导言（No == 0）**不进** toc：它是单独的节，直接传 n=0 读。
type TocEntry struct {
	// No 原样回灌给 section 接口的 n 参数。
	No int
	// Level 只用来决定 toc 文本的缩进（2 空格/级）。
	Level int
	Title string
	// PathText 目前不参与 toc 文本渲染，留着给 json 格式与调试用。
	PathText string
	// Chars 是本节自己的正文字数；0 表示空节（通常是个父标题）。
	Chars int
	// Long 表示这一节超过 maxSectionChars，读取时会被截断（编号不变）。
	Long bool
}

// SplitResult 是一次切片的结果。
type SplitResult struct {
	// Summary 是第一个标题之前的原始正文（未清洗）。摘要的清洗是另一件事，
	// 见 docs/02-slicing.md 的「摘要」一节 —— 导言节本身不洗。
	Summary string
	// Sections 已含导言节（Summary 为空时不含）。编号见 Section.No。
	Sections []Section
	// TOC 由 Sections 派生，**不含导言节**。
	TOC []TocEntry
}

// SectionByNo 按编号取一节。ok == false 表示这一页没有这个编号。
func (r SplitResult) SectionByNo(n int) (Section, bool) {
	for _, s := range r.Sections {
		if s.No == n {
			return s, true
		}
	}
	return Section{}, false
}

// Entry 是一篇文件的切片结果 + 检索字段。
//
// 检索字段在构建时算好并小写化，这样每次查询不需要重复做 ——
// 5000 篇 × 5 关键词要在毫秒级跑完（docs/03-search.md「性能与上限」）。
//
// 命名对照 docs/03 的记号：SearchTitle = title / SearchMeta = body（摘要+标签）/
// TocText = toc / RelPath = path / SearchFull = full。
type Entry struct {
	// RelPath 是相对 KB 根目录的 POSIX 路径，同时是条目的对外 ID。
	RelPath string
	// Title 走标题取值链，见 docs/02-slicing.md。
	Title string
	// Summary 是洗过的简介，给人和模型看。
	Summary string
	// Tags 来自 frontmatter。
	Tags []string
	// Sections 见 SplitResult.Sections。
	Sections []Section
	// TOC 见 SplitResult.TOC。
	TOC []TocEntry
	// Chars 是去掉 frontmatter 之后整篇正文的字符数（按 rune 计，含标题行）。
	// 注意它**不等于** toc 各行 chars 之和：那些只算每节自己的正文，不含标题行。
	Chars int
	// Rev 是内容 sha1 的前 8 位，由 daemon 填。core 不碰哈希。
	Rev string

	// SearchTitle 是 Title 的小写形式（同时充当 leaf 档：见 03 的「与线上版的三处差异」第 1 条）。
	SearchTitle string
	// SearchPath 是 RelPath 的小写形式（03 的 path 档）。
	SearchPath string
	// SearchPathLeaf 是 RelPath 末段的小写形式；只在关键词是标识符时参与 leaf 档（03 字段表最后一行）。
	SearchPathLeaf string
	// SearchMeta 是「摘要 + 标签」拼起来的小写文本（03 里的 body 档）。
	SearchMeta string
	// TocText 是所有节的 title + pathText 拼起来并小写；「这一篇里有一节叫这个名字」是很强的信号。
	// 与 SearchFull 一样封顶 BodyLimit 字符。
	TocText string
	// SearchFull 是整篇正文的小写形式，只取前 BodyLimit 个字符（03 里的 full 档）。
	SearchFull string
}

// BodyLimit 是 Entry.SearchFull 的截断长度：再长对召回的边际收益为零，而且占内存。
const BodyLimit = 20000

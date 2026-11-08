package core

import (
	"sort"
	"strings"
	"unicode"
)

// 权重（docs/03-search.md「打分」）。**必须维持这个序**：
//
//	只命中全文 < 只命中摘要/标签 ≲ 命中路径 ≲ 命中章节标题 ≈ 命中标题 ≈ 命中条目标题 < 命中标题且命中路径
//
// 调权重时只允许在这个序内微调，并且必须跑 TestRankSnapshot。
const (
	WeightStrong   = 10
	WeightTitle    = 5
	WeightTOC      = 4
	WeightLeaf     = 4
	WeightPath     = 3
	WeightWeak     = 3
	WeightFullOnly = 1
)

const (
	// MaxKeywords 是一次查询最多取几个关键词，超出的丢弃（不合并）。
	MaxKeywords = 5
	// DefaultLimit 是 search 的默认返回条数（docs/04）。
	DefaultLimit = 5
)

// TokenizeQuery 是 docs/03「关键词拆分」的逐条实现。
func TokenizeQuery(query string) []string {
	fields := strings.FieldsFunc(strings.TrimSpace(query), func(r rune) bool {
		return unicode.IsSpace(r) || r == '.'
	})
	if len(fields) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(fields))
	out := make([]string, 0, MaxKeywords)
	for _, f := range fields {
		// 剔掉首尾的括号/引号/逗号类字符：`map()` → `map`。只剔首尾、且只剔这些
		// 纯语法字符 —— `C++` / `C#` / `a-b` / `v1.2` 整块都是有信息的（docs/03 步骤 3）。
		f = trimQueryNoise(f)
		// 不含任何字母或数字的碎片（`()` `--` `&`）对「哪个条目更相关」没有信息量。
		if f == "" || !hasLetterOrDigit(f) {
			continue
		}
		key := strings.ToLower(f)
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, f)
		if len(out) == MaxKeywords {
			break
		}
	}
	return out
}

// trimQueryNoise 剔掉查询碎片首尾的纯语法字符。
//
// 只剔这些，**不剔** `+ # - _ . $ /` —— 否则 `C++` 会变成 `C`、`C#` 变成 `C`，
// 那比不剔更糟。见 docs/03 步骤 3。
func trimQueryNoise(s string) string {
	return strings.TrimFunc(s, func(r rune) bool { return queryNoiseRunes[r] })
}

var queryNoiseRunes = map[rune]bool{
	'(': true, ')': true, '[': true, ']': true, '{': true, '}': true,
	'"': true, '\'': true, '`': true, ',': true, ';': true, ':': true,
	'!': true, '?': true,
	'（': true, '）': true, '【': true, '】': true, '「': true, '」': true,
	'《': true, '》': true, '，': true, '；': true, '：': true, '！': true,
	'？': true, '。': true, '、': true,
}

func hasLetterOrDigit(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// Matcher 是一个关键词的匹配器。
//
// 标识符型（纯 ASCII `[A-Za-z0-9_$]`）**卡词边界**：搜 `Array` 不该命中 `TypedArray`、
// 搜 `map` 不该命中 `flatMap`。含中文之类的一律纯子串 —— 中文没有词边界，
// 卡了等于搜不到（「数组」应该命中「类型化数组」）。
//
// ⚠️ 这个边界**不是用正则写的**：Go 的 RE2 没有 lookbehind，`(?<!…)` 编译不过；
// 退化成 `\b` 又是错的（`\b` 不把 `$` `_` 当词字符，与我们要的字符类不一致）。见 ADR-013。
type Matcher struct {
	// Display 是 --explain 里显示的写法；bigram 带 `(≈)` 后缀。
	Display string
	kw      string // 小写，用于匹配
	ident   bool
	approx  bool
}

func newMatcher(keyword string, approx bool) Matcher {
	m := Matcher{Display: keyword, kw: strings.ToLower(keyword), ident: isIdentifier(keyword), approx: approx}
	if approx {
		m.Display = keyword + "(≈)"
	}
	return m
}

// isIdentifier 判断是不是纯 ASCII 标识符（决定要不要卡词边界）。
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if !isIdentByte(s[i]) {
			return false
		}
	}
	return true
}

func isIdentByte(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' ||
		b >= '0' && b <= '9' || b == '_' || b == '$'
}

func identByteAt(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	return isIdentByte(s[i])
}

// test 判断是否命中。s 必须**已经小写**（所有检索字段在构建时都小写化了）。
func (m Matcher) test(s string) bool {
	if m.kw == "" {
		return false
	}
	if !m.ident {
		return strings.Contains(s, m.kw)
	}
	for from := 0; from <= len(s); {
		j := strings.Index(s[from:], m.kw)
		if j < 0 {
			return false
		}
		start := from + j
		end := start + len(m.kw)
		// 前后都不是标识符字符 —— 这就是 `(?<![A-Za-z0-9_$])kw(?![A-Za-z0-9_$])`。
		if !identByteAt(s, start-1) && !identByteAt(s, end) {
			return true
		}
		from = start + 1
	}
	return false
}

// Hit 是一个关键词在一条候选上的命中位置（docs/03 的 `Hit`）。
type Hit struct {
	Keyword string `json:"keyword"`
	Title   bool   `json:"title"`
	Path    bool   `json:"path"`
	TOC     bool   `json:"toc"`
	Leaf    bool   `json:"leaf"`
	Body    bool   `json:"body"`
	Full    bool   `json:"full"`
	// Approx 表示这是 CJK bigram 变体：只加分，不参与 strong 判定。
	Approx bool `json:"approx,omitempty"`
}

// Detail 是一条候选在一个查询下的完整得分明细 —— CLI/UI 的 `--explain` 直接吃它。
type Detail struct {
	Hits     []Hit `json:"hits"`
	Strong   int   `json:"strong"`
	Weak     int   `json:"weak"`
	FullOnly int   `json:"fullOnly"`
	InTitle  int   `json:"inTitle"`
	InPath   int   `json:"inPath"`
	InTOC    int   `json:"inToc"`
	InLeaf   int   `json:"inLeaf"`
	// Approx 是 bigram 的加分总额。
	Approx int `json:"approx"`
	Score  int `json:"score"`
}

// Query 是一次编译好的查询。
type Query struct {
	// Keywords 是拆分后的关键词（保留首次出现的写法），--explain 的表头用。
	Keywords []string
	groups   []matcherGroup
}

type matcherGroup struct {
	main   Matcher
	approx []Matcher
}

// Compile 把查询文本编译成匹配器（含 CJK bigram 展开）。
func Compile(query string) *Query {
	kws := TokenizeQuery(query)
	q := &Query{Keywords: kws}
	for _, k := range kws {
		g := matcherGroup{main: newMatcher(k, false)}
		for _, b := range bigramsOf(k) {
			g.approx = append(g.approx, newMatcher(b, true))
		}
		q.groups = append(q.groups, g)
	}
	return q
}

// bigramsOf 生成 CJK bigram 变体（docs/03「CJK bigram 辅助」）。
//
// 只在关键词**含 CJK 且长度 ≥ 3** 时生成；并且只在**连续 CJK 段内部**取，
// 免得 `api知识` 这种拼出 `i知` 来。
func bigramsOf(keyword string) []string {
	runes := []rune(keyword)
	if len(runes) < 3 {
		return nil
	}
	hasCJK := false
	for _, r := range runes {
		if isCJK(r) {
			hasCJK = true
			break
		}
	}
	if !hasCJK {
		return nil
	}

	var out []string
	seen := make(map[string]struct{}, len(runes))
	for i := 0; i < len(runes); {
		if !isCJK(runes[i]) {
			i++
			continue
		}
		j := i
		for j < len(runes) && isCJK(runes[j]) {
			j++
		}
		for k := i; k+1 < j; k++ {
			bg := string(runes[k : k+2])
			if _, dup := seen[bg]; dup {
				continue
			}
			seen[bg] = struct{}{}
			out = append(out, bg)
		}
		i = j
	}
	return out
}

func isCJK(r rune) bool {
	return unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r)
}

// Score 算一条候选在本次查询下的明细与分数。
func (q *Query) Score(e *Entry) Detail {
	var d Detail
	for _, g := range q.groups {
		mh := g.main.hit(e)
		d.Hits = append(d.Hits, mh)

		// strong = 命中 (title || path || leaf) 的关键词数：这一页「就是讲这个的」。
		if mh.Title || mh.Path || mh.Leaf {
			d.Strong++
		}
		// weak = 只在摘要/标签命中：页面里提了一嘴。
		if mh.Body && !(mh.Title || mh.Path || mh.Leaf) {
			d.Weak++
		}
		if mh.Full && !(mh.Title || mh.Path || mh.TOC || mh.Leaf || mh.Body) {
			d.FullOnly++
		}
		if mh.Title {
			d.InTitle++
		}
		if mh.Path {
			d.InPath++
		}
		if mh.TOC {
			d.InTOC++
		}
		if mh.Leaf {
			d.InLeaf++
		}

		// bigram 只加分，且**不参与 strong** —— 这里只累加除 strong 之外的那几项，
		// 最后按该关键词得分的 ×0.5 记入（向下取整）。
		bonus := 0
		for _, bm := range g.approx {
			bh := bm.hit(e)
			d.Hits = append(d.Hits, bh)
			if bh.Title {
				bonus += WeightTitle
			}
			if bh.Path {
				bonus += WeightPath
			}
			if bh.TOC {
				bonus += WeightTOC
			}
			if bh.Leaf {
				bonus += WeightLeaf
			}
			if bh.Body && !(bh.Title || bh.Path || bh.Leaf) {
				bonus += WeightWeak
			}
			if bh.Full && !(bh.Title || bh.Path || bh.TOC || bh.Leaf || bh.Body) {
				bonus += WeightFullOnly
			}
		}
		d.Approx += bonus / 2
	}

	d.Score = d.Strong*WeightStrong +
		d.InTitle*WeightTitle +
		d.InTOC*WeightTOC +
		d.InLeaf*WeightLeaf +
		d.InPath*WeightPath +
		d.Weak*WeightWeak +
		d.FullOnly*WeightFullOnly +
		d.Approx
	return d
}

// hit 把匹配器打到一条候选的所有字段上。
func (m Matcher) hit(e *Entry) Hit {
	h := Hit{Keyword: m.Display, Approx: m.approx}
	h.Title = m.test(e.SearchTitle)
	// leaf = 条目标题（差异 1）；关键词是标识符时**额外**再看路径末段（03 字段表最后一行）。
	h.Leaf = h.Title || (m.ident && m.test(e.SearchPathLeaf))
	h.Path = m.test(e.SearchPath)
	h.TOC = m.test(e.TocText)
	h.Body = m.test(e.SearchMeta)
	h.Full = m.test(e.SearchFull)
	return h
}

// RankOptions 是排序参数。
type RankOptions struct {
	Limit int
	// WithDetails 为 true 时结果里带每个查询的明细（CLI/UI 的 --explain 用）。
	// 不要给 agent 用：它会让响应膨胀几倍（docs/04 的 `explain=1`）。
	WithDetails bool
}

// SearchHit 是一条结果。
type SearchHit struct {
	Entry *Entry
	// Score 是合并后的总分（多查询时相加）。
	Score int
	// Details 与查询一一对应，只有设了 WithDetails 时才有内容。
	Details []Detail
}

// Rank 按 docs/03 的「候选窗口」「多查询合并」「排序」跑一遍。
//
// 每个查询独立完整算分，再按 relPath 合并、分数相加 —— 不把多个查询的词混在一起算：
// `Array map` 这种「所有词都在标题里」的强信号，混词之后几乎永远不成立（线上踩过）。
//
// 超过 50000 篇时的 `--fast` 粗筛没有实现：全表扫 `entries × keywords` 次，
// 5000 篇 × 5 关键词实测 < 20 ms，等真的到那个量级再说。
func Rank(entries []*Entry, queries []*Query, opts RankOptions) []SearchHit {
	limit := opts.Limit
	if limit <= 0 {
		limit = DefaultLimit
	}

	// perQuery = min(max(limit*4, 10), 100)：让「A 查询的第 6 名」也有机会进合并池。
	perQuery := limit * 4
	if perQuery < 10 {
		perQuery = 10
	}
	if perQuery > 100 {
		perQuery = 100
	}

	merged := make(map[string]*SearchHit, len(entries))
	order := make([]string, 0, len(entries))

	for _, q := range queries {
		if q == nil || len(q.groups) == 0 {
			continue
		}
		scored := make([]SearchHit, 0, len(entries))
		for _, e := range entries {
			d := q.Score(e)
			if d.Score <= 0 {
				continue
			}
			h := SearchHit{Entry: e, Score: d.Score}
			if opts.WithDetails {
				h.Details = []Detail{d}
			}
			scored = append(scored, h)
		}
		sortHits(scored)
		if len(scored) > perQuery {
			scored = scored[:perQuery]
		}

		for _, h := range scored {
			if existing, ok := merged[h.Entry.RelPath]; ok {
				existing.Score += h.Score
				if opts.WithDetails {
					existing.Details = append(existing.Details, h.Details...)
				}
				continue
			}
			cp := h
			merged[h.Entry.RelPath] = &cp
			order = append(order, h.Entry.RelPath)
		}
	}

	out := make([]SearchHit, 0, len(order))
	for _, rel := range order {
		out = append(out, *merged[rel])
	}
	sortHits(out)
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// sortHits 是 docs/03 的三级排序：score 降序 → title 字典序升序 → relPath 升序。
//
// title 用 rune 码点序（Go 的字符串比较就是 UTF-8 字节序 = 码点序）。
// 线上版用的是 `localeCompare`，顺序会更「像字典」；这里是三级 tie-breaker，
// 影响面小，而**确定性**更重要 —— 快照以本实现为准生成（ADR-013）。
func sortHits(hits []SearchHit) {
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if a.Score != b.Score {
			return a.Score > b.Score
		}
		if a.Entry.Title != b.Entry.Title {
			return a.Entry.Title < b.Entry.Title
		}
		return a.Entry.RelPath < b.Entry.RelPath
	})
}

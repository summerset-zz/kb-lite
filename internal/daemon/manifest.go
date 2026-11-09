package daemon

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// Manifest 是 `GET /kb/{name}/manifest` 的结果（docs/04）。
//
// 它是给 agent 的「先看哪儿」入口：不知道搜什么词的时候，用一份**按字符预算裁剪**的
// 分层概览来定位。所以它和 search 的目标相反 —— search 要精准，它要便宜。
type Manifest struct {
	Name     string `json:"name"`
	Entries  int    `json:"entries"` // prefix 过滤之后的条目数
	Sections int    `json:"sections"`
	Budget   int    `json:"budget"`
	Prefix   string `json:"prefix,omitempty"`
	Depth    int    `json:"depth"`
	// Dirs 按条目数降序；同一组内的 Items 按字数降序。
	Dirs []ManifestDir `json:"dirs"`
	// Items 是被列出来的条目（受预算限制，顺序与 Dirs 对应）。
	Items []ManifestEntry `json:"items"`
	// Folded 是因为预算不够而没列出来的条目数。
	Folded int `json:"folded"`
}

// ManifestDir 是一个目录分组。
type ManifestDir struct {
	// Path 是 posix 相对路径的目录部分；根目录为空串。
	Path    string `json:"path"`
	Entries int    `json:"entries"`
	Chars   int    `json:"chars"`
	// Shown 表示这个目录**在文本里被列出来了**。
	// 预算不够时整组不出现，只算进末尾那句「还有 N 篇」—— 否则目录行本身
	// 也能把预算吃穿（真实语料上一次多出 1000 多字）。
	Shown bool `json:"shown"`
	// Folded 是这一组里没列出来的条目数。
	Folded int `json:"folded"`
}

// ManifestEntry 是 manifest 里的一行条目。
type ManifestEntry struct {
	Title    string `json:"title"`
	Path     string `json:"path"`
	Sections int    `json:"sections"`
	Chars    int    `json:"chars"`
}

// 默认参数（docs/04）。
const (
	DefaultManifestBudget = 2000
	DefaultManifestDepth  = 3
	maxManifestBudget     = 20000
)

// foldedTailFormat 是超预算时的最后一行。渲染与预算预扣**共用**这一个格式，
// 免得改了一边就悄悄超预算（预扣用的位数比实际多，所以只会偏宽不会偏窄）。
const foldedTailFormat = "… 还有 %d 篇没列出（budget=%d，调大 ?budget= 可以看更多）\n"

// ManifestOptions 是 manifest 的参数。
type ManifestOptions struct {
	Budget int
	Prefix string
	Depth  int
}

// BuildManifest 按预算生成分层概览。
//
// 「预算」算的是**渲染出来的字符数**（给人/模型看的就是那些字），不是 JSON 字节。
//
// 末尾那句「… 还有 N 篇没列出」本身也占字，而它的长度取决于折叠了几篇。做法是
// **先按最长的样子预扣**再排内容 —— 试过「排完发现超了再收紧重排」，但总长度对
// 内容预算不是单调的（收紧一点反而多塞进一行），会在两个值之间来回跳。
func BuildManifest(kb *KBIndex, opts ManifestOptions) Manifest {
	budget := opts.Budget
	if budget <= 0 {
		budget = DefaultManifestBudget
	}
	if budget > maxManifestBudget {
		budget = maxManifestBudget
	}

	reserve := runeLen(fmt.Sprintf(foldedTailFormat, 9999999, budget))
	content := budget - reserve
	if content < 40 {
		content = 40 // 预算小到连表头都放不下时，至少把表头与第一个目录说清楚
	}

	m := buildManifest(kb, content, opts)
	// 对外报告的仍是用户给的那个数（尾巴里那句提示也用它）。
	m.Budget = budget
	return m
}

func buildManifest(kb *KBIndex, budget int, opts ManifestOptions) Manifest {
	depth := opts.Depth
	if depth <= 0 {
		depth = DefaultManifestDepth
	}
	prefix := normalizePrefix(opts.Prefix)

	m := Manifest{Name: kb.KB.Name, Budget: budget, Prefix: prefix, Depth: depth}

	type group struct {
		dir   string
		items []*core.Entry
		chars int
	}
	byDir := map[string]*group{}
	var groups []*group
	for _, e := range kb.Entries {
		if prefix != "" && !strings.HasPrefix(e.RelPath, prefix) {
			continue
		}
		m.Entries++
		m.Sections += len(e.Sections)

		dir := manifestDirOf(e.RelPath, depth)
		g, ok := byDir[dir]
		if !ok {
			g = &group{dir: dir}
			byDir[dir] = g
			groups = append(groups, g)
		}
		g.items = append(g.items, e)
		g.chars += e.Chars
	}

	// 目录按条目数降序，同数量按路径升序（保证输出确定）。
	sort.Slice(groups, func(i, j int) bool {
		if len(groups[i].items) != len(groups[j].items) {
			return len(groups[i].items) > len(groups[j].items)
		}
		return groups[i].dir < groups[j].dir
	})
	for _, g := range groups {
		sort.Slice(g.items, func(i, j int) bool {
			if g.items[i].Chars != g.items[j].Chars {
				return g.items[i].Chars > g.items[j].Chars
			}
			return g.items[i].RelPath < g.items[j].RelPath
		})
	}

	// 表头先占预算；之后一个目录一个目录地加，加不下就整组折叠（docs/04「优先折叠叶子」）。
	used := runeLen(m.headerLine()) + 1
	for _, g := range groups {
		md := ManifestDir{Path: g.dir, Entries: len(g.items), Chars: g.chars}
		cost := runeLen(m.dirLine(md)) + 1
		if used+cost > budget {
			// 连目录行都放不下：这组一行都不出现，只计进末尾那句「还有 N 篇」。
			md.Folded = len(g.items)
			m.Folded += len(g.items)
			m.Dirs = append(m.Dirs, md)
			continue
		}
		used += cost
		md.Shown = true

		shown := 0
		for _, e := range g.items {
			me := ManifestEntry{Title: e.Title, Path: e.RelPath, Sections: len(e.Sections), Chars: e.Chars}
			lineCost := runeLen(m.itemLine(me)) + 1
			if used+lineCost > budget {
				// **一旦放不下就停**，不跳过去试试后面更短的：那样列出来的条目
				// 就不再是「前缀」，渲染时的游标会对不上位置。
				break
			}
			used += lineCost
			shown++
			m.Items = append(m.Items, me)
		}
		md.Folded = len(g.items) - shown
		m.Folded += md.Folded
		m.Dirs = append(m.Dirs, md)
	}
	return m
}

func runeLen(s string) int { return len([]rune(s)) }

// manifestDirOf 取条目的目录，并按 depth 截断层级。
//
// `depth=1` 时 `docs/en/a.md` 归到 `docs`，免得「深目录 × 很多层」把预算全吃掉。
func manifestDirOf(relPath string, depth int) string {
	dir := path.Dir(relPath)
	if dir == "." {
		return ""
	}
	segs := strings.Split(dir, "/")
	if len(segs) > depth {
		return strings.Join(segs[:depth], "/")
	}
	return dir
}

func normalizePrefix(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	if p == "" || p == "." || p == "/" {
		return ""
	}
	if !strings.HasSuffix(p, "/") {
		p += "/"
	}
	return p
}

// RenderManifestText 是默认形态（docs/04 的示例）。
//
// 条目按 Dirs 的顺序输出：Items 的追加顺序与 Dirs 一致，用游标对位即可。
func (m Manifest) RenderManifestText() string {
	var b strings.Builder
	b.WriteString(m.headerLine())
	b.WriteString("\n")

	i := 0
	for _, d := range m.Dirs {
		if !d.Shown {
			continue // 整组没列出来的话，连目录行都不占篇幅
		}
		b.WriteString(m.dirLine(d))
		b.WriteString("\n")
		for n := 0; n < d.Entries-d.Folded && i < len(m.Items); n++ {
			b.WriteString(m.itemLine(m.Items[i]))
			b.WriteString("\n")
			i++
		}
	}
	if m.Folded > 0 {
		fmt.Fprintf(&b, foldedTailFormat, m.Folded, m.Budget)
	}
	return b.String()
}

func (m Manifest) headerLine() string {
	return fmt.Sprintf("%s · %d 篇 / %d 节", m.Name, m.Entries, m.Sections)
}

func (m Manifest) dirLine(d ManifestDir) string {
	label := d.Path
	if label == "" {
		label = "（根目录）"
	} else if !strings.HasSuffix(label, "/") {
		label += "/"
	}
	return fmt.Sprintf("▸ %s  %d 篇  %s 字", label, d.Entries, humanChars(d.Chars))
}

func (m Manifest) itemLine(e ManifestEntry) string {
	return fmt.Sprintf("   %s  %d 节  %s 字", clipTitle(e.Title, e.Path), e.Sections, humanChars(e.Chars))
}

// clipTitle 截断过长的标题。
//
// 不是为了好看：实测语料里有条目因为 frontmatter 没换行，标题变成一整段
// 「Privacy Policykeywords: … description: …」，一个条目就能把整份预算吃光。
func clipTitle(title, relPath string) string {
	if strings.TrimSpace(title) == "" {
		title = path.Base(relPath)
	}
	const max = 40
	runes := []rune(title)
	if len(runes) <= max {
		return title
	}
	return string(runes[:max]) + "…"
}

// humanChars 是「3.4k 字」这种写法：agent 只需要数量级，不需要精确到个位。
func humanChars(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprintf("%d", n)
	case n < 10000:
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	default:
		return fmt.Sprintf("%.0fk", float64(n)/1000)
	}
}

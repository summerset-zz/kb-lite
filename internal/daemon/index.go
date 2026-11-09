package daemon

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// KBIndex 是一个知识库的**只读索引快照**。
//
// 发布之后就不再修改：重建时整体换一个新的 Index，读请求拿到的永远是自洽的那一份。
// 这就是 docs/01「并发」里说的「原子指针交换，读请求不会被阻塞」。
type KBIndex struct {
	KB       KB
	Entries  []*core.Entry
	ByPath   map[string]*core.Entry
	Sections int
	Bytes    int64
	// Warnings 是扫描 / 解码阶段记下的问题，UI 里可看（docs/02「限制与告警」）。
	Warnings []Warning
	// Skipped 是被**整篇跳过**的文件与原因（frontmatter 要求跳过 / 正文为空）。
	// 跳过的东西必须能看见 —— 静默丢弃比多一条噪音糟得多（docs/02「整篇跳过」）。
	Skipped   []SkippedFile
	IndexedAt time.Time
}

// SkippedFile 是一篇被跳过的文件。
type SkippedFile struct {
	RelPath string
	// Reason 是 core.SkipFrontmatter 或 core.SkipEmptyBody。
	Reason string
}

// Entry 按相对路径取一个条目。relPath 必须是 POSIX 形式。
func (k *KBIndex) Entry(relPath string) (*core.Entry, bool) {
	e, ok := k.ByPath[relPath]
	return e, ok
}

// Index 是所有 KB 的快照。
type Index struct {
	KBs     []*KBIndex
	ByName  map[string]*KBIndex
	BuiltAt time.Time
}

func emptyIndex() *Index {
	return &Index{ByName: make(map[string]*KBIndex), BuiltAt: time.Now()}
}

// BuildOptions 是建索引的参数。
type BuildOptions struct {
	// MaxFileBytes <= 0 时用 DefaultMaxFileBytes。
	MaxFileBytes int64
	// MaxSectionChars <= 0 时用 core.DefaultMaxSectionChars。
	MaxSectionChars int
	// Workers <= 0 时用 runtime.GOMAXPROCS(0)。
	Workers int
}

// KBState 是一个 KB 的索引状态（对应 docs/04 `/api/status` 里的 state）。
type KBState struct {
	Name  string
	ID    string
	State string // ready | indexing | error | disabled
	Error string
	Done  int
	Total int
}

// Manager 持有当前索引快照与每个 KB 的索引状态。
type Manager struct {
	cfg  *Config
	opts BuildOptions

	idx atomic.Pointer[Index]

	mu     sync.Mutex
	states map[string]KBState
}

// NewManager 建一个空的 Manager。索引要等 Rebuild() 之后才有内容。
func NewManager(cfg *Config, opts BuildOptions) *Manager {
	m := &Manager{cfg: cfg, opts: opts, states: make(map[string]KBState, len(cfg.KBs))}
	m.idx.Store(emptyIndex())
	return m
}

// Config 返回配置对象（调用方不要改它）。
func (m *Manager) Config() *Config { return m.cfg }

// Index 返回当前快照，**永远非 nil**。
func (m *Manager) Index() *Index { return m.idx.Load() }

// KB 按名字取索引。ok == false 表示这个 KB 不存在、被禁用，或者索引失败。
func (m *Manager) KB(name string) (*KBIndex, bool) {
	k, ok := m.idx.Load().ByName[name]
	return k, ok
}

// Rebuild 重建全部索引，完成后原子替换快照。
//
// v0.1 没有增量：每次都全扫、全切（sha1 跳过与 SQLite 是 v0.2 的事）。
// 单个 KB 失败（根目录不在了之类）不会影响其它 KB —— 记成 error 状态继续。
func (m *Manager) Rebuild() error {
	next := emptyIndex()

	for _, kb := range m.cfg.KBs {
		if !kb.Enabled {
			m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "disabled"})
			continue
		}
		m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "indexing"})

		ki, err := buildKBIndex(kb, m.opts, func(done, total int) {
			m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "indexing", Done: done, Total: total})
		})
		if err != nil {
			m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "error", Error: err.Error()})
			continue
		}

		m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "ready",
			Done: len(ki.Entries), Total: len(ki.Entries)})
		next.KBs = append(next.KBs, ki)
		next.ByName[kb.Name] = ki
	}

	next.BuiltAt = time.Now()
	m.idx.Store(next)
	return nil
}

func (m *Manager) setState(s KBState) {
	m.mu.Lock()
	m.states[s.Name] = s
	m.mu.Unlock()
}

// States 按配置顺序返回各 KB 的索引状态。
func (m *Manager) States() []KBState {
	m.mu.Lock()
	defer m.mu.Unlock()

	out := make([]KBState, 0, len(m.cfg.KBs))
	for _, kb := range m.cfg.KBs {
		if s, ok := m.states[kb.Name]; ok {
			out = append(out, s)
			continue
		}
		out = append(out, KBState{Name: kb.Name, ID: kb.ID, State: "disabled"})
	}
	return out
}

// buildKBIndex 扫一个目录并把每个文件切片。
//
// 并行切片，但**结果按下标回填** —— 条目顺序与 Scan 的输出顺序（RelPath 升序）一致，
// 这样 search 的同分排序在任何机器上都一样（docs/01「并发」）。
func buildKBIndex(kb KB, opts BuildOptions, progress func(done, total int)) (*KBIndex, error) {
	scanned, err := Scan(ScanOptions{
		Root:         kb.RootPath,
		Include:      kb.Include,
		Ignore:       kb.Ignore,
		MaxFileBytes: opts.MaxFileBytes,
	})
	if err != nil {
		return nil, err
	}
	files := scanned.Files

	var (
		mu       sync.Mutex
		warnings = append([]Warning{}, scanned.Warnings...)
		skipped  []SkippedFile
		done     int64
		total    = len(files)
	)
	addWarn := func(kind, msg string) {
		mu.Lock()
		warnings = append(warnings, Warning{Kind: kind, Message: msg})
		mu.Unlock()
	}

	entries := make([]*core.Entry, total)

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > total {
		workers = total
	}
	if workers < 1 {
		workers = 1
	}

	jobs := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				// 每个下标只被一个 worker 写，不需要锁。
				e, skip := readEntry(files[i], opts, addWarn)
				entries[i] = e
				if skip != "" {
					mu.Lock()
					skipped = append(skipped, SkippedFile{RelPath: files[i].RelPath, Reason: skip})
					mu.Unlock()
				}
				if n := atomic.AddInt64(&done, 1); progress != nil && n%50 == 0 {
					progress(int(n), total)
				}
			}
		}()
	}
	for i := range files {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	if progress != nil {
		progress(total, total)
	}

	ki := &KBIndex{
		KB:        kb,
		ByPath:    make(map[string]*core.Entry, total),
		IndexedAt: time.Now(),
	}
	for i, e := range entries {
		if e == nil {
			continue
		}
		ki.Entries = append(ki.Entries, e)
		ki.ByPath[e.RelPath] = e
		ki.Sections += len(e.Sections)
		ki.Bytes += files[i].Size
	}
	sort.Slice(warnings, func(i, j int) bool {
		if warnings[i].Kind != warnings[j].Kind {
			return warnings[i].Kind < warnings[j].Kind
		}
		return warnings[i].Message < warnings[j].Message
	})
	ki.Warnings = warnings
	sort.Slice(skipped, func(i, j int) bool { return skipped[i].RelPath < skipped[j].RelPath })
	ki.Skipped = skipped
	return ki, nil
}

// readEntry 返回条目与跳过原因（原因为空表示没跳过）。
// 读不出来 / 解码失败不是「跳过」而是「告警」：它们属于意外，不属于用户的意图。
func readEntry(f ScannedFile, opts BuildOptions, addWarn func(kind, msg string)) (*core.Entry, string) {
	data, err := os.ReadFile(f.AbsPath)
	if err != nil {
		addWarn(WarnRead, f.RelPath+": 读取失败（"+err.Error()+"）")
		return nil, ""
	}
	text, warn := Decode(data)
	if warn != "" {
		addWarn(WarnEncoding, f.RelPath+": "+warn)
		return nil, ""
	}

	res := core.BuildEntry(f.RelPath, text, core.SliceOptions{MaxSectionChars: opts.MaxSectionChars})
	for _, w := range res.Warnings {
		// core 目前只会报 frontmatter 相关的问题（不合法 / 宽松解析）。
		// 它们**不阻止**这篇被索引：正文已经救回来了，把类别说准很重要。
		addWarn(WarnFrontmatter, f.RelPath+": "+w)
	}
	if res.Skipped {
		return nil, res.SkipReason
	}
	e := res.Entry
	// rev = 内容 sha1 的前 8 位（docs/04「rev 与漂移」）。
	e.Rev = contentRev(text)
	return &e, ""
}

// contentRev 是内容的短指纹。sha1 用在这里是为了「便宜且稳定」，不是安全用途。
func contentRev(s string) string {
	sum := sha1.Sum([]byte(s))
	return hex.EncodeToString(sum[:])[:8]
}

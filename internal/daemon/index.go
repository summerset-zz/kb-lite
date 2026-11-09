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
	// Stats 是这次建索引的增量统计（复用了几篇 / 重切了几篇）。
	Stats IndexStats
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
	// Store 是 SQLite 缓存。为 nil 时纯内存模式下工作（docs/01：驱动不可用时的降级）。
	Store *Store
	// Force 忽略 sha1 缓存，全部重切（`--force`）。
	Force bool
	// ConfigDir 是配置目录。管理接口改完配置要落盘；离线只读时可以为空。
	ConfigDir string
}

// IndexStats 是一次建索引的增量统计。
type IndexStats struct {
	// Reused 是 sha1 没变、直接复用缓存切片的文件数。
	Reused int
	// Parsed 是真正重新切片的文件数。
	Parsed int
	// Skipped 是整篇跳过的文件数（空正文 / frontmatter 要求跳过）。
	Skipped int
	// FromCache 表示这次读到了缓存（false = 首次建索引 / 缓存不可用）。
	FromCache bool
}

// KBState 是一个 KB 的索引状态（对应 docs/04 `/api/status` 里的 state）。
type KBState struct {
	Name  string
	ID    string
	State string // ready | indexing | error | disabled
	Error string
	Done  int
	Total int
	// StartedAt 是本次整理开始的时间（毫秒），只在 indexing 时有意义。
	StartedAt int64
}

// Manager 持有当前索引快照与每个 KB 的索引状态。
type Manager struct {
	cfg  *Config
	opts BuildOptions

	idx atomic.Pointer[Index]

	mu     sync.Mutex
	states map[string]KBState

	// cfgMu 串行化配置改写：UI 与 CLI 可能同时发请求，直接改 m.cfg 会丢更新。
	cfgMu sync.Mutex
	// jobMu 保护 running：全局同时只允许一个整理任务（docs/01）。
	jobMu   sync.Mutex
	running string
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
// 有缓存时按 sha1 跳过未变的文件（docs/01「重建」）：只有内容变了的文件才会重新切片。
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
//
// 增量：先读缓存，文件内容的 sha1 与缓存相同就不解析（这是「几千篇也秒级」的关键）。
// 读文件仍然要做 —— 否则判断不出「变没变」；但读一个文件比解析一篇便宜一个量级。
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
		upsert   []cachedEntry
		skipRows []skipRow
		stats    IndexStats
		done     int64
		total    = len(files)
	)
	addWarn := func(kind, msg string) {
		mu.Lock()
		warnings = append(warnings, Warning{Kind: kind, Message: msg})
		mu.Unlock()
	}

	var cache *kbCache
	if opts.Store != nil {
		c, err := opts.Store.LoadKB(kb.ID)
		switch {
		case err != nil:
			// 缓存坏了不是致命错误：从零切一遍就行。但要说出来 —— 静默变慢很难查。
			addWarn(WarnCache, "读取缓存失败，本次全量重建（"+err.Error()+"）")
		default:
			cache = c
			// FromCache 是「真的用了缓存」：刚建的空库不算，否则首次启动的统计会骗人。
			stats.FromCache = len(c.Entries) > 0 || len(c.Skipped) > 0
		}
	}

	metaOf := make(map[string]fileMeta, total)
	for _, f := range files {
		metaOf[f.RelPath] = fileMeta{Size: f.Size, ModTime: f.ModTimeMS}
	}

	entries := make([]*core.Entry, total)
	// 这篇以前是条目、这次整篇跳过了 —— DB 里的旧条目必须删掉。
	becameSkipped := make(map[string]bool)

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
				f := files[i]
				// 每个下标只被一个 worker 写，不需要锁。
				data, readErr := os.ReadFile(f.AbsPath)
				if readErr != nil {
					addWarn(WarnRead, f.RelPath+": 读取失败（"+readErr.Error()+"）")
				} else {
					sum := sha1Hex(data)
					var hit *cachedEntry
					var hitSkip *cachedSkip
					if cache != nil && !opts.Force {
						if ce, ok := cache.Entries[f.RelPath]; ok && ce.SHA1 == sum {
							hit = &ce
						} else if cs, ok := cache.Skipped[f.RelPath]; ok && cs.SHA1 == sum {
							hitSkip = &cs
						}
					}
					switch {
					case hit != nil:
						entries[i] = hit.Entry
						for _, warn := range hit.Warnings {
							addWarn(warn.Kind, warn.Message)
						}
						mu.Lock()
						stats.Reused++
						mu.Unlock()
					case hitSkip != nil:
						mu.Lock()
						skipped = append(skipped, SkippedFile{RelPath: f.RelPath, Reason: hitSkip.Reason})
						skipRows = append(skipRows, skipRow{RelPath: f.RelPath, SHA1: sum, Reason: hitSkip.Reason})
						becameSkipped[f.RelPath] = true
						mu.Unlock()
					default:
						e, skip, warns := parseEntry(f, data, opts)
						for _, warn := range warns {
							addWarn(warn.Kind, warn.Message)
						}
						if skip != "" {
							mu.Lock()
							skipped = append(skipped, SkippedFile{RelPath: f.RelPath, Reason: skip})
							skipRows = append(skipRows, skipRow{RelPath: f.RelPath, SHA1: sum, Reason: skip})
							becameSkipped[f.RelPath] = true
							mu.Unlock()
							break
						}
						entries[i] = e
						mu.Lock()
						upsert = append(upsert, cachedEntry{SHA1: sum, Entry: e, Warnings: warns})
						stats.Parsed++
						mu.Unlock()
					}
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
	stats.Skipped = len(skipped)
	ki.Stats = stats

	if opts.Store != nil {
		// 卸载：缓存里有、这次目录里没有（或者这次变成跳过了）的条目。
		present := make(map[string]bool, len(files))
		for _, f := range files {
			present[f.RelPath] = true
		}
		var removed []string
		if cache != nil {
			for rel := range cache.Entries {
				if !present[rel] || becameSkipped[rel] {
					removed = append(removed, rel)
				}
			}
		}
		sort.Strings(removed)
		sort.Slice(skipRows, func(i, j int) bool { return skipRows[i].RelPath < skipRows[j].RelPath })
		if err := opts.Store.SaveKB(kb.ID, kbWrite{Upsert: upsert, Skipped: skipRows, Removed: removed}, metaOf, ki.IndexedAt); err != nil {
			// 写不进缓存不影响服务：内存索引已经对了，只是下次还要重切。
			ki.Warnings = append(ki.Warnings, Warning{WarnCache, "写入缓存失败，下次启动会全量重建（" + err.Error() + "）"})
		}
	}
	return ki, nil
}

// parseEntry 解析一篇已经读进内存的文件。
//
// 返回条目、跳过原因（空 = 没跳过）以及这篇产生的告警。
// 告警是**返回值**而不是回调：它们要跟着条目一起写进缓存，否则下次复用时
// 这些告警就消失了（第一次启动有 8 条 frontmatter 告警、第二次 0 条，那才是真的误导）。
func parseEntry(f ScannedFile, data []byte, opts BuildOptions) (*core.Entry, string, []Warning) {
	var warns []Warning
	text, warn := Decode(data)
	if warn != "" {
		return nil, "", []Warning{{WarnEncoding, f.RelPath + ": " + warn}}
	}

	res := core.BuildEntry(f.RelPath, text, core.SliceOptions{MaxSectionChars: opts.MaxSectionChars})
	for _, w := range res.Warnings {
		// core 目前只会报 frontmatter 相关的问题（不合法 / 宽松解析）。
		// 它们**不阻止**这篇被索引：正文已经救回来了，把类别说准很重要。
		warns = append(warns, Warning{WarnFrontmatter, f.RelPath + ": " + w})
	}
	if res.Skipped {
		return nil, res.SkipReason, warns
	}
	e := res.Entry
	// rev = 内容 sha1 的前 8 位（docs/04「rev 与漂移」）。
	e.Rev = contentRev(text)
	return &e, "", warns
}

// sha1Hex 是**文件原始字节**的 sha1，用于判断「这个文件变没变」。
//
// 不要拿它当对外可见的 rev：rev 是解码后正文的指纹（见 contentRev），
// 两者在 BOM / CRLF / GB18030 的文件上并不相同。
func sha1Hex(data []byte) string {
	sum := sha1.Sum(data)
	return hex.EncodeToString(sum[:])
}

// contentRev 是内容的短指纹。sha1 用在这里是为了「便宜且稳定」，不是安全用途。
func contentRev(s string) string {
	return sha1Hex([]byte(s))[:8]
}

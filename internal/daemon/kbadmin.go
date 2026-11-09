package daemon

import (
	"errors"
	"strings"
	"time"
	"unicode"
)

// 管理接口的错误（docs/04）。HTTP 层据此选状态码。
var (
	ErrKBNotFound  = errors.New("没有这个知识库")
	ErrKBDuplicate = errors.New("知识库重名")
	ErrInvalidName = errors.New("知识库名称不合法")
	ErrInvalidRoot = errors.New("目录不可用")
	ErrNoConfigDir = errors.New("不知道配置目录，改不了配置")
)

// ValidateKBName 校验知识库名称（docs/04）：1 到 64 字，不含路径分隔符与控制字符。
//
// 名字是 URL 的一段，也是 agent 唯一会看到的人话标识，所以不允许歧义字符。
func ValidateKBName(name string) error {
	n := len([]rune(name))
	if n == 0 || n > 64 {
		return ErrInvalidName
	}
	if strings.ContainsAny(name, `/\`) {
		return ErrInvalidName
	}
	if strings.ContainsFunc(name, unicode.IsControl) {
		return ErrInvalidName
	}
	if name == "." || name == ".." {
		return ErrInvalidName
	}
	return nil
}

// cfgKB 按名字找一个配置项。
func (m *Manager) cfgKB(name string) (KB, bool) {
	for _, kb := range m.cfg.KBs {
		if kb.Name == name {
			return kb, true
		}
	}
	return KB{}, false
}

// ConfigDirPath 返回配置目录（管理接口改完配置要落盘）。
func (m *Manager) ConfigDirPath() string { return m.opts.ConfigDir }

// UpdateConfig 串行地改配置并落盘。
//
// **只能走这里**：UI 与 CLI 可能同时发请求，直接改 m.cfg 会丢更新。
func (m *Manager) UpdateConfig(fn func(c *Config) error) error {
	dir := m.opts.ConfigDir
	if dir == "" {
		return ErrNoConfigDir
	}
	m.cfgMu.Lock()
	defer m.cfgMu.Unlock()

	if err := fn(m.cfg); err != nil {
		return err
	}
	return m.cfg.Save(dir)
}

// CreateKB 新增一个知识库并立刻开始首次整理。
func (m *Manager) CreateKB(kb KB) (KB, error) {
	if err := ValidateKBName(kb.Name); err != nil {
		return KB{}, err
	}
	if err := ensureDir(kb.RootPath); err != nil {
		return KB{}, err
	}
	if kb.ID == "" {
		id, err := NewKBID()
		if err != nil {
			return KB{}, err
		}
		kb.ID = id
	}
	if !kb.Enabled {
		kb.Enabled = true // 新建默认开启（手写 JSON 时的缺省语义这里也一样）
	}
	err := m.UpdateConfig(func(c *Config) error {
		for _, old := range c.KBs {
			if old.Name == kb.Name {
				return ErrKBDuplicate
			}
		}
		c.KBs = append(c.KBs, kb)
		return nil
	})
	if err != nil {
		return KB{}, err
	}
	m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "indexing"})
	_, _ = m.ReindexAsync(kb.Name, false)
	return kb, nil
}

// KBPatch 是 PATCH /api/kbs/{name} 的可改字段。nil 表示不改。
type KBPatch struct {
	Name     *string   `json:"name"`
	RootPath *string   `json:"rootPath"`
	Enabled  *bool     `json:"enabled"`
	Ignore   *[]string `json:"ignore"`
	Include  *[]string `json:"include"`
}

// PatchKB 改一个知识库。返回是否改了影响索引的字段（改了就重新整理）。
func (m *Manager) PatchKB(name string, p KBPatch) (bool, error) {
	var needReindex, renamed bool
	err := m.UpdateConfig(func(c *Config) error {
		idx := -1
		for i := range c.KBs {
			if c.KBs[i].Name == name {
				idx = i
				break
			}
		}
		if idx < 0 {
			return ErrKBNotFound
		}
		kb := &c.KBs[idx]

		if p.Name != nil && *p.Name != name {
			if err := ValidateKBName(*p.Name); err != nil {
				return err
			}
			for _, other := range c.KBs {
				if other.Name == *p.Name {
					return ErrKBDuplicate
				}
			}
			kb.Name = *p.Name
			renamed = true
		}
		if p.RootPath != nil && *p.RootPath != kb.RootPath {
			if err := ensureDir(*p.RootPath); err != nil {
				return err
			}
			kb.RootPath = *p.RootPath
			needReindex = true
		}
		if p.Enabled != nil {
			kb.Enabled = *p.Enabled
		}
		if p.Ignore != nil {
			kb.Ignore = *p.Ignore
			needReindex = true
		}
		if p.Include != nil {
			kb.Include = *p.Include
			needReindex = true
		}
		return nil
	})
	if err != nil {
		return false, err
	}

	if renamed {
		m.renameInIndex(name, *p.Name)
	}
	return needReindex || renamed, nil
}

// RemoveKB 从配置与索引里移除一个知识库。
//
// **只删关联与索引条目，绝不碰用户的目录**（ADR-004）。返回删掉的条目与节数，
// 让 UI 的确认文案能说清自己做了什么。
func (m *Manager) RemoveKB(name string) (entries, sections int, err error) {
	if k, ok := m.KB(name); ok {
		entries, sections = len(k.Entries), k.Sections
	}
	id := ""
	err = m.UpdateConfig(func(c *Config) error {
		keep := c.KBs[:0]
		found := false
		for _, kb := range c.KBs {
			if kb.Name == name {
				found = true
				id = kb.ID
				continue
			}
			keep = append(keep, kb)
		}
		if !found {
			return ErrKBNotFound
		}
		c.KBs = keep
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	if id != "" && m.opts.Store != nil {
		// 缓存删不掉不影响正确性（下次启动对不上 sha1 就会重切），所以只忽略错误。
		_ = m.opts.Store.ClearKB(id)
	}
	m.dropFromIndex(name)
	m.deleteState(name)
	return entries, sections, nil
}

// SetEnabled 启用或禁用一个知识库。
//
// 禁用**不动索引**：重新启用不需要重切（docs/04）。
func (m *Manager) SetEnabled(name string, enabled bool) error {
	err := m.UpdateConfig(func(c *Config) error {
		for i := range c.KBs {
			if c.KBs[i].Name == name {
				c.KBs[i].Enabled = enabled
				return nil
			}
		}
		return ErrKBNotFound
	})
	if err != nil {
		return err
	}
	if !enabled {
		// 索引留着，但检索面要立刻 503。用状态表达，而不是删索引。
		if st, ok := m.stateOf(name); ok {
			st.State = "disabled"
			m.setState(st)
		} else {
			m.setState(KBState{Name: name, State: "disabled"})
		}
		return nil
	}
	if k, ok := m.KB(name); ok && len(k.Entries) > 0 {
		m.setState(KBState{Name: name, ID: k.KB.ID, State: "ready",
			Done: len(k.Entries), Total: len(k.Entries)})
		return nil
	}
	_, _ = m.ReindexAsync(name, false)
	return nil
}

// ReindexAsync 在后台重建一个知识库。already 为 true 表示已经有任务在跑。
//
// 全局只允许一个整理任务（docs/01）：同时跑两个会抢同一个 SQLite 连接，
// 而且进度条也没法表达。
func (m *Manager) ReindexAsync(name string, force bool) (already bool, err error) {
	kb, ok := m.cfgKB(name)
	if !ok {
		return false, ErrKBNotFound
	}

	m.jobMu.Lock()
	if m.running != "" {
		m.jobMu.Unlock()
		return true, nil
	}
	m.running = name
	m.jobMu.Unlock()

	opts := m.opts
	opts.Force = force
	m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "indexing", StartedAt: time.Now().UnixMilli()})

	go func() {
		defer func() {
			m.jobMu.Lock()
			m.running = ""
			m.jobMu.Unlock()
		}()

		ki, buildErr := buildKBIndex(kb, opts, func(done, total int) {
			m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "indexing",
				Done: done, Total: total, StartedAt: time.Now().UnixMilli()})
		})
		if buildErr != nil {
			m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "error", Error: buildErr.Error()})
			return
		}
		m.swapKB(ki)
		m.setState(KBState{Name: kb.Name, ID: kb.ID, State: "ready",
			Done: len(ki.Entries), Total: len(ki.Entries)})
	}()
	return false, nil
}

// Running 返回当前正在整理的知识库名字（没有就是空串）。
func (m *Manager) Running() string {
	m.jobMu.Lock()
	defer m.jobMu.Unlock()
	return m.running
}

// ---- 索引快照的局部替换 ----

// swapKB 用新的 KBIndex 替换快照里同名的那个（没有就追加）。
func (m *Manager) swapKB(ki *KBIndex) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cur := m.idx.Load()
	next := &Index{BuiltAt: time.Now()}
	next.ByName = make(map[string]*KBIndex, len(cur.KBs)+1)
	replaced := false
	for _, k := range cur.KBs {
		if k.KB.Name == ki.KB.Name {
			next.KBs = append(next.KBs, ki)
			replaced = true
			continue
		}
		next.KBs = append(next.KBs, k)
	}
	if !replaced {
		next.KBs = append(next.KBs, ki)
	}
	for _, k := range next.KBs {
		next.ByName[k.KB.Name] = k
	}
	m.idx.Store(next)
}

func (m *Manager) dropFromIndex(name string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cur := m.idx.Load()
	next := &Index{BuiltAt: time.Now()}
	next.ByName = make(map[string]*KBIndex, len(cur.KBs))
	for _, k := range cur.KBs {
		if k.KB.Name == name {
			continue
		}
		next.KBs = append(next.KBs, k)
		next.ByName[k.KB.Name] = k
	}
	m.idx.Store(next)
}

// renameInIndex 只改索引里的显示名（id 不变，条目不用重切）。
//
// ⚠️ 这里**自己拿 m.mu 并直接操作 states**，不能再调 stateOf/setState：
// 它们是同一把不可重入的锁，嵌套调用会死锁（踩过一次）。
func (m *Manager) renameInIndex(oldName, newName string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	cur := m.idx.Load()
	next := &Index{BuiltAt: time.Now()}
	next.ByName = make(map[string]*KBIndex, len(cur.KBs))
	for _, k := range cur.KBs {
		if k.KB.Name == oldName {
			k.KB.Name = newName
		}
		next.KBs = append(next.KBs, k)
		next.ByName[k.KB.Name] = k
	}
	m.idx.Store(next)

	// 状态表的键也是名字。
	if st, ok := m.states[oldName]; ok {
		st.Name = newName
		delete(m.states, oldName)
		m.states[newName] = st
	}
}

// stateOf 读一个 KB 的状态（没有就是零值）。
func (m *Manager) stateOf(name string) (KBState, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.states[name]
	return s, ok
}

func (m *Manager) deleteState(name string) {
	m.mu.Lock()
	delete(m.states, name)
	m.mu.Unlock()
}

// ---- 目录校验 ----

// ensureDir 要求路径存在且是目录。
func ensureDir(p string) error {
	if strings.TrimSpace(p) == "" {
		return ErrInvalidRoot
	}
	st, err := statDir(p)
	if err != nil || !st {
		return ErrInvalidRoot
	}
	return nil
}

// mu 用在四件事上：索引快照替换、状态表、配置改写、整理任务。
// 都拿同一把锁会让整理期间（长任务）阻塞状态读取，所以拆开，见 Manager 字段注释。

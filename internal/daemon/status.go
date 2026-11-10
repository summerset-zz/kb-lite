package daemon

// Status 是 `GET /api/status` 的响应（docs/04「管理接口」）。
//
// 它同时是 UI 的「首页数据」和调用方判断**数据新鲜度**的依据：
// `state == "indexing"` 时返回的检索结果还是上一次索引的（docs/01「索引期间继续提供旧数据」）。
type Status struct {
	Version string `json:"version"`
	Port    int    `json:"port"`
	// ConfigDir 是配置目录。config.json、index.db 与两个 token 文件都在这里，
	// 界面要能把它显示给用户看（换用户、搬目录、报障靠它）。
	ConfigDir string `json:"configDir"`
	// DBPath 是记录文件的位置，永远等于 <ConfigDir>/index.db。
	DBPath string `json:"dbPath"`
	DBMode string `json:"dbMode"` // sqlite | memory
	// SetupCompleted 为 false 时 UI 进向导。
	SetupCompleted bool `json:"setupCompleted"`
	// LanExposed 表示绑了非回环地址，UI 要挂红条。
	LanExposed bool `json:"lanExposed"`
	// RequireToken 是**实际生效**的读取密码开关（已含“绑局域网必定开”）。
	RequireToken bool `json:"requireToken"`
	// AdminAuth 恒为 true：管理面永远要凭据，这不是可选项。
	AdminAuth bool `json:"adminAuth"`
	// Paused 为真时给 AI 的那一面（/kb/*、/skill.md）返回 503 paused。
	Paused bool       `json:"paused"`
	KBs    []StatusKB `json:"kbs"`
	// User 是跑这个服务的系统用户名。界面用它说“这个目录只有 <user> 能访问”。
	User string `json:"user"`
	// LanAddrs 是本机的局域网 IPv4 地址，用告诉用户局域网上该用哪个地址访问。
	// 拿不到就是空的，界面退回不带具体地址的说法。
	LanAddrs []string `json:"lanAddrs,omitempty"`
}

// StatusKB 是一个知识库的状态。
type StatusKB struct {
	Name     string `json:"name"`
	ID       string `json:"id"`
	RootPath string `json:"rootPath"`
	Enabled  bool   `json:"enabled"`
	// State 是 ready | indexing | error | disabled。
	State         string          `json:"state"`
	Error         string          `json:"error,omitempty"`
	Entries       int             `json:"entries"`
	Sections      int             `json:"sections"`
	Bytes         int64           `json:"bytes"`
	LastIndexedAt int64           `json:"lastIndexedAt"` // 毫秒；0 = 还没索引过
	Warnings      int             `json:"warnings"`
	Progress      *StatusProgress `json:"progress,omitempty"`
}

// StatusProgress 是 reindex 进度（只有 state == indexing 时才有）。
type StatusProgress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// Status 汇总当前状态。port、dbMode 与 requireToken 由调用方给
// （端口被占时会加一，requireToken 要含“绑局域网”这条，都不能从配置直接读）。
func (m *Manager) Status(port int, dbMode string, requireToken bool) Status {
	st := Status{
		Version:        Version,
		Port:           port,
		ConfigDir:      m.opts.ConfigDir,
		DBPath:         m.cfg.DBPath,
		DBMode:         dbMode,
		SetupCompleted: m.cfg.SetupCompleted,
		LanExposed:     !IsLoopbackHost(m.cfg.Host),
		RequireToken:   requireToken,
		AdminAuth:      true,
		KBs:            m.kbStatuses(),
		User:           currentUserName(),
		LanAddrs:       lanAddrs(),
	}
	return st
}

// kbStatuses 按配置顺序返回每个知识库的状态（`/api/status` 与 `/api/kbs` 共用）。
func (m *Manager) kbStatuses() []StatusKB {
	out := make([]StatusKB, 0, len(m.cfg.KBs))
	for _, kb := range m.cfg.KBs {
		out = append(out, m.kbStatusOf(kb))
	}
	return out
}

// kbStatusByName 取一个知识库的状态。找不到时返回零值（调用方已经校验过存在）。
func (m *Manager) kbStatusByName(name string) StatusKB {
	if kb, ok := m.cfgKB(name); ok {
		return m.kbStatusOf(kb)
	}
	return StatusKB{Name: name}
}

func (m *Manager) kbStatusOf(kb KB) StatusKB {
	item := StatusKB{Name: kb.Name, ID: kb.ID, RootPath: kb.RootPath, Enabled: kb.Enabled}

	if s, ok := m.stateOf(kb.Name); ok {
		item.State = s.State
		item.Error = s.Error
		if s.State == "indexing" {
			item.Progress = &StatusProgress{Done: s.Done, Total: s.Total}
		}
	} else if kb.Enabled {
		// 还没跑过整理：不是 ready，但也不是错误。
		item.State = "indexing"
	}

	if k, ok := m.idx.Load().ByName[kb.Name]; ok {
		item.Entries = len(k.Entries)
		item.Sections = k.Sections
		item.Bytes = k.Bytes
		item.Warnings = len(k.Warnings)
		if !k.IndexedAt.IsZero() {
			item.LastIndexedAt = k.IndexedAt.UnixMilli()
		}
	}
	return item
}

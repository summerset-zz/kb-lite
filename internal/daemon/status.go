package daemon

// Status 是 `GET /api/status` 的响应（docs/04「管理接口」）。
//
// 它同时是 UI 的「首页数据」和调用方判断**数据新鲜度**的依据：
// `state == "indexing"` 时返回的检索结果还是上一次索引的（docs/01「索引期间继续提供旧数据」）。
type Status struct {
	Version string `json:"version"`
	Port    int    `json:"port"`
	DBPath  string `json:"dbPath"`
	DBMode  string `json:"dbMode"` // sqlite | memory
	// SetupCompleted 为 false 时 UI 进向导。
	SetupCompleted bool `json:"setupCompleted"`
	// LanExposed 表示绑了非回环地址，UI 要挂红条。
	LanExposed bool `json:"lanExposed"`
	// RequireToken 是**实际生效**的读取密码开关（已含“绑局域网必定开”）。
	RequireToken bool `json:"requireToken"`
	// AdminAuth 恒为 true：管理面永远要凭据，这不是可选项。
	AdminAuth bool       `json:"adminAuth"`
	KBs       []StatusKB `json:"kbs"`
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
		DBPath:         m.cfg.DBPath,
		DBMode:         dbMode,
		SetupCompleted: m.cfg.SetupCompleted,
		LanExposed:     !IsLoopbackHost(m.cfg.Host),
		RequireToken:   requireToken,
		AdminAuth:      true,
		KBs:            make([]StatusKB, 0, len(m.cfg.KBs)),
	}

	idx := m.idx.Load()
	states := m.States()
	byName := make(map[string]KBState, len(states))
	for _, s := range states {
		byName[s.Name] = s
	}

	for _, kb := range m.cfg.KBs {
		item := StatusKB{Name: kb.Name, ID: kb.ID, RootPath: kb.RootPath, Enabled: kb.Enabled}
		if s, ok := byName[kb.Name]; ok {
			item.State = s.State
			item.Error = s.Error
			if s.State == "indexing" {
				item.Progress = &StatusProgress{Done: s.Done, Total: s.Total}
			}
		}
		if k, ok := idx.ByName[kb.Name]; ok {
			item.Entries = len(k.Entries)
			item.Sections = k.Sections
			item.Bytes = k.Bytes
			item.Warnings = len(k.Warnings)
			if !k.IndexedAt.IsZero() {
				item.LastIndexedAt = k.IndexedAt.UnixMilli()
			}
		}
		st.KBs = append(st.KBs, item)
	}
	return st
}

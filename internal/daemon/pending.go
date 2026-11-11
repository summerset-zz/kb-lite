package daemon

// 「有没有改动」的廉价判断。
//
// 这件事以前没做，于是用户让 AI 往目录里写了 86 篇，界面还说“已就绪、0 篇文档”。
//
// 为什么不去看**目录自己的 mtime**：在 Windows（以及大多数文件系统）上，目录的
// 修改时间只在“直接子项被增删改名”时更新，**改文件内容不会更新它**；子目录里的变化
// 也不会冒泡到父目录。拿它当依据必然漏掉“改了内容”，而漏的后果比不检测更糟：
// 界面会给出一个假的“没有改动”。
//
// 真正便宜的部分是 **walk + stat**：读目录项、拿大小与 mtime，不读文件内容、不算 sha1。
// 贵的是读文件与解析，那部分只在 reindex 时做。所以这里就是走一遍目录，跟缓存里
// 记下的 (size, mtime) 对一眼，给出一个“有几篇对不上”的数字。
//
// 它只是**提示**，不是判据：git checkout 会把一堆文件的 mtime 磨掉，于是明明内容一样
// 也会显示“待更新”。真正决定要不要重切的是 sha1（buildKBIndex 里那一套）。

import "time"

// pendingTTL 是缓存的寿命。
//
// 界面每 5 秒轮询一次 /api/status，不缓存就每 5 秒走一遍目录。86 篇是毫秒级，
// 5000 篇是百毫秒级，白白烧 CPU。30 秒的旧结果对“我刚写完文件”来说够快，
// 对目录很大的库又不亏。
const pendingTTL = 30 * time.Second

// pendingEntry 是某个知识库上一次算出来的结果。
type pendingEntry struct {
	at        time.Time
	count     int
	rootPath  string
	indexedAt int64
}

// countPending 数「目录里有、缓存里没有或对不上」的文件。
//
// 返回 0 的情况：知识库被禁用、目录读不了、没有缓存可比（纯内存模式）。
// 目录读不了不在这里报错：那一行另有 error 状态，界面会说清是哪儿不对。
func (m *Manager) countPending(kb KB) int {
	if !kb.Enabled {
		return 0
	}
	store := m.opts.Store
	if store == nil {
		// 纯内存模式（--no-db）：每次启动都重切，没有“待更新”这回事。
		return 0
	}
	scanned, err := Scan(ScanOptions{
		Root:         kb.RootPath,
		Include:      kb.Include,
		Ignore:       kb.Ignore,
		MaxFileBytes: m.opts.MaxFileBytes,
	})
	if err != nil {
		return 0
	}
	known, err := store.StoredMeta(kb.ID)
	if err != nil {
		return 0
	}

	n := 0
	seen := make(map[string]bool, len(scanned.Files))
	for _, f := range scanned.Files {
		seen[f.RelPath] = true
		old, ok := known[f.RelPath]
		if !ok {
			n++ // 新增
			continue
		}
		// Size < 0 表示“只知道它在索引里，没有元信息”（skipped 的那批）。
		if old.Size >= 0 && (old.Size != f.Size || old.ModTime != f.ModTimeMS) {
			n++ // 大小或修改时间变了
		}
	}
	for rel := range known {
		if !seen[rel] {
			n++ // 目录里没有了
		}
	}
	return n
}

// pendingCount 带缓存地算，供 /api/status 用。
//
// 缓存跟着 (rootPath, lastIndexedAt) 走：重新整理过、或者换了目录，立刻重算，
// 不靠 30 秒过期。
func (m *Manager) pendingCount(kb KB, indexedAt int64) int {
	m.pendingMu.Lock()
	prev, ok := m.pending[kb.ID]
	m.pendingMu.Unlock()

	if ok && prev.rootPath == kb.RootPath && prev.indexedAt == indexedAt &&
		time.Since(prev.at) < pendingTTL {
		return prev.count
	}

	n := m.countPending(kb)

	m.pendingMu.Lock()
	if m.pending == nil {
		m.pending = make(map[string]pendingEntry, 4)
	}
	m.pending[kb.ID] = pendingEntry{at: time.Now(), count: n, rootPath: kb.RootPath, indexedAt: indexedAt}
	m.pendingMu.Unlock()
	return n
}

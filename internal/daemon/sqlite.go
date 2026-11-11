package daemon

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// DBSchemaVersion 存在 `PRAGMA user_version` 里（docs/01「配置变更与迁移」）。
//
// 加列 / 改语义就 +1，并且**必须**能在 openStore 里从旧版本迁过来；
// 暂时不支持降级：看到更高的版本就拒绝启动，见 ErrDBNeedsUpgrade。
const DBSchemaVersion = 1

// ErrDBNeedsUpgrade 表示 index.db 由更新的版本写过。不要尝试降级 —— 新版本的字段
// 我们看不懂，硬读只会得到半截索引（docs/01）。
var ErrDBNeedsUpgrade = errors.New("index.db 由更新版本的 kb-lite 写过，请升级后再用")

// ErrDBUnavailable 表示这个构建没有带 SQLite 驱动（`-tags nosqlite`），退回纯内存模式。
var ErrDBUnavailable = errors.New("这个构建没有 SQLite 驱动（-tags nosqlite），使用纯内存模式")

// Store 是 SQLite 缓存。**它只是缓存**（ADR-003）：删掉它只损失启动时间，不损失任何数据。
type Store struct {
	db       *sql.DB
	path     string
	readOnly bool
}

// cachedEntry 是 DB 里一条 entry 的可复用形态。
//
// Entry 里的指针是**共享**的：复用出来的条目会直接进新的索引快照。
// 这不危险，因为 Entry 一旦构建就不再改动（docs/01「并发」）。
type cachedEntry struct {
	SHA1     string
	Entry    *core.Entry
	Warnings []Warning
}

// cachedSkip 是一条「上次就判定为整篇跳过」的记录。存 sha1 是为了让没变的空文件
// 下次连解析都不用做（否则每次启动都得重算一遍 frontmatter）。
type cachedSkip struct {
	SHA1   string
	Reason string
}

// kbCache 是一个 KB 的缓存内容。
type kbCache struct {
	Entries map[string]cachedEntry
	Skipped map[string]cachedSkip
}

// skipRow 是一条要写回去的跳过记录。
type skipRow struct {
	RelPath string
	SHA1    string
	Reason  string
}

// kbWrite 是一次索引要落盘的内容。
type kbWrite struct {
	// Upsert 是本次真正解析出来的条目（复用的不用重写）。
	Upsert []cachedEntry
	// Skipped 全量覆盖：跳过清单很小，整体重写比做差集省事也更容易对齐。
	Skipped []skipRow
	// Removed 要从 DB 抹掉的 rel_path：目录里没有了、或者这次变成了整篇跳过。
	Removed []string
}

// fileMeta 是写缓存时顺带记下的文件元信息。
//
// **不参与「变没变」的判断**（那个只看 sha1）：mtime 会被 git checkout / 复制磨掉，
// 拿它当依据会漏掉真正改过的文件。这里存下来只是为了诊断时能对一眼。
type fileMeta struct {
	Size    int64
	ModTime int64
}

// OpenStore 打开（必要时创建）缓存库。只有 daemon 用它（docs/01「只有一个进程能写索引」）。
func OpenStore(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	s := &Store{path: path}
	if err := s.open(false); err != nil {
		return nil, err
	}
	return s, nil
}

// OpenStoreReadOnly 打开一个**只用来读取**的缓存库。
//
// CLI 的离线模式走这条路径：它仍然会扫目录、比 sha1（所以结果不会是旧的），
// 但不写库 —— 写索引只允许 daemon 做，免得抢 WAL 锁（docs/01 / docs/05）。
//
// 注意用的是 `PRAGMA query_only` 而**不是** DSN 的 `mode=ro`：WAL 库在只读模式下
// 连 `-shm` 都需要写权限，真跟 daemon 同时开着时会打不开。
func OpenStoreReadOnly(path string) (*Store, error) {
	if _, err := os.Stat(path); err != nil {
		return nil, err
	}
	s := &Store{path: path, readOnly: true}
	if err := s.open(true); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) open(readOnly bool) error {
	db, err := openSQLite(s.path)
	if err != nil {
		return err
	}
	// modernc 的驱动在同一个文件上开多条连接会互相等锁；索引本来就是单线程写，
	// 一条连接足够，也顺手让「读到的永远是同一个快照」成立。
	db.SetMaxOpenConns(1)
	s.db = db

	if readOnly {
		// 只读时不改 journal_mode（那是写操作），只要不写就行。
		if _, err := db.Exec("PRAGMA query_only=1"); err != nil {
			db.Close()
			return err
		}
	} else {
		if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
			db.Close()
			return err
		}
		if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
			db.Close()
			return err
		}
	}

	ver, err := s.userVersion()
	if err != nil {
		db.Close()
		return err
	}
	if ver > DBSchemaVersion {
		db.Close()
		return ErrDBNeedsUpgrade
	}
	if !readOnly && ver < DBSchemaVersion {
		if err := s.migrate(); err != nil {
			db.Close()
			return err
		}
	}
	return nil
}

func (s *Store) userVersion() (int, error) {
	var v int
	err := s.db.QueryRow("PRAGMA user_version").Scan(&v)
	return v, err
}

// migrate 建表。schema 见 docs/01「SQLite（缓存，可随时删）」。
//
// 比文档多出来的列（rev / search_* / toc / toc_text / warn 表 / skipped 的 sha1）
// 是**实现必需**的：内存索引要能只靠 DB 重建，否则每次启动还是得重新切片，
// 「二次启动 < 500 ms」就不成立。文档已同步说明。
func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)`,
		`CREATE TABLE IF NOT EXISTS entry (
			kb_id            TEXT NOT NULL,
			rel_path         TEXT NOT NULL,
			sha1             TEXT NOT NULL,
			mtime_ms         INTEGER NOT NULL,
			size_bytes       INTEGER NOT NULL,
			title            TEXT NOT NULL,
			summary          TEXT NOT NULL,
			tags             TEXT NOT NULL,
			search_text      TEXT NOT NULL,
			char_count       INTEGER NOT NULL,
			rev              TEXT NOT NULL,
			search_title     TEXT NOT NULL,
			search_path      TEXT NOT NULL,
			search_path_leaf TEXT NOT NULL,
			search_meta      TEXT NOT NULL,
			toc_text         TEXT NOT NULL,
			toc              TEXT NOT NULL,
			indexed_at       INTEGER NOT NULL,
			PRIMARY KEY (kb_id, rel_path)
		)`,
		`CREATE TABLE IF NOT EXISTS section (
			kb_id      TEXT NOT NULL,
			rel_path   TEXT NOT NULL,
			section_no INTEGER NOT NULL,
			level      INTEGER NOT NULL,
			title      TEXT NOT NULL,
			path       TEXT NOT NULL,
			path_text  TEXT NOT NULL,
			chars      INTEGER NOT NULL,
			content    TEXT NOT NULL,
			PRIMARY KEY (kb_id, rel_path, section_no)
		)`,
		`CREATE TABLE IF NOT EXISTS warn (
			kb_id    TEXT NOT NULL,
			rel_path TEXT NOT NULL,
			kind     TEXT NOT NULL,
			message  TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS skipped (
			kb_id    TEXT NOT NULL,
			rel_path TEXT NOT NULL,
			sha1     TEXT NOT NULL,
			reason   TEXT NOT NULL,
			PRIMARY KEY (kb_id, rel_path)
		)`,
		// tombstone 曾经用来记“这个文件被删了”，但它**只写不读**：
		// 没有任何代码查它，删 KB 时也不清，改个文件名就多一行，于是一直涨。
		// 诊断这件事已经由 `kb-lite doctor` 干了（它扫磁盘对比缓存里的 rel_path），
		// 所以这里直接把旧库里的那张表扔掉。
		`DROP TABLE IF EXISTS tombstone`,
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	for _, q := range stmts {
		if _, err := tx.Exec(q); err != nil {
			tx.Rollback()
			return err
		}
	}
	if _, err := tx.Exec(fmt.Sprintf("PRAGMA user_version=%d", DBSchemaVersion)); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// Close 关掉连接。
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

// Path 返回缓存文件路径（CLI 打日志用）。
func (s *Store) Path() string { return s.path }

// ReadOnly 表示这是一条只读连接。
func (s *Store) ReadOnly() bool { return s.readOnly }

// StoredPaths 返回某个 KB 在缓存里登记过的相对路径（doctor 用）。
//
// 只读、只取一列：doctor 要回答“索引里有多少条记录的源文件已经不见了”，
// 不需要把正文全部读出来。
func (s *Store) StoredPaths(kbID string) ([]string, error) {
	rows, err := s.db.Query(`SELECT rel_path FROM entry WHERE kb_id = ?`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []string
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// StoredMeta 读缓存里每个文件的 (size, mtime)，用来算「有几篇对不上」（见 pending.go）。
//
// 顺带把 skipped 的路径也带上（`Size` 记成 -1）：那些文件在索引里「已知不需要收录」，
// 不带就把它们每次都算成新增，界面会永远显示「有 N 篇待更新」。
func (s *Store) StoredMeta(kbID string) (map[string]fileMeta, error) {
	out := make(map[string]fileMeta, 64)

	rows, err := s.db.Query(`SELECT rel_path, size_bytes, mtime_ms FROM entry WHERE kb_id = ?`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			rel string
			m   fileMeta
		)
		if err := rows.Scan(&rel, &m.Size, &m.ModTime); err != nil {
			return nil, err
		}
		out[rel] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	srows, err := s.db.Query(`SELECT rel_path FROM skipped WHERE kb_id = ?`, kbID)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var rel string
		if err := srows.Scan(&rel); err != nil {
			return nil, err
		}
		if _, ok := out[rel]; !ok {
			out[rel] = fileMeta{Size: -1}
		}
	}
	return out, srows.Err()
}

// LoadKB 读出一个 KB 的全部缓存。
func (s *Store) LoadKB(kbID string) (*kbCache, error) {
	c := &kbCache{Entries: map[string]cachedEntry{}, Skipped: map[string]cachedSkip{}}

	rows, err := s.db.Query(`SELECT rel_path, sha1, title, summary, tags, search_text, char_count,
		rev, search_title, search_path, search_path_leaf, search_meta, toc_text, toc
		FROM entry WHERE kb_id = ?`, kbID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			rel, sha, title, summary, tags, searchText string
			chars                                      int
			rev, sTitle, sPath, sLeaf, sMeta, tocText  string
			toc                                        string
		)
		if err := rows.Scan(&rel, &sha, &title, &summary, &tags, &searchText, &chars,
			&rev, &sTitle, &sPath, &sLeaf, &sMeta, &tocText, &toc); err != nil {
			return nil, err
		}
		e := &core.Entry{
			RelPath: rel, Title: title, Summary: summary, Chars: chars, Rev: rev,
			SearchTitle: sTitle, SearchPath: sPath, SearchPathLeaf: sLeaf,
			SearchMeta: sMeta, TocText: tocText, SearchFull: searchText,
		}
		if err := json.Unmarshal([]byte(tags), &e.Tags); err != nil {
			return nil, fmt.Errorf("%s: tags 坏了：%w", rel, err)
		}
		if err := json.Unmarshal([]byte(toc), &e.TOC); err != nil {
			return nil, fmt.Errorf("%s: toc 坏了：%w", rel, err)
		}
		c.Entries[rel] = cachedEntry{SHA1: sha, Entry: e}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// 显式关掉：连接池只有一条连接，后面还要跑别的查询。
	rows.Close()

	if err := s.loadSections(kbID, c); err != nil {
		return nil, err
	}
	if err := s.loadWarns(kbID, c); err != nil {
		return nil, err
	}
	if err := s.loadSkipped(kbID, c); err != nil {
		return nil, err
	}
	return c, nil
}

func (s *Store) loadSections(kbID string, c *kbCache) error {
	rows, err := s.db.Query(`SELECT rel_path, section_no, level, title, path, path_text, chars, content
		FROM section WHERE kb_id = ? ORDER BY rel_path, section_no`, kbID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			rel, title, pathJSON, pathText, content string
			no, level, chars                        int
		)
		if err := rows.Scan(&rel, &no, &level, &title, &pathJSON, &pathText, &chars, &content); err != nil {
			return err
		}
		ce, ok := c.Entries[rel]
		if !ok {
			continue // section 所属的 entry 不在（脏数据），直接跳过，反正下次会重建
		}
		var p []string
		if err := json.Unmarshal([]byte(pathJSON), &p); err != nil {
			return fmt.Errorf("%s#%d: path 坏了：%w", rel, no, err)
		}
		ce.Entry.Sections = append(ce.Entry.Sections, core.Section{
			No: no, Level: level, Title: title, Path: p, PathText: pathText,
			Content: content, Chars: chars,
		})
	}
	return rows.Err()
}

func (s *Store) loadWarns(kbID string, c *kbCache) error {
	rows, err := s.db.Query(`SELECT rel_path, kind, message FROM warn WHERE kb_id = ?`, kbID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rel, kind, msg string
		if err := rows.Scan(&rel, &kind, &msg); err != nil {
			return err
		}
		ce, ok := c.Entries[rel]
		if !ok {
			continue
		}
		ce.Warnings = append(ce.Warnings, Warning{Kind: kind, Message: msg})
		c.Entries[rel] = ce
	}
	return rows.Err()
}

func (s *Store) loadSkipped(kbID string, c *kbCache) error {
	rows, err := s.db.Query(`SELECT rel_path, sha1, reason FROM skipped WHERE kb_id = ?`, kbID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var rel, sha, reason string
		if err := rows.Scan(&rel, &sha, &reason); err != nil {
			return err
		}
		c.Skipped[rel] = cachedSkip{SHA1: sha, Reason: reason}
	}
	return rows.Err()
}

// SaveKB 把一次索引的结果写回缓存。**一个事务**：中途失败不会留下半截索引。
func (s *Store) SaveKB(kbID string, w kbWrite, meta map[string]fileMeta, now time.Time) error {
	if s.readOnly {
		return nil
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	fail := func(err error) error {
		tx.Rollback()
		return err
	}

	for _, rel := range w.Removed {
		if _, err := tx.Exec(`DELETE FROM entry WHERE kb_id=? AND rel_path=?`, kbID, rel); err != nil {
			return fail(err)
		}
		if _, err := tx.Exec(`DELETE FROM section WHERE kb_id=? AND rel_path=?`, kbID, rel); err != nil {
			return fail(err)
		}
		if _, err := tx.Exec(`DELETE FROM warn WHERE kb_id=? AND rel_path=?`, kbID, rel); err != nil {
			return fail(err)
		}
	}

	for _, ce := range w.Upsert {
		e := ce.Entry
		// 空条目一律不写：这里曾经因为上层的一个 nil 直接 panic，而 panic 发生在
		// 后台整理 goroutine 里 —— 一个坏文件就能把整个服务干掉。缓存的写入不值得
		// 为此担风险：这条没写成，下次重新切片就好。
		if e == nil {
			continue
		}
		tags, err := json.Marshal(e.Tags)
		if err != nil {
			return fail(err)
		}
		toc, err := json.Marshal(e.TOC)
		if err != nil {
			return fail(err)
		}
		if _, err := tx.Exec(`INSERT OR REPLACE INTO entry
			(kb_id, rel_path, sha1, mtime_ms, size_bytes, title, summary, tags, search_text,
			 char_count, rev, search_title, search_path, search_path_leaf, search_meta,
			 toc_text, toc, indexed_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			kbID, e.RelPath, ce.SHA1, meta[e.RelPath].ModTime, meta[e.RelPath].Size, e.Title, e.Summary,
			string(tags), e.SearchFull, e.Chars, e.Rev, e.SearchTitle, e.SearchPath,
			e.SearchPathLeaf, e.SearchMeta, e.TocText, string(toc), now.UnixMilli()); err != nil {
			return fail(err)
		}
		// 节先删后插：节的数量会变，做差集不值得。
		if _, err := tx.Exec(`DELETE FROM section WHERE kb_id=? AND rel_path=?`, kbID, e.RelPath); err != nil {
			return fail(err)
		}
		for _, sec := range e.Sections {
			p, err := json.Marshal(sec.Path)
			if err != nil {
				return fail(err)
			}
			if _, err := tx.Exec(`INSERT INTO section
				(kb_id, rel_path, section_no, level, title, path, path_text, chars, content)
				VALUES (?,?,?,?,?,?,?,?,?)`,
				kbID, e.RelPath, sec.No, sec.Level, sec.Title, string(p), sec.PathText,
				sec.Chars, sec.Content); err != nil {
				return fail(err)
			}
		}
		if _, err := tx.Exec(`DELETE FROM warn WHERE kb_id=? AND rel_path=?`, kbID, e.RelPath); err != nil {
			return fail(err)
		}
		for _, warn := range ce.Warnings {
			if _, err := tx.Exec(`INSERT INTO warn (kb_id, rel_path, kind, message) VALUES (?,?,?,?)`,
				kbID, e.RelPath, warn.Kind, warn.Message); err != nil {
				return fail(err)
			}
		}
	}

	if _, err := tx.Exec(`DELETE FROM skipped WHERE kb_id=?`, kbID); err != nil {
		return fail(err)
	}
	for _, sk := range w.Skipped {
		if _, err := tx.Exec(`INSERT INTO skipped (kb_id, rel_path, sha1, reason) VALUES (?,?,?,?)`,
			kbID, sk.RelPath, sk.SHA1, sk.Reason); err != nil {
			return fail(err)
		}
	}

	if _, err := tx.Exec(`INSERT OR REPLACE INTO meta (key, value) VALUES ('last_index_at', ?)`,
		now.Format(time.RFC3339)); err != nil {
		return fail(err)
	}
	return tx.Commit()
}

// ClearKB 抹掉一个 KB 的缓存（`--force` 全量重建 / KB 被删时用）。
func (s *Store) ClearKB(kbID string) error {
	if s.readOnly {
		return nil
	}
	for _, table := range []string{"entry", "section", "warn", "skipped"} {
		if _, err := s.db.Exec("DELETE FROM "+table+" WHERE kb_id = ?", kbID); err != nil {
			return err
		}
	}
	return nil
}

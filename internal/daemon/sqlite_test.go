package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// openTestStore 在临时目录里开一个缓存库。
func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := OpenStore(filepath.Join(t.TempDir(), "index.db"))
	if err != nil {
		t.Skipf("这个构建没有 SQLite 驱动：%v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

// buildWith 建一遍索引（带缓存），返回 KBIndex。
func buildWith(t *testing.T, m *Manager) *KBIndex {
	t.Helper()
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}
	k, ok := m.KB("笔记")
	if !ok {
		t.Fatal("KB 不存在")
	}
	return k
}

func TestStoreRoundTrip(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "b.md"),
		"---\ntitle: 乙\nsummary: 第二个。\ntags: [ops, 上线]\n---\n\n## 节一\n\n正文内容。\n\n## 节二\n\n空\n")
	writeFile(t, filepath.Join(root, "a.txt"), "纯文本，没有标题。\n")
	writeFile(t, filepath.Join(root, "empty.md"), "---\ntitle: 只有标题\n---\n")

	store := openTestStore(t)
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	first := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	if first.Stats.FromCache {
		t.Error("首次建索引不该读到缓存")
	}
	if first.Stats.Reused != 0 || first.Stats.Parsed != 2 {
		t.Errorf("首次统计 = %+v，想要 Parsed=2 Reused=0", first.Stats)
	}

	// 从库里读回来，逐字段比对：缓存必须能完整重建内存索引（否则二次启动还是得重切）。
	cache, err := store.LoadKB("k1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Entries) != 2 {
		t.Fatalf("缓存里 %d 条，想要 2", len(cache.Entries))
	}
	for _, want := range first.Entries {
		got, ok := cache.Entries[want.RelPath]
		if !ok {
			t.Fatalf("缓存里没有 %s", want.RelPath)
		}
		if got.Entry.Rev != want.Rev || got.Entry.Title != want.Title {
			t.Errorf("%s: rev/title = %q/%q，想要 %q/%q",
				want.RelPath, got.Entry.Rev, got.Entry.Title, want.Rev, want.Title)
		}
		if got.Entry.Chars != want.Chars || got.Entry.SearchFull != want.SearchFull {
			t.Errorf("%s: 正文字段没对上", want.RelPath)
		}
		if len(got.Entry.Sections) != len(want.Sections) {
			t.Fatalf("%s: 节数 = %d，想要 %d", want.RelPath, len(got.Entry.Sections), len(want.Sections))
		}
		for i := range want.Sections {
			a, b := want.Sections[i], got.Entry.Sections[i]
			if a.No != b.No || a.Level != b.Level || a.Title != b.Title ||
				a.Content != b.Content || a.Chars != b.Chars || a.PathText != b.PathText {
				t.Errorf("%s 第 %d 节没对上：\n旧 %+v\n新 %+v", want.RelPath, i, a, b)
			}
			if len(a.Path) != len(b.Path) {
				t.Errorf("%s 第 %d 节的面包屑长度不一样", want.RelPath, i)
			}
		}
	}

	// 空正文的那篇要进 skipped 表，否则下次启动还会再解析一遍。
	if _, ok := cache.Skipped["empty.md"]; !ok {
		t.Error("empty.md 应当记在 skipped 里")
	}
}

func TestIncrementalReusesUnchangedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\n---\n\n## 一\n\n内容\n")
	writeFile(t, filepath.Join(root, "b.md"), "---\ntitle: 乙\n---\n\n## 二\n\n内容\n")

	store := openTestStore(t)
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	first := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))

	// 二次启动：一篇没改 → 全部复用。
	second := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	if !second.Stats.FromCache {
		t.Fatal("第二次应当读到缓存")
	}
	if second.Stats.Reused != 2 || second.Stats.Parsed != 0 {
		t.Errorf("第二次统计 = %+v，想要 Reused=2 Parsed=0", second.Stats)
	}
	assertSameIndex(t, first, second)

	// 改一篇 → 只重切那一篇。
	writeFile(t, filepath.Join(root, "b.md"), "---\ntitle: 乙（改过）\n---\n\n## 二\n\n新内容\n")
	third := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	if third.Stats.Reused != 1 || third.Stats.Parsed != 1 {
		t.Errorf("第三次统计 = %+v，想要 Reused=1 Parsed=1", third.Stats)
	}
	e, ok := third.Entry("b.md")
	if !ok || e.Title != "乙（改过）" {
		t.Errorf("改过的标题没生效：%+v", e)
	}
	if e.Rev == "" {
		t.Error("rev 不该为空")
	}

	// --force：全部重切。
	forced := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store, Force: true}))
	if forced.Stats.Reused != 0 || forced.Stats.Parsed != 2 {
		t.Errorf("--force 统计 = %+v，想要 Reused=0 Parsed=2", forced.Stats)
	}
	assertSameIndex(t, third, forced)
}

// TestDeletingDBGivesSameResult 是 docs/09 的 v0.2 验收：删掉 index.db 后结果完全一致。
func TestDeletingDBGivesSameResult(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"),
		"---\ntitle: 甲\ntags: [x]\n---\n\n## 一\n\n内容甲\n\n### 一·一\n\n更深\n")
	writeFile(t, filepath.Join(root, "b.md"), "---\ntitle: 乙\n---\n\n## 二\n\n内容乙\n")
	writeFile(t, filepath.Join(root, "c.md"), "---\ntitle: 丙\n---\n")

	dbPath := filepath.Join(t.TempDir(), "index.db")
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Skipf("这个构建没有 SQLite 驱动：%v", err)
	}
	cached := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	store.Close()
	if err := os.Remove(dbPath); err != nil {
		t.Fatal(err)
	}

	fresh := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2}))
	assertSameIndex(t, fresh, cached)
}

func TestCacheDropsDeletedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\n---\n\n## 一\n\n内容\n")
	writeFile(t, filepath.Join(root, "b.md"), "---\ntitle: 乙\n---\n\n## 二\n\n内容\n")

	store := openTestStore(t)
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))

	if err := os.Remove(filepath.Join(root, "b.md")); err != nil {
		t.Fatal(err)
	}
	k := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	if _, ok := k.Entry("b.md"); ok {
		t.Error("删掉的文件不该还在索引里")
	}

	cache, err := store.LoadKB("k1")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := cache.Entries["b.md"]; ok {
		t.Error("删掉的文件不该还在缓存里")
	}

	var n int
	if err := store.db.QueryRow(`SELECT COUNT(*) FROM tombstone WHERE kb_id='k1' AND rel_path='b.md'`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Errorf("tombstone 条数 = %d，想要 1（删掉的文件要留痕，便于诊断）", n)
	}
}

// TestCacheKeepsWarnings 保证「第一次启动有告警、第二次就没有了」这种漂移不会发生。
func TestCacheKeepsWarnings(t *testing.T) {
	root := t.TempDir()
	// 值里裸冒号不是合法 YAML → 会走宽松解析并记一条 frontmatter 告警。
	writeFile(t, filepath.Join(root, "bad.md"),
		"---\ntitle: 甲\ndescription: The format: a TTF.\n---\n\n## 一\n\n内容\n")

	store := openTestStore(t)
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	first := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	if len(first.Warnings) == 0 {
		t.Fatal("第一次应当有 frontmatter 告警")
	}

	second := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	if second.Stats.Reused != 1 {
		t.Fatalf("第二次应当复用缓存：%+v", second.Stats)
	}
	if len(second.Warnings) != len(first.Warnings) {
		t.Errorf("复用之后告警丢了：第一次 %d 条，第二次 %d 条\n%v",
			len(first.Warnings), len(second.Warnings), second.Warnings)
	}
}

// TestReadOnlyStoreDoesNotWrite：CLI 离线模式用只读连接，不能污染 daemon 的库。
func TestReadOnlyStoreDoesNotWrite(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\n---\n\n## 一\n\n内容\n")

	dbPath := filepath.Join(t.TempDir(), "index.db")
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	rw, err := OpenStore(dbPath)
	if err != nil {
		t.Skipf("这个构建没有 SQLite 驱动：%v", err)
	}
	buildWith(t, NewManager(cfg, BuildOptions{Workers: 1, Store: rw}))
	rw.Close()

	ro, err := OpenStoreReadOnly(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if !ro.ReadOnly() {
		t.Error("ReadOnly() 应当为 true")
	}
	if _, err := OpenStoreReadOnly(filepath.Join(t.TempDir(), "nope.db")); err == nil {
		t.Error("文件不存在时应当报错，而不是悄悄建一个空库")
	}

	// 读得到旧条目；写不进去（SaveKB 在只读连接上是空操作）。
	cache, err := ro.LoadKB("k1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cache.Entries) != 1 {
		t.Fatalf("缓存里 %d 条，想要 1", len(cache.Entries))
	}
	if err := ro.SaveKB("k1", kbWrite{}, nil, time.Now()); err != nil {
		t.Errorf("只读连接的 SaveKB 应当静默成功（空操作）：%v", err)
	}
}

// BenchmarkRebuildColdAndWarm 是 docs/09 的 v0.2 验收指标的落脚点：
// 「1000 篇的库二次启动 < 500 ms」。跑法：
//
//	go test -run XXX -bench BenchmarkRebuild -benchtime 3x ./internal/daemon
func BenchmarkRebuildColdAndWarm(b *testing.B) {
	root := b.TempDir()
	body := strings.Repeat("这一段模拟真实笔记的正文长度与中英混排。标准写法是 GB 18030。\n", 20)
	for i := 0; i < 1000; i++ {
		var sub string
		if i%2 == 0 {
			sub = "zh"
		} else {
			sub = "en"
		}
		p := filepath.Join(root, sub, fmt.Sprintf("doc-%04d.md", i))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			b.Fatal(err)
		}
		md := fmt.Sprintf("---\ntitle: 文档 %d\nsummary: 摘要 %d。\ntags: [t%d]\n---\n\n## 小节\n\n%s", i, i, i%7, body)
		if err := os.WriteFile(p, []byte(md), 0o644); err != nil {
			b.Fatal(err)
		}
	}

	storePath := filepath.Join(b.TempDir(), "index.db")
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}

	rebuild := func(b *testing.B, st *Store) {
		b.Helper()
		m := NewManager(cfg, BuildOptions{Store: st})
		start := time.Now()
		if err := m.Rebuild(); err != nil {
			b.Fatal(err)
		}
		b.ReportMetric(float64(time.Since(start).Milliseconds()), "ms/rebuild")
	}

	b.Run("cold", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			os.RemoveAll(storePath)
			st, err := OpenStore(storePath)
			if err != nil {
				b.Skipf("这个构建没有 SQLite 驱动：%v", err)
			}
			rebuild(b, st)
			st.Close()
		}
	})
	b.Run("warm", func(b *testing.B) {
		st, err := OpenStore(storePath)
		if err != nil {
			b.Skipf("这个构建没有 SQLite 驱动：%v", err)
		}
		defer st.Close()
		rebuild(b, st) // 先把库里填满
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			rebuild(b, st)
		}
	})
}

// assertSameIndex 比对两个索引快照的「对外可见」内容。
func assertSameIndex(t *testing.T, want, got *KBIndex) {
	t.Helper()
	if len(want.Entries) != len(got.Entries) {
		t.Fatalf("条目数 = %d，想要 %d", len(got.Entries), len(want.Entries))
	}
	if want.Sections != got.Sections {
		t.Errorf("节数 = %d，想要 %d", got.Sections, want.Sections)
	}
	for i := range want.Entries {
		a, b := want.Entries[i], got.Entries[i]
		if a.RelPath != b.RelPath || a.Title != b.Title || a.Summary != b.Summary ||
			a.Rev != b.Rev || a.Chars != b.Chars || a.SearchFull != b.SearchFull ||
			a.SearchMeta != b.SearchMeta || a.TocText != b.TocText {
			t.Errorf("%s 不一致：\n旧 %+v\n新 %+v", a.RelPath, a, b)
			continue
		}
		if len(a.Sections) != len(b.Sections) {
			t.Errorf("%s 节数 = %d，想要 %d", a.RelPath, len(b.Sections), len(a.Sections))
			continue
		}
		for k := range a.Sections {
			sa, sb := a.Sections[k], b.Sections[k]
			if sa.No != sb.No || sa.Level != sb.Level || sa.Title != sb.Title ||
				sa.Content != sb.Content || sa.Chars != sb.Chars || sa.PathText != sb.PathText {
				t.Errorf("%s 第 %d 节不一致：\n旧 %+v\n新 %+v", a.RelPath, k, sa, sb)
			}
		}
	}
	if len(want.Skipped) != len(got.Skipped) {
		t.Errorf("跳过数 = %d，想要 %d", len(got.Skipped), len(want.Skipped))
	}
}

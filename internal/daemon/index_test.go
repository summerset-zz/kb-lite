package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRebuildIndex(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "b.md"),
		"---\ntitle: 乙\nsummary: 第二个。\n---\n\n## 节一\n\n正文\n")
	writeFile(t, filepath.Join(root, "a.txt"), "纯文本，没有标题。\n")
	writeFile(t, filepath.Join(root, "draft.md"), "---\ndraft: true\n---\n\n不收录\n")

	m := NewManager(&Config{KBs: []KB{
		{ID: "k1", Name: "笔记", RootPath: root, Enabled: true},
	}}, BuildOptions{Workers: 2})
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}

	k, ok := m.KB("笔记")
	if !ok {
		t.Fatal("KB 不存在")
	}
	if len(k.Entries) != 2 {
		t.Fatalf("条目 = %d，想要 2（draft 应当被跳过）：%v", len(k.Entries), k.Entries)
	}
	// 顺序必须与 Scan 一致（RelPath 升序），否则 search 的同分排序会飘。
	if k.Entries[0].RelPath != "a.txt" || k.Entries[1].RelPath != "b.md" {
		t.Errorf("顺序 = %s, %s", k.Entries[0].RelPath, k.Entries[1].RelPath)
	}
	// b.md：只有导言之后那一个标题，导言为空 → 1 节（没有 section 0）。
	if n := len(k.Entries[1].Sections); n != 1 {
		t.Errorf("b.md 的节数 = %d，想要 1", n)
	}
	// 总计 = a.txt 的导言节（1）+ b.md 的「节一」（1）。
	if k.Sections != 2 {
		t.Errorf("总节数 = %d，想要 2", k.Sections)
	}
	if k.Bytes <= 0 {
		t.Errorf("Bytes = %d", k.Bytes)
	}

	if _, ok := k.Entry("b.md"); !ok {
		t.Error("ByPath 里找不到 b.md")
	}
	if _, ok := k.Entry("draft.md"); ok {
		t.Error("draft 不该进索引")
	}

	states := m.States()
	if len(states) != 1 || states[0].State != "ready" || states[0].Done != 2 {
		t.Errorf("States = %+v", states)
	}
}

func TestRebuildSkipsDisabled(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "正文\n")

	m := NewManager(&Config{KBs: []KB{
		{ID: "k1", Name: "关掉的", RootPath: root, Enabled: false},
	}}, BuildOptions{})
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}
	if _, ok := m.KB("关掉的"); ok {
		t.Error("禁用的 KB 不该出现在索引里")
	}
	if s := m.States()[0]; s.State != "disabled" {
		t.Errorf("state = %q，想要 disabled", s.State)
	}
}

func TestRebuildOneBadKBBreaksNothingElse(t *testing.T) {
	good := t.TempDir()
	writeFile(t, filepath.Join(good, "a.md"), "正文\n")

	m := NewManager(&Config{KBs: []KB{
		{ID: "k1", Name: "坏的", RootPath: filepath.Join(t.TempDir(), "不存在"), Enabled: true},
		{ID: "k2", Name: "好的", RootPath: good, Enabled: true},
	}}, BuildOptions{})
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}

	if _, ok := m.KB("坏的"); ok {
		t.Error("根目录不存在的 KB 不该进索引")
	}
	if _, ok := m.KB("好的"); !ok {
		t.Error("一个 KB 出错不该影响别的 KB")
	}

	states := m.States()
	if states[0].State != "error" || states[0].Error == "" {
		t.Errorf("坏的 KB 状态 = %+v", states[0])
	}
	if states[1].State != "ready" {
		t.Errorf("好的 KB 状态 = %+v", states[1])
	}
}

func TestRebuildRecordsWarnings(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "bad.md"), "---\ntitle: [未闭合\n---\n\n正文\n")

	m := NewManager(&Config{KBs: []KB{
		{ID: "k1", Name: "笔记", RootPath: root, Enabled: true},
	}}, BuildOptions{})
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}

	k, _ := m.KB("笔记")
	if len(k.Warnings) == 0 {
		t.Fatal("frontmatter 解析失败应当记一条 warn")
	}
	if !strings.Contains(k.Warnings[0], "bad.md") {
		t.Errorf("warn 没带文件名：%v", k.Warnings)
	}
}

func TestIndexSnapshotIsReplacedNotMutated(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "第一版\n")

	m := NewManager(&Config{KBs: []KB{
		{ID: "k1", Name: "笔记", RootPath: root, Enabled: true},
	}}, BuildOptions{Workers: 1})
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}
	first := m.Index()

	writeFile(t, filepath.Join(root, "b.md"), "第二篇\n")
	if err := m.Rebuild(); err != nil {
		t.Fatal(err)
	}
	second := m.Index()

	if first == second {
		t.Fatal("重建应当换一个新快照，而不是原地改")
	}
	// 拿着旧快照的读者看到的仍然是自洽的旧数据。
	if len(first.ByName["笔记"].Entries) != 1 {
		t.Errorf("旧快照被改了：%d 条", len(first.ByName["笔记"].Entries))
	}
	if len(second.ByName["笔记"].Entries) != 2 {
		t.Errorf("新快照 = %d 条，想要 2", len(second.ByName["笔记"].Entries))
	}
}

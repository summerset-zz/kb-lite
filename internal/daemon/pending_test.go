package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestPendingCount 守「界面能看出目录里有多少篇还没进索引」。
//
// 这件事以前没做：用户让 AI 往目录里写了 86 篇，界面还写着“已就绪、0 篇文档”。
func TestPendingCount(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\n---\n\n## 一\n\n内容\n")
	writeFile(t, filepath.Join(root, "b.md"), "---\ntitle: 乙\n---\n\n## 二\n\n内容\n")

	store := openTestStore(t)
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	m := NewManager(cfg, BuildOptions{Workers: 1, Store: store})
	buildWith(t, m)

	kb := cfg.KBs[0]
	if n := m.countPending(kb); n != 0 {
		t.Fatalf("刚整理完，待更新应当是 0，实际 %d", n)
	}

	// 新增一篇。
	writeFile(t, filepath.Join(root, "c.md"), "---\ntitle: 丙\n---\n\n## 三\n\n内容\n")
	if n := m.countPending(kb); n != 1 {
		t.Errorf("加了一篇之后待更新 = %d，想要 1", n)
	}

	// 改内容（大小变了）。
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\n---\n\n## 一\n\n内容改了，长一点。\n")
	if n := m.countPending(kb); n != 2 {
		t.Errorf("改了一篇之后待更新 = %d，想要 2", n)
	}

	// 删一篇：新增 1（c）+ 改动 1（a）+ 删除 1（b）= 3。
	if err := os.Remove(filepath.Join(root, "b.md")); err != nil {
		t.Fatal(err)
	}
	if n := m.countPending(kb); n != 3 {
		t.Errorf("删了一篇之后待更新 = %d，想要 3", n)
	}

	// 重新整理之后清零。
	buildWith(t, m)
	if n := m.countPending(kb); n != 0 {
		t.Errorf("重新整理之后待更新 = %d，想要 0", n)
	}
}

// 草稿这类“已知不收录”的文件不该每次都算成新增，否则界面会永远显示待更新。
func TestPendingIgnoresSkippedFiles(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\n---\n\n## 一\n\n内容\n")
	writeFile(t, filepath.Join(root, "草稿.md"), "---\ntitle: 草稿\ndraft: true\n---\n\n正文。\n")

	store := openTestStore(t)
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	m := NewManager(cfg, BuildOptions{Workers: 1, Store: store})
	buildWith(t, m)

	if n := m.countPending(cfg.KBs[0]); n != 0 {
		t.Errorf("草稿是“已知跳过”，不该算待更新，实际 %d", n)
	}
}

// 禁用与纯内存模式都返回 0：前者不该催用户更新，后者每次启动都重切、没有这回事。
func TestPendingZeroWhenNotComparable(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "内容")

	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: false}}}
	if n := NewManager(cfg, BuildOptions{Workers: 1}).countPending(cfg.KBs[0]); n != 0 {
		t.Errorf("禁用的库不该算待更新，实际 %d", n)
	}

	cfg2 := &Config{KBs: []KB{{ID: "k2", Name: "笔记", RootPath: root, Enabled: true}}}
	if n := NewManager(cfg2, BuildOptions{Workers: 1}).countPending(cfg2.KBs[0]); n != 0 {
		t.Errorf("没有 Store 时不该算，实际 %d", n)
	}
}

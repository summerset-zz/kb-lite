package daemon

import (
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

// tableCount 数一下某个知识库在缓存里还剩多少行。
func tableCount(t *testing.T, store *Store, table, kbID string) int {
	t.Helper()
	var n int
	q := fmt.Sprintf("SELECT COUNT(*) FROM %s WHERE kb_id = ?", table)
	if err := store.db.QueryRow(q, kbID).Scan(&n); err != nil {
		t.Fatalf("查 %s 失败：%v", table, err)
	}
	return n
}

// TestKBDeleteClearsCache 盯「删掉知识库之后，缓存里不留任何它的行」。
//
// 这条以前没人验过：`TestKBDeleteDoesNotTouchFiles` 用的是没有 Store 的 manager，
// 所以 `ClearKB` 根本没被走到。它只证明了“用户的文件没被动”，
// 而“我们自己的行清干净了没有”是另一件事。
func TestKBDeleteClearsCache(t *testing.T) {
	store := openTestStore(t)
	mgr := NewManager(&Config{}, BuildOptions{Workers: 1, ConfigDir: t.TempDir(), Store: store})
	srv := newServerFor(t, mgr, testServerOpts{features: DefaultFeatures, requireToken: true})
	t.Cleanup(srv.Close)

	// 要删的这个库里故意放一张草稿：这样 skipped 表也会有行，
	// 四张表一起验，别只验 entry。
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\n---\n\n## 一\n\n内容\n")
	writeFile(t, filepath.Join(root, "草稿.md"), "---\ntitle: 草稿\ndraft: true\n---\n\n正文。\n")

	// 另一个库要留着，用来证明「只删该删的」。
	keepRoot := t.TempDir()
	writeFile(t, filepath.Join(keepRoot, "b.md"), "---\ntitle: 乙\n---\n\n## 二\n\n内容\n")

	for _, req := range []kbWriteReq{
		{Name: "要删的", RootPath: root},
		{Name: "留着的", RootPath: keepRoot},
	} {
		if status, _, raw := adminReq(t, srv, http.MethodPost, "/api/kbs", req); status != http.StatusCreated {
			t.Fatalf("建库 %s 失败（%d）：%s", req.Name, status, raw)
		}
		waitIndexed(t, srv, req.Name)
	}

	_, body, raw := adminReq(t, srv, http.MethodGet, "/api/kbs", nil)
	if body["kbs"] == nil {
		t.Fatalf("取列表失败：%s", raw)
	}
	ids := map[string]string{}
	for _, item := range body["kbs"].([]any) {
		m := item.(map[string]any)
		ids[m["name"].(string)] = m["id"].(string)
	}
	gone, kept := ids["要删的"], ids["留着的"]
	if gone == "" || kept == "" {
		t.Fatalf("拿不到 id：%v", ids)
	}

	// 删之前：四张表都该有行，否则这个测试等于没验到东西。
	for _, table := range []string{"entry", "section", "warn", "skipped"} {
		if n := tableCount(t, store, table, gone); n == 0 && table != "warn" {
			t.Fatalf("删之前 %s 里没有行，这个测试没验到东西", table)
		}
	}

	if status, _, raw := adminReq(t, srv, http.MethodDelete, "/api/kbs/"+url.PathEscape("要删的"), nil); status != http.StatusOK {
		t.Fatalf("删除失败（%d）：%s", status, raw)
	}

	// 删之后：该库的四张表必须是空的。
	for _, table := range []string{"entry", "section", "warn", "skipped"} {
		if n := tableCount(t, store, table, gone); n != 0 {
			t.Errorf("知识库都删了，%s 里还留着 %d 行", table, n)
		}
	}

	// 另一个库一行都不能少。
	if n := tableCount(t, store, "entry", kept); n != 1 {
		t.Errorf("删一个库把别的库也动了：留着的库里 entry 还有 %d 行，想要 1", n)
	}
	if n := tableCount(t, store, "section", kept); n == 0 {
		t.Error("删一个库把别的库的节也删了")
	}

	// 目录里的文件一个都不能动（ADR-004）。
	if _, err := os.Stat(filepath.Join(root, "a.md")); err != nil {
		t.Errorf("删知识库把用户的文件删了：%v", err)
	}
}

// TestCacheDropsEntryWhenBecomesSkipped 盯「本来是入库的，后来变成草稿」这条。
//
// 只按“路径还在不在”删行的话，这种文件会一直留在缓存里 —— 路径明明还在，
// 但它已经不该被搜到了。
func TestCacheDropsEntryWhenBecomesSkipped(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\n---\n\n## 一\n\n内容\n")

	store := openTestStore(t)
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	if n := tableCount(t, store, "entry", "k1"); n != 1 {
		t.Fatalf("第一次整理之后 entry = %d，想要 1", n)
	}

	// 同一路径，内容改成草稿。
	writeFile(t, filepath.Join(root, "a.md"), "---\ntitle: 甲\ndraft: true\n---\n\n内容\n")
	k := buildWith(t, NewManager(cfg, BuildOptions{Workers: 2, Store: store}))
	if _, ok := k.Entry("a.md"); ok {
		t.Error("草稿不该还在内存索引里")
	}
	if n := tableCount(t, store, "entry", "k1"); n != 0 {
		t.Errorf("变成草稿之后 entry 还有 %d 行，想要 0", n)
	}
	if n := tableCount(t, store, "section", "k1"); n != 0 {
		t.Errorf("变成草稿之后 section 还有 %d 行，想要 0", n)
	}
	if n := tableCount(t, store, "skipped", "k1"); n != 1 {
		t.Errorf("skipped = %d，想要 1（草稿要记进去，下次没变就不用再解析）", n)
	}
}

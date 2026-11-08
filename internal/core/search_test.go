package core

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

var updateRankSnapshot = flag.Bool("update-rank", false, "重新生成 testdata/rank/snapshot.json")

func TestTokenizeQuery(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{"Element.scrollTo", []string{"Element", "scrollTo"}},
		{"部署  回滚", []string{"部署", "回滚"}},
		{"a A", []string{"a"}}, // 大小写不敏感去重，保留首次写法
		{"one two three four five six", []string{"one", "two", "three", "four", "five"}},
		{"-- & ()", nil}, // 不含字母数字的碎片全丢
		{"Array.map()", []string{"Array", "map"}},
		{"（回滚）", []string{"回滚"}},
		{`"部署"`, []string{"部署"}},
		// 这些整块都是有信息的，**不能**被剥成 `C` / `C`：
		{"C++ C# a-b", []string{"C++", "C#", "a-b"}},
		{"snake_case", []string{"snake_case"}},
	}
	for _, c := range cases {
		got := TokenizeQuery(c.in)
		if len(got) != len(c.want) {
			t.Errorf("TokenizeQuery(%q) = %v，想要 %v", c.in, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("TokenizeQuery(%q) = %v，想要 %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestBigrams(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"知识库", []string{"知识", "识库"}},
		{"部署流程", []string{"部署", "署流", "流程"}},
		{"知识", nil},  // 长度 < 3
		{"abc", nil}, // 没有 CJK
		{"api知识库", []string{"知识", "识库"}}, // 只在连续 CJK 段内部取，不拼出 `i知`
		{"啊啊啊", []string{"啊啊"}},          // 去重
	}
	for _, c := range cases {
		got := bigramsOf(c.in)
		if len(got) != len(c.want) {
			t.Errorf("bigramsOf(%q) = %v，想要 %v", c.in, got, c.want)
			continue
		}
		for i := range c.want {
			if got[i] != c.want[i] {
				t.Errorf("bigramsOf(%q) = %v，想要 %v", c.in, got, c.want)
				break
			}
		}
	}
}

func TestIdentifierWordBoundary(t *testing.T) {
	typedArray := BuildEntry("dom/typedarray.md",
		"---\ntitle: TypedArray\nsummary: 类型化数组。\n---\n\n## 方法\n\n### map\n\n返回新数组。\n",
		SliceOptions{}).Entry

	// `Array` 不该命中标题 `TypedArray`（子串但不是词）。
	if d := Compile("Array").Score(&typedArray); d.Score != 0 {
		t.Errorf("搜 Array 命中了 TypedArray，分数 %d：%+v", d.Score, d.Hits)
	}
	// 反过来应当命中。
	if d := Compile("TypedArray").Score(&typedArray); d.Score == 0 {
		t.Error("搜 TypedArray 没命中它自己")
	}

	// `map` 不该命中标题 `flatMap`。
	flatMap := BuildEntry("dom/flatmap.md",
		"---\ntitle: flatMap\n---\n\n无。\n", SliceOptions{}).Entry
	if d := Compile("map").Score(&flatMap); d.Score != 0 {
		t.Errorf("搜 map 命中了 flatMap，分数 %d", d.Score)
	}

	// 中文没有词边界：「数组」应当命中「类型化数组」。
	if d := Compile("数组").Score(&typedArray); d.Score == 0 {
		t.Error("搜「数组」没命中「类型化数组」")
	}
}

func TestFullTextHitRanksBelowTitleHit(t *testing.T) {
	titleHit := BuildEntry("a.md", "---\ntitle: 部署手册\n---\n\n无关内容。\n", SliceOptions{}).Entry
	fullHit := BuildEntry("b.md", "---\ntitle: 别的文档\n---\n\n这里提了一次部署，但就一次。\n", SliceOptions{}).Entry

	hits := Rank([]*Entry{&fullHit, &titleHit}, []*Query{Compile("部署")}, RankOptions{Limit: 5})
	if len(hits) != 2 {
		t.Fatalf("命中 %d 条，想要 2", len(hits))
	}
	if hits[0].Entry.RelPath != "a.md" {
		t.Errorf("命中文档标题的应当排第一，实际 %s（分 %d vs %d）",
			hits[0].Entry.RelPath, hits[0].Score, hits[1].Score)
	}
}

func TestEmptyQueryReturnsNothing(t *testing.T) {
	e := BuildEntry("a.md", "任意内容。\n", SliceOptions{}).Entry
	if hits := Rank([]*Entry{&e}, []*Query{Compile("   ")}, RankOptions{}); len(hits) != 0 {
		t.Errorf("空查询不该有结果：%+v", hits)
	}
}

func TestMultiQuerySumsScores(t *testing.T) {
	entries := snapshotCorpus()
	a, b := Compile("部署"), Compile("回滚")

	collect := func(qs ...*Query) map[string]int {
		out := map[string]int{}
		for _, h := range Rank(entries, qs, RankOptions{Limit: 10}) {
			out[h.Entry.RelPath] = h.Score
		}
		return out
	}
	one, two, both := collect(a), collect(b), collect(a, b)

	for path, s1 := range one {
		want := s1
		if s2, ok := two[path]; ok {
			want += s2
		}
		if both[path] != want {
			t.Errorf("%s 合并分 = %d，想要 %d", path, both[path], want)
		}
	}
}

func TestExplainDetail(t *testing.T) {
	e := BuildEntry("guides/kb.md",
		"---\ntitle: 知识库使用指南\ntags: [检索]\n---\n\n这套知识库怎么用。\n\n## 检索\n\n先搜再读。\n",
		SliceOptions{}).Entry

	d := Compile("知识库").Score(&e)
	if d.Score == 0 {
		t.Fatal("应当命中")
	}
	if d.InTitle != 1 || d.Strong != 1 {
		t.Errorf("InTitle=%d Strong=%d，想要 1/1", d.InTitle, d.Strong)
	}

	// bigram 在 --explain 里带 `(≈)` 后缀，并且标了 Approx。
	var sawApprox bool
	for _, h := range d.Hits {
		if h.Approx {
			sawApprox = true
			if len(h.Keyword) < len("(≈)") || h.Keyword[len(h.Keyword)-len("(≈)"):] != "(≈)" {
				t.Errorf("bigram 的 keyword = %q，应当带 (≈) 后缀", h.Keyword)
			}
		}
	}
	if !sawApprox {
		t.Error("「知识库」应当生成 bigram 的 Hit 行")
	}
}

func TestRankDeterministicOnTies(t *testing.T) {
	// 同分时按 title 字典序 → relPath，保证输出可复现。
	a := BuildEntry("z/aa.md", "---\ntitle: 乙\n---\n\n共同词。\n", SliceOptions{}).Entry
	b := BuildEntry("a/bb.md", "---\ntitle: 甲\n---\n\n共同词。\n", SliceOptions{}).Entry
	c := BuildEntry("m/cc.md", "---\ntitle: 甲\n---\n\n共同词。\n", SliceOptions{}).Entry

	for i := 0; i < 5; i++ {
		hits := Rank([]*Entry{&a, &b, &c}, []*Query{Compile("共同词")}, RankOptions{Limit: 5})
		got := []string{hits[0].Entry.RelPath, hits[1].Entry.RelPath, hits[2].Entry.RelPath}
		// 同分后的第二级是 title 的**码点序**（不是 localeCompare 的字典序）：
		// `乙` = U+4E59 < `甲` = U+7532，所以 z/aa.md 排在最前。
		// 这是 ADR-013 里明确接受的代价；第三级是 relPath 升序。
		want := []string{"z/aa.md", "a/bb.md", "m/cc.md"}
		for k := range want {
			if got[k] != want[k] {
				t.Fatalf("第 %d 次排序 = %v，想要 %v", i, got, want)
			}
		}
	}
}

// ---- 排序快照 ----

type rankRecord struct {
	Query string   `json:"query"`
	Limit int      `json:"limit"`
	Paths []string `json:"paths"`
}

type rankSnapshot struct {
	Note    string       `json:"note"`
	Records []rankRecord `json:"records"`
}

func TestRankSnapshot(t *testing.T) {
	cases := []struct {
		q     string
		limit int
	}{
		{"Array", 5},
		{"map", 5},
		{"flatMap", 5},
		{"Element.scrollTo", 5},
		{"部署 回滚", 5},
		{"知识库", 5},
		{"README", 5},
		{"rollback", 5},
		{"回滚", 5},
		{"配置", 5},
		{"deploy", 5},
		{"服务器", 5},
		{"类型化数组", 5},
		{"上线", 5},
		{"tag", 5},
		{"端口", 3},
	}

	entries := snapshotCorpus()
	snap := rankSnapshot{
		Note: "排序快照。改切片或打分必然让这里变，diff 就是给人看的：先确认「变对了吗」再接受。" +
			"重新生成：go test ./internal/core -run TestRankSnapshot -update-rank",
	}
	for _, c := range cases {
		hits := Rank(entries, []*Query{Compile(c.q)}, RankOptions{Limit: c.limit})
		rec := rankRecord{Query: c.q, Limit: c.limit, Paths: make([]string, 0, len(hits))}
		for _, h := range hits {
			rec.Paths = append(rec.Paths, h.Entry.RelPath)
		}
		snap.Records = append(snap.Records, rec)
	}

	path := filepath.Join("testdata", "rank", "snapshot.json")
	if *updateRankSnapshot {
		writeRankSnapshot(t, path, snap)
		t.Logf("已重新生成 %s", path)
		return
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读快照失败（先跑一次 -update-rank）：%v", err)
	}
	var want rankSnapshot
	if err := json.Unmarshal(raw, &want); err != nil {
		t.Fatalf("快照 JSON 坏了：%v", err)
	}
	if len(want.Records) != len(snap.Records) {
		t.Fatalf("快照有 %d 条，这次生成了 %d 条", len(want.Records), len(snap.Records))
	}
	for i := range want.Records {
		w, g := want.Records[i], snap.Records[i]
		if w.Query != g.Query || w.Limit != g.Limit {
			t.Fatalf("第 %d 条对不上：快照 %q/%d，实际 %q/%d", i, w.Query, w.Limit, g.Query, g.Limit)
		}
		if len(w.Paths) != len(g.Paths) {
			t.Errorf("查询 %q：快照 %d 条，实际 %d 条\n  旧: %v\n  新: %v",
				g.Query, len(w.Paths), len(g.Paths), w.Paths, g.Paths)
			continue
		}
		for k := range w.Paths {
			if w.Paths[k] != g.Paths[k] {
				t.Errorf("查询 %q 的排序变了：\n  旧: %v\n  新: %v", g.Query, w.Paths, g.Paths)
				break
			}
		}
	}
}

func writeRankSnapshot(t *testing.T, path string, snap rankSnapshot) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}

// snapshotCorpus 是排序快照用的语料。
//
// 刻意包含：文件名与标题不一致、README 陷阱、同名小节（map）、
// 「只在正文里提过一次」的弱命中、中英混排。
func snapshotCorpus() []*Entry {
	docs := []struct{ rel, md string }{
		{"ops/runbook.md", `---
title: 运维手册
summary: 线上环境的操作手册。
tags: [ops, 上线]
---

## 上线流程

先打 tag，再跑 deploy 脚本。

## 回滚

### 快速回滚

回滚到上一个 tag，先确认监控正常。
`},
		{"ops/migrate.md", `---
title: 服务器迁移记录
summary: 一次机房搬迁的流水账。
---

这次迁移过程中部署了几次，也回滚过一次，但都是临时操作，没有正式流程。
`},
		{"dom/api.md", `---
title: Element.scrollTo
summary: 让元素滚动到指定位置。
tags: [dom]
---

## 语法

调用 scrollTo 并传入 x、y 两个坐标。

## 兼容性

Chrome 61 起支持。
`},
		{"dom/array.md", `---
title: Array
summary: JavaScript 的数组类型。
---

## 方法

### map

返回一个新数组。

### flatMap

先 map 再摊平。
`},
		{"dom/typedarray.md", `---
title: TypedArray
summary: 类型化数组。
---

## 方法

### map

和 Array 的 map 行为一致。
`},
		{"dom/map.md", `---
title: map
summary: 键值集合类型。
---

## 用法

用 new Map 创建。
`},
		{"guides/kb.md", `---
title: 知识库使用指南
summary: 怎么用这套知识库做检索。
tags: [知识库, 检索]
---

## 检索

先搜关键词，再按编号读某一节。
`},
		{"misc/README.md", `只有正文，没有标题。

这里顺带提了一次部署，但没有别的信息。
`},
		{"ops/config.md", `---
title: 配置管理
summary: 所有配置项的位置与含义。
---

## 端口

默认 7331。
`},
	}

	out := make([]*Entry, 0, len(docs))
	for _, d := range docs {
		r := BuildEntry(d.rel, d.md, SliceOptions{})
		e := r.Entry
		out = append(out, &e)
	}
	return out
}

package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/summerset-zz/kb-lite/internal/core"
	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// ---- search ----

type hitDTO struct {
	Keyword string `json:"keyword"`
	Title   bool   `json:"title"`
	Path    bool   `json:"path"`
	TOC     bool   `json:"toc"`
	Leaf    bool   `json:"leaf"`
	Body    bool   `json:"body"`
	Full    bool   `json:"full"`
	Approx  bool   `json:"approx"`
}

type detailDTO struct {
	Hits  []hitDTO `json:"hits"`
	Score int      `json:"score"`
}

type explainRowDTO struct {
	Path    string      `json:"path"`
	Details []detailDTO `json:"details"`
}

type searchItemDTO struct {
	Title   string   `json:"title"`
	Path    string   `json:"path"`
	Rev     string   `json:"rev"`
	Score   int      `json:"score"`
	Summary string   `json:"summary"`
	Tags    []string `json:"tags"`
	Chars   int      `json:"chars"`
	TOC     string   `json:"toc"`
}

type searchRespDTO struct {
	OK      bool            `json:"ok"`
	Query   []string        `json:"query"`
	Results []searchItemDTO `json:"results"`
	Debug   []explainRowDTO `json:"debug"`
}

func runSearch(args []string) int {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	limit := fs.Int("limit", 5, "")
	explain := fs.Bool("explain", false, "")
	asJSON := fs.Bool("json", false, "")
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	if len(rest) < 2 {
		// 别静默退 1：`kb-lite search GB18030`（忘了写库名）是最容易犯的错，
		// 什么都不打印的话用的人只会以为「搜不到」。
		fmt.Fprintln(os.Stderr, "用法：kb-lite search <库名> <关键词…> [--limit n] [--explain] [--json]")
		fmt.Fprintln(os.Stderr, "  库名用 kb-lite list 看；关键词可以给多个。")
		return exitError
	}
	name, queries := rest[0], rest[1:]

	q := url.Values{}
	for _, s := range queries {
		q.Add("q", s)
	}
	q.Set("limit", strconv.Itoa(*limit))
	if *explain {
		q.Set("explain", "1")
	}

	c, err := newClient()
	if err != nil {
		// 连 token 都没有（没跑过 serve）——直接走离线。
		return searchOffline(name, queries, *limit, *explain, *asJSON)
	}
	body, err := c.get(kbPath(name, "search"), q)
	if err != nil {
		var down daemonDownError
		if errors.As(err, &down) {
			return searchOffline(name, queries, *limit, *explain, *asJSON)
		}
		return fail(err)
	}
	if *asJSON {
		fmt.Println(string(body))
		return exitOK
	}

	var resp searchRespDTO
	if err := json.Unmarshal(body, &resp); err != nil {
		fmt.Fprintln(os.Stderr, "服务端返回的不是合法 JSON：", err)
		return exitError
	}
	printSearch(name, strings.Join(queries, " "), &resp, *explain)
	return exitOK
}

// searchOffline 是 docs/05 里「daemon 未运行时自动降级为离线只读」的实现。
//
// 进程内扫目录、切片、打分，结果与走服务端一致（同一个 core.Rank）。
// 代价是每次查询都重建一遍索引，所以会先提示一句。
func searchOffline(name string, queries []string, limit int, explain, asJSON bool) int {
	mgr, err := buildLocalIndex()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	kb, ok := mgr.KB(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "没有这个知识库：%s（用 kb-lite list 看看有哪些）\n", name)
		return exitNotFound
	}
	fmt.Fprintln(os.Stderr, "（daemon 没在跑，本次是离线索引：会慢一点，结果相同）")

	compiled := make([]*core.Query, 0, len(queries))
	for _, q := range queries {
		compiled = append(compiled, core.Compile(q))
	}
	limit = clampInt(limit, 1, 10)
	hits := core.Rank(kb.Entries, compiled, core.RankOptions{Limit: limit, WithDetails: explain})

	resp := searchRespDTO{OK: true, Query: queries}
	for _, h := range hits {
		e := h.Entry
		resp.Results = append(resp.Results, searchItemDTO{
			Title: e.Title, Path: e.RelPath, Rev: e.Rev, Score: h.Score,
			Summary: e.Summary, Tags: e.Tags, Chars: e.Chars,
			TOC: daemon.FormatTOC(e.TOC),
		})
	}
	if explain {
		for _, h := range hits {
			row := explainRowDTO{Path: h.Entry.RelPath}
			for _, d := range h.Details {
				row.Details = append(row.Details, detailDTO{Hits: hitDTOs(d.Hits), Score: d.Score})
			}
			resp.Debug = append(resp.Debug, row)
		}
	}

	if asJSON {
		b, _ := json.MarshalIndent(resp, "", "  ")
		fmt.Println(string(b))
		return exitOK
	}
	printSearch(name, strings.Join(queries, " "), &resp, explain)
	return exitOK
}

func hitDTOs(hits []core.Hit) []hitDTO {
	out := make([]hitDTO, 0, len(hits))
	for _, h := range hits {
		out = append(out, hitDTO{
			Keyword: h.Keyword, Title: h.Title, Path: h.Path, TOC: h.TOC,
			Leaf: h.Leaf, Body: h.Body, Full: h.Full, Approx: h.Approx,
		})
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// buildLocalIndex 在进程内建一遍索引（离线命令共用）。
func buildLocalIndex() (*daemon.Manager, error) {
	dir, err := daemon.ConfigDir()
	if err != nil {
		return nil, err
	}
	cfg, err := daemon.Load(dir)
	if err != nil {
		return nil, err
	}
	if len(cfg.KBs) == 0 {
		return nil, fmt.Errorf("还没有知识库。用 kb-lite add <目录> 添加一个。\n配置目录：%s", dir)
	}
	mgr := daemon.NewManager(cfg, daemon.BuildOptions{})
	if err := mgr.Rebuild(); err != nil {
		return nil, err
	}
	return mgr, nil
}

func printSearch(name, query string, resp *searchRespDTO, explain bool) {
	fmt.Printf("%s ← %q    关键词 = %q    返回 %d 条\n",
		name, query, resp.Query, len(resp.Results))
	if len(resp.Results) == 0 {
		fmt.Println("  （没有命中：换词再试，这个服务不做模糊匹配）")
		return
	}

	width := 0
	for _, r := range resp.Results {
		if w := displayWidth(r.Title); w > width {
			width = w
		}
	}

	byPath := map[string]*explainRowDTO{}
	for i := range resp.Debug {
		byPath[resp.Debug[i].Path] = &resp.Debug[i]
	}

	for i, r := range resp.Results {
		fmt.Printf("%3d. %3d  %s  %s\n", i+1, r.Score, padRight(r.Title, width), r.Path)
		if !explain {
			continue
		}
		if row, ok := byPath[r.Path]; ok {
			fmt.Printf("        %s\n", explainLine(row.Details))
		}
	}
}

// explainLine 把命中明细渲染成 `部署: 标题 路径 章节   回滚: 路径 全文`。
//
// 位置名固定为 docs/03 那六个：标题 / 条目标题 / 路径 / 章节 / 摘要 / 全文。
func explainLine(details []detailDTO) string {
	segs := make([]string, 0, len(details))
	for _, d := range details {
		parts := make([]string, 0, len(d.Hits))
		for _, h := range d.Hits {
			parts = append(parts, h.Keyword+": "+positions(h))
		}
		if len(parts) > 0 {
			segs = append(segs, strings.Join(parts, "    "))
		}
	}
	return strings.Join(segs, "  ‖  ")
}

func positions(h hitDTO) string {
	var parts []string
	if h.Title {
		parts = append(parts, "标题")
	}
	if h.Leaf && !h.Title {
		parts = append(parts, "条目标题")
	}
	if h.Path {
		parts = append(parts, "路径")
	}
	if h.TOC {
		parts = append(parts, "章节")
	}
	if h.Body {
		parts = append(parts, "摘要")
	}
	if h.Full {
		parts = append(parts, "全文")
	}
	if len(parts) == 0 {
		return "（无）"
	}
	return strings.Join(parts, " ")
}

// ---- toc ----

func runTOC(args []string) int {
	fs := flag.NewFlagSet("toc", flag.ContinueOnError)
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "用法：kb-lite toc <name> <path>")
		return exitError
	}

	if c, err := newClient(); err == nil {
		body, err := c.get(kbPath(rest[0], "entry/"+escapeRelPath(rest[1])+"/toc"), nil)
		if err == nil {
			var resp struct {
				TOC      string `json:"toc"`
				Sections int    `json:"sections"`
				Rev      string `json:"rev"`
			}
			if err := json.Unmarshal(body, &resp); err != nil {
				fmt.Fprintln(os.Stderr, "服务端返回的不是合法 JSON：", err)
				return exitError
			}
			fmt.Printf("%s  （%d 节，rev %s）\n\n%s\n", rest[1], resp.Sections, resp.Rev, resp.TOC)
			return exitOK
		}
		var down daemonDownError
		if !errors.As(err, &down) {
			return fail(err)
		}
	}

	mgr, err := buildLocalIndex()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	kb, ok := mgr.KB(rest[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "没有这个知识库：%s\n", rest[0])
		return exitNotFound
	}
	e, ok := kb.Entry(rest[1])
	if !ok {
		fmt.Fprintf(os.Stderr, "没有这个条目：%s\n", rest[1])
		return exitNotFound
	}
	fmt.Printf("%s  （%d 节，rev %s）\n\n%s\n", rest[1], len(e.Sections), e.Rev, daemon.FormatTOC(e.TOC))
	return exitOK
}

// ---- read ----

type candidateDTO struct {
	Section      int    `json:"section"`
	SectionTitle string `json:"sectionTitle"`
	SectionPath  string `json:"sectionPath"`
	Chars        int    `json:"chars"`
}

type sectionRespDTO struct {
	OK           bool           `json:"ok"`
	Error        string         `json:"error"`
	Candidates   []candidateDTO `json:"candidates"`
	SectionCount int            `json:"sectionCount"`
	Title        string         `json:"title"`
	Section      int            `json:"section"`
	Level        int            `json:"level"`
	SectionTitle string         `json:"sectionTitle"`
	PathText     string         `json:"pathText"`
	Chars        int            `json:"chars"`
	Content      string         `json:"content"`
	Note         string         `json:"note"`
}

func runRead(args []string) int {
	fs := flag.NewFlagSet("read", flag.ContinueOnError)
	section := fs.String("section", "", "")
	title := fs.String("title", "", "")
	offset := fs.Int("offset", -1, "")
	maxChars := fs.Int("max-chars", 0, "")
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "用法：kb-lite read <name> <path> [--section n | --title t]")
		return exitError
	}

	q := url.Values{}
	if *section != "" {
		q.Set("n", *section)
	} else if *title != "" {
		q.Set("title", *title)
	}
	if *offset >= 0 {
		q.Set("offset", strconv.Itoa(*offset))
	}
	if *maxChars > 0 {
		q.Set("maxChars", strconv.Itoa(*maxChars))
	}

	if c, err := newClient(); err == nil {
		body, err := c.get(kbPath(rest[0], "entry/"+escapeRelPath(rest[1])+"/section"), q)
		if err == nil {
			var resp sectionRespDTO
			if err := json.Unmarshal(body, &resp); err != nil {
				fmt.Fprintln(os.Stderr, "服务端返回的不是合法 JSON：", err)
				return exitError
			}
			return printSectionResp(&resp)
		}
		var down daemonDownError
		if !errors.As(err, &down) {
			return fail(err)
		}
	}
	return readOffline(rest[0], rest[1], q)
}

func printSectionResp(resp *sectionRespDTO) int {
	if !resp.OK {
		printMiss(resp)
		return exitNotFound
	}
	fmt.Printf("《%s》第 %d 节「%s」  （%s）  %d 字\n\n%s\n",
		resp.Title, resp.Section, resp.SectionTitle, resp.PathText, resp.Chars, resp.Content)
	if resp.Note != "" {
		fmt.Printf("\n[note] %s\n", resp.Note)
	}
	return exitOK
}

// readOffline 与 HTTP handler 共用 daemon.AnswerSection：
// 空父节换子节、超长截断、rev 漂移这三件事只写一份。
func readOffline(name, relPath string, q url.Values) int {
	mgr, err := buildLocalIndex()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	kb, ok := mgr.KB(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "没有这个知识库：%s\n", name)
		return exitNotFound
	}
	e, ok := kb.Entry(relPath)
	if !ok {
		fmt.Fprintf(os.Stderr, "没有这个条目：%s\n", relPath)
		return exitNotFound
	}

	query, badParam := daemon.ParseSectionQuery(q.Get)
	if badParam != "" {
		fmt.Fprintln(os.Stderr, badParam)
		return exitError
	}
	answer := daemon.AnswerSection(e, query)
	if !answer.OK {
		printMiss(&sectionRespDTO{
			Error: answer.Message, Candidates: candidatesOf(e), SectionCount: len(e.Sections),
		})
		return exitNotFound
	}

	return printSectionResp(&sectionRespDTO{
		OK: true, Title: e.Title, Section: answer.Section.No, Level: answer.Section.Level,
		SectionTitle: answer.Section.Title, PathText: answer.Section.PathText,
		Chars: answer.Section.Chars, Content: answer.Content, Note: answer.Note,
	})
}

func candidatesOf(e *core.Entry) []candidateDTO {
	cs := daemon.CandidatesOf(e.Sections)
	out := make([]candidateDTO, 0, len(cs))
	for _, c := range cs {
		out = append(out, candidateDTO{Section: c.Section, SectionTitle: c.SectionTitle, SectionPath: c.SectionPath, Chars: c.Chars})
	}
	return out
}

// printMiss 渲染 200+ok:false 的候选清单（ADR-010）。
func printMiss(resp *sectionRespDTO) {
	fmt.Fprintln(os.Stderr, resp.Error)
	if len(resp.Candidates) == 0 {
		return
	}
	fmt.Fprintln(os.Stderr)
	width := 0
	for _, c := range resp.Candidates {
		if w := displayWidth(c.SectionPath); w > width {
			width = w
		}
	}
	for _, c := range resp.Candidates {
		chars := strconv.Itoa(c.Chars) + " 字"
		if c.Chars == 0 {
			chars = "空"
		}
		fmt.Fprintf(os.Stderr, "  n=%-3d %s  %s\n", c.Section, padRight(c.SectionPath, width), chars)
	}
}

// ---- skill ----

func runSkill(args []string) int {
	fs := flag.NewFlagSet("skill", flag.ContinueOnError)
	base := fs.String("url", "", "覆盖服务地址")
	outPath := fs.String("out", "", "写到文件（UTF-8、无 BOM），而不是打到 stdout")
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	text, code := fetchSkillText(*base)
	if code != exitOK {
		return code
	}

	if *outPath != "" {
		// 自己写文件，而不是让用户 `> SKILL.md`：PowerShell 的 `>` 会写成 UTF-16，
		// `Out-File -Encoding utf8` 又会带 BOM —— 两者都会让 YAML frontmatter 失效。
		if err := os.MkdirAll(filepath.Dir(*outPath), 0o755); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitError
		}
		if err := os.WriteFile(*outPath, []byte(text), 0o644); err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitError
		}
		fmt.Fprintf(os.Stderr, "已写入 %s（%d 字节）\n", *outPath, len(text))
		return exitOK
	}

	fmt.Print(text)
	return exitOK
}

// fetchSkillText 优先问 daemon 要，连不上就自己渲染（两边共用 daemon.RenderSkillText）。
func fetchSkillText(base string) (string, int) {
	if c, err := newClient(); err == nil {
		if base != "" {
			c.base = strings.TrimRight(base, "/")
		}
		body, err := c.get("/skill.md", nil)
		if err == nil {
			return string(body), exitOK
		}
		var down daemonDownError
		if !errors.As(err, &down) {
			return "", fail(err)
		}
	}

	dir, err := daemon.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return "", exitError
	}
	cfg, err := daemon.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return "", exitError
	}
	mgr, err := buildLocalIndex()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return "", exitError
	}

	// 离线时**要保证 token 存在**：文档里没有 token 就是一份不能用的说明。
	// 这里建的和 serve 之后用的是同一个文件，不冲突。
	token, err := daemon.LoadOrCreateToken(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return "", exitError
	}

	serviceBase := "http://127.0.0.1:" + strconv.Itoa(cfg.Port)
	if base != "" {
		serviceBase = strings.TrimRight(base, "/")
	}
	return daemon.RenderSkillText(mgr.Index(), serviceBase, token, nil), exitOK
}

// ---- token ----

func runToken(args []string) int {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	show := fs.Bool("show", false, "")
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	dir, err := daemon.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	tok := readToken(dir)
	if tok == "" {
		fmt.Fprintln(os.Stderr, "还没有 token —— 先运行一次 kb-lite serve，它会生成")
		return exitError
	}
	if *show {
		fmt.Println(tok)
		return exitOK
	}
	if len(tok) > 6 {
		tok = tok[:6] + "…（用 --show 看完整的）"
	}
	fmt.Println(tok)
	return exitOK
}

// ---- add / list（纯配置操作）----

// runAdd 只改 config.json，不碰索引。
//
// docs/01 说 CLI 不要直接读写索引（会和 daemon 抢锁、逻辑实现两遍），但 config.json 是例外：
// v0.1 还没有管理接口（那是 v0.3），不然用户连「把目录加进来」都做不到。
// daemon 只在启动时读配置，所以改完要重启 —— 这一点会明确打印出来。
func runAdd(args []string) int {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	name := fs.String("name", "", "")
	var ignore, include stringsFlag
	fs.Var(&ignore, "ignore", "额外忽略的 glob，可重复")
	fs.Var(&include, "include", "覆盖默认收录规则，可重复")
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	if len(rest) < 1 {
		fmt.Fprintln(os.Stderr, "用法：kb-lite add <dir> [--name <n>] [--ignore <glob>]... [--include <glob>]...")
		return exitError
	}

	root, err := filepath.Abs(rest[0])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		fmt.Fprintf(os.Stderr, "不是一个目录：%s\n", root)
		return exitError
	}

	dir, err := daemon.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	cfg, err := daemon.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}

	kbName := *name
	if kbName == "" {
		kbName = filepath.Base(root)
	}
	for _, kb := range cfg.KBs {
		if kb.Name == kbName {
			fmt.Fprintf(os.Stderr, "已经有同名知识库了：%s（用 --name 换个名字）\n", kbName)
			return exitError
		}
	}

	id, err := daemon.NewKBID()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	cfg.KBs = append(cfg.KBs, daemon.KB{
		ID: id, Name: kbName, RootPath: root, Enabled: true,
		Ignore: ignore, Include: include,
	})
	if err := cfg.Save(dir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}

	scanned, scanErr := daemon.Scan(daemon.ScanOptions{
		Root: root, Include: include, Ignore: ignore,
	})
	if scanErr != nil {
		fmt.Fprintf(os.Stderr, "已写入配置，但扫描失败：%v\n", scanErr)
		return exitError
	}

	fmt.Printf("已添加「%s」→ %s（扫到 %d 个文件）\n", kbName, root, len(scanned.Files))
	if len(scanned.Warnings) > 0 {
		fmt.Printf("  其中 %d 条告警（尺寸超限 / 编码失败之类），启动后可在日志里看到\n", len(scanned.Warnings))
	}
	if daemonRunning(cfg.Port) {
		fmt.Println("⚠ kb-lite 正在运行：重启 serve 之后才能看到这个知识库")
	} else {
		fmt.Println("下一步：kb-lite serve")
	}
	return exitOK
}

func runList(args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	dir, err := daemon.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	cfg, err := daemon.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	if len(cfg.KBs) == 0 {
		fmt.Println("还没有知识库。用 kb-lite add <目录> 添加一个。")
		return exitOK
	}

	width := 0
	for _, kb := range cfg.KBs {
		if w := displayWidth(kb.Name); w > width {
			width = w
		}
	}
	for _, kb := range cfg.KBs {
		state := "已开启"
		if !kb.Enabled {
			state = "已关闭"
		}
		fmt.Printf("%s  %s  %s\n", padRight(kb.Name, width), state, kb.RootPath)
	}
	return exitOK
}

// ---- status（离线：进程内建一遍索引，不需要 daemon）----

type statusEntryDTO struct {
	Path     string   `json:"path"`
	Title    string   `json:"title"`
	Summary  string   `json:"summary"`
	Tags     []string `json:"tags"`
	Sections int      `json:"sections"`
	Chars    int      `json:"chars"`
}

// skippedDTO 是被整篇跳过的条目。它有独立的结构，因为「跳过」与「告警」是两回事：
// 跳过通常是用户自己的意图（草稿、纯元数据卡片），告警则是意外。
type skippedDTO struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// warningDTO 带类别：调用方要能一眼看出「这些告警影不影响索引可用」。
// 文件过大 / 编码失败 = 那篇没进去；frontmatter 不合法 = 正文已经救回来了。
type warningDTO struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
}

// printWarnings 按类别汇总，再列出前几条明细。
func printWarnings(warnings []warningDTO, indent string) {
	if len(warnings) == 0 {
		return
	}
	counts := map[string]int{}
	for _, w := range warnings {
		counts[w.Kind]++
	}
	parts := make([]string, 0, len(counts))
	for kind, n := range counts {
		parts = append(parts, fmt.Sprintf("%s %d", daemon.WarningLabel(kind), n))
	}
	sort.Strings(parts)
	fmt.Printf("%s⚠ %d 条告警：%s\n", indent, len(warnings), strings.Join(parts, " / "))
	for i, w := range warnings {
		if i >= 3 {
			fmt.Printf("%s    …\n", indent)
			break
		}
		fmt.Printf("%s    %s\n", indent, w.Message)
	}
}

type statusKBDTO struct {
	Name     string           `json:"name"`
	ID       string           `json:"id"`
	RootPath string           `json:"rootPath"`
	State    string           `json:"state"`
	Error    string           `json:"error,omitempty"`
	Entries  int              `json:"entries"`
	Sections int              `json:"sections"`
	Bytes    int64            `json:"bytes"`
	Warnings []warningDTO     `json:"warnings,omitempty"`
	Skipped  []skippedDTO     `json:"skipped,omitempty"`
	Items    []statusEntryDTO `json:"items,omitempty"`
}

// runStatus 建一遍索引并打印统计。
//
// 它是 docs/05 里「daemon 未运行时自动降级为离线只读」的形态：进程内扫目录、切片、报数，
// 不起服务。v0.1 没有 /api/status（那是 v0.3），所以想看告警（超大文件、编码失败）
// 目前只有这条路径和 serve 的启动输出。
func runStatus(args []string) int {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "")
	showEntries := fs.Int("entries", 0, "每个知识库打印前 N 个条目")
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	dir, err := daemon.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	cfg, err := daemon.Load(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	if len(cfg.KBs) == 0 {
		fmt.Fprintf(os.Stderr, "还没有知识库。用 kb-lite add <目录> 添加一个。\n配置目录：%s\n", dir)
		return exitOK
	}

	mgr := daemon.NewManager(cfg, daemon.BuildOptions{})
	if err := mgr.Rebuild(); err != nil {
		fmt.Fprintln(os.Stderr, "建索引失败：", err)
		return exitError
	}

	var out []statusKBDTO
	for _, st := range mgr.States() {
		dto := statusKBDTO{Name: st.Name, ID: st.ID, State: st.State, Error: st.Error}
		if kb, ok := mgr.KB(st.Name); ok {
			dto.RootPath = kb.KB.RootPath
			dto.Entries = len(kb.Entries)
			dto.Sections = kb.Sections
			dto.Bytes = kb.Bytes
			dto.Warnings = nil
			for _, w := range kb.Warnings {
				dto.Warnings = append(dto.Warnings, warningDTO{Kind: w.Kind, Message: w.Message})
			}
			for _, s := range kb.Skipped {
				dto.Skipped = append(dto.Skipped, skippedDTO{Path: s.RelPath, Reason: s.Reason})
			}
			if *showEntries > 0 {
				for i, e := range kb.Entries {
					if i >= *showEntries {
						break
					}
					dto.Items = append(dto.Items, statusEntryDTO{
						Path: e.RelPath, Title: e.Title, Summary: e.Summary, Tags: e.Tags,
						Sections: len(e.Sections), Chars: e.Chars,
					})
				}
			}
		} else {
			for _, kb := range cfg.KBs {
				if kb.Name == st.Name {
					dto.RootPath = kb.RootPath
				}
			}
		}
		out = append(out, dto)
	}

	if *asJSON {
		b, _ := json.MarshalIndent(out, "", "  ")
		fmt.Println(string(b))
		return exitOK
	}

	fmt.Printf("配置目录 %s\n\n", dir)
	for _, kb := range out {
		switch kb.State {
		case "ready":
			fmt.Printf("%s  %d 篇 / %d 节 / %s\n", padRight(kb.Name, 12), kb.Entries, kb.Sections, formatBytes(kb.Bytes))
		case "error":
			fmt.Printf("%s  索引失败：%s\n", padRight(kb.Name, 12), kb.Error)
		default:
			fmt.Printf("%s  %s\n", padRight(kb.Name, 12), kb.State)
		}
		fmt.Printf("  %s\n", kb.RootPath)
		printWarnings(kb.Warnings, "  ")
		if len(kb.Skipped) > 0 {
			fmt.Printf("  跳过 %d 篇：%s\n", len(kb.Skipped), skipSummary(kb.Skipped))
			for i, s := range kb.Skipped {
				if i >= 3 {
					fmt.Printf("      …\n")
					break
				}
				fmt.Printf("      %s（%s）\n", s.Path, reasonText(s.Reason))
			}
		}
		for _, it := range kb.Items {
			title := it.Title
			if title == "" {
				title = "（无标题）"
			}
			fmt.Printf("    %s %s %3d 节 %7d 字\n",
				padRight(it.Path, 44), padRight(title, 24), it.Sections, it.Chars)
		}
		fmt.Println()
	}
	return exitOK
}

// skipSummary 按原因汇总被跳过的文件，例如「正文为空 14 / frontmatter 要求跳过 2」。
func skipSummary(files []skippedDTO) string {
	counts := map[string]int{}
	for _, f := range files {
		counts[f.Reason]++
	}
	parts := make([]string, 0, len(counts))
	for reason, n := range counts {
		parts = append(parts, fmt.Sprintf("%s %d", reasonText(reason), n))
	}
	sort.Strings(parts)
	return strings.Join(parts, " / ")
}

func reasonText(reason string) string {
	switch reason {
	case core.SkipEmptyBody:
		return "正文为空"
	case core.SkipFrontmatter:
		return "frontmatter 要求跳过"
	default:
		return reason
	}
}

func daemonRunning(port int) bool {
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)), 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

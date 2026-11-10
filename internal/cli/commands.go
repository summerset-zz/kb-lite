package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
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
	// 离线也吃缓存：文件没变过的就不重切。但**只读**打开 —— 写索引只允许 daemon 做，
	// 否则两边抢 WAL 锁（docs/01 / docs/05）。
	//
	// 仍然会扫目录并比 sha1，所以结果不会是旧的；只是不必重新解析。
	opts := daemon.BuildOptions{}
	if store, err := daemon.OpenStoreReadOnly(cfg.DBPath); err == nil {
		defer store.Close()
		opts.Store = store
	}
	mgr := daemon.NewManager(cfg, opts)
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

// ---- manifest ----

// runManifest 打印 `/kb/{name}/manifest`（docs/04）：按字符预算裁剪的分层概览。
//
// 默认输出就是服务端渲染好的文本 —— 这个接口的存在意义是「便宜」，
// CLI 自己再拼一遍只会多一处会跑偏的渲染。
func runManifest(args []string) int {
	fs := flag.NewFlagSet("manifest", flag.ContinueOnError)
	budget := fs.Int("budget", 0, "")
	depth := fs.Int("depth", 0, "")
	prefix := fs.String("prefix", "", "")
	asJSON := fs.Bool("json", false, "")
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	if len(rest) < 1 {
		fmt.Fprintln(os.Stderr, "用法：kb-lite manifest <库名> [--budget 2000] [--prefix <p>] [--depth 3] [--json]")
		return exitError
	}
	name := rest[0]

	q := url.Values{}
	if *budget > 0 {
		q.Set("budget", strconv.Itoa(*budget))
	}
	if *depth > 0 {
		q.Set("depth", strconv.Itoa(*depth))
	}
	if *prefix != "" {
		q.Set("prefix", *prefix)
	}
	if *asJSON {
		q.Set("format", "json")
	}

	if c, err := newClient(); err == nil {
		body, err := c.get(kbPath(name, "manifest"), q)
		if err == nil {
			printRaw(body)
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
	kb, ok := mgr.KB(name)
	if !ok {
		fmt.Fprintf(os.Stderr, "没有这个知识库：%s（用 kb-lite list 看看有哪些）\n", name)
		return exitNotFound
	}
	m := daemon.BuildManifest(kb, daemon.ManifestOptions{Budget: *budget, Prefix: *prefix, Depth: *depth})
	if *asJSON {
		b, err := json.MarshalIndent(m, "", "  ")
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return exitError
		}
		printRaw(append(b, '\n'))
		return exitOK
	}
	fmt.Print(m.RenderManifestText())
	return exitOK
}

// printRaw 原样输出服务端内容，缺换行时补一个（不然 shell 提示符会贴着最后一行）。
func printRaw(b []byte) {
	os.Stdout.Write(b)
	if len(b) > 0 && b[len(b)-1] != '\n' {
		fmt.Println()
	}
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
	install := fs.Bool("install", false, "装到 agent 的 skill 目录（教 AI 怎么查）")
	installAuthoring := fs.Bool("install-authoring", false, "装另一份：教 AI 怎么写")
	targetFlag := fs.String("target", "dsh", "dsh | claude | both")
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	if *install || *installAuthoring {
		kind := "read"
		if *installAuthoring {
			kind = "authoring"
		}
		return runSkillInstall(kind, *targetFlag)
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

// runSkillInstall 走管理接口安装 SKILL（两份分开装，ADR-017）。
func runSkillInstall(kind, target string) int {
	c, err := newClient()
	if err != nil {
		return fail(err)
	}
	targets := []string{target}
	if target == "both" {
		targets = []string{"dsh", "claude"}
	}
	raw, err := c.admin(http.MethodPost, "/api/skill/install", map[string]any{
		"kind": kind, "targets": targets,
	})
	if err != nil {
		return fail(err)
	}
	var res struct {
		Installed []struct {
			Target string `json:"target"`
			Path   string `json:"path"`
			Bytes  int    `json:"bytes"`
		} `json:"installed"`
	}
	_ = json.Unmarshal(raw, &res)
	if len(res.Installed) == 0 {
		fmt.Println("没有装到任何地方，检查一下 --target。")
		return exitError
	}
	what := "「让 AI 能查知识库」的 SKILL"
	if kind == "authoring" {
		what = "「让 AI 把文件整理成知识库格式」的 SKILL"
	}
	for _, it := range res.Installed {
		fmt.Printf("已安装%s → %s（%d 字节）\n", what, it.Path, it.Bytes)
	}
	fmt.Println("装好后要重启一次 AI 工具才会生效。")
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

	// 离线时只有在“读取面要密码”时才需要 token。没启用密码就渲染一份不带鉴权的文档，
	// 不能留一个空的 `Bearer `（agent 会以为自己的头写错了）。
	token := ""
	if cfg.EffectiveRequireToken() {
		token, err = daemon.LoadOrCreateReadToken(dir)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			return "", exitError
		}
	}

	serviceBase := "http://127.0.0.1:" + strconv.Itoa(cfg.Port)
	if base != "" {
		serviceBase = strings.TrimRight(base, "/")
	}
	return daemon.RenderSkillText(mgr.Index(), serviceBase, token, daemon.DefaultFeatures), exitOK
}

// ---- token ----

// runToken 只讲**读取面**的访问密码。管理凭据（api-token）不提供打印命令（ADR-015）。
func runToken(args []string) int {
	fs := flag.NewFlagSet("token", flag.ContinueOnError)
	show := fs.Bool("show", false, "")
	rotate := fs.Bool("rotate", false, "")
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
	if !cfg.EffectiveRequireToken() {
		fmt.Fprintln(os.Stderr, "当前没有启用访问密码（只有本机能访问）。要开就改 config.json 的 requireToken，或用设置页。")
		return exitOK
	}

	if *rotate {
		_ = os.Remove(filepath.Join(dir, daemon.ReadTokenFileName))
	}
	tok, err := daemon.LoadOrCreateReadToken(dir)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return exitError
	}
	if *rotate {
		fmt.Fprintln(os.Stderr, "已换新密码。已配置的 agent 都要换成新的。")
	}
	if *show || *rotate {
		fmt.Println(tok)
		return exitOK
	}
	if len(tok) > 6 {
		tok = tok[:6] + "…（用 --show 看完整的）"
	}
	fmt.Println(tok)
	return exitOK
}

// ---- settings / stop ----

// runSettings 查看或修改设置。改端口 / 绑定地址要重启才生效。
//
// 没有“换记录文件位置”：它永远和 config.json 放在一起（ADR-018）。
func runSettings(args []string) int {
	fs := flag.NewFlagSet("settings", flag.ContinueOnError)
	port := fs.Int("port", 0, "")
	host := fs.String("host", "", "")
	password := fs.String("password", "", "on | off")
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	c, err := newClient()
	if err != nil {
		return fail(err)
	}

	patch := map[string]any{}
	if *port > 0 {
		patch["port"] = *port
	}
	if *host != "" {
		patch["host"] = *host
	}
	if *password != "" {
		switch *password {
		case "on", "off":
			patch["requireToken"] = *password == "on"
		default:
			fmt.Fprintln(os.Stderr, "--password 只能是 on 或 off")
			return exitError
		}
	}

	if len(patch) == 0 {
		raw, err := c.admin(http.MethodGet, "/api/status", nil)
		if err != nil {
			return fail(err)
		}
		var st struct {
			Port         int    `json:"port"`
			RequireToken bool   `json:"requireToken"`
			ConfigDir    string `json:"configDir"`
			DBPath       string `json:"dbPath"`
			DBMode       string `json:"dbMode"`
		}
		_ = json.Unmarshal(raw, &st)
		// host 不在 status 里，直接读配置（CLI 与 daemon 同一个文件）。
		dir, _ := daemon.ConfigDir()
		cfg, _ := daemon.Load(dir)
		pw := "关闭（只有本机能访问）"
		if st.RequireToken {
			pw = "开启"
		}
		fmt.Printf("端口        %d\n", st.Port)
		if cfg != nil {
			fmt.Printf("绑定地址    %s\n", cfg.Host)
		}
		fmt.Printf("访问密码    %s\n", pw)
		fmt.Printf("配置目录    %s\n", st.ConfigDir)
		fmt.Printf("记录文件    %s（%s）\n", st.DBPath, st.DBMode)
		return exitOK
	}

	raw, err := c.admin(http.MethodPost, "/api/settings", patch)
	if err != nil {
		return fail(err)
	}
	var resp struct {
		RestartRequired bool `json:"restartRequired"`
		Moved           bool `json:"moved"`
		Effective       struct {
			Port         int    `json:"port"`
			Host         string `json:"host"`
			RequireToken bool   `json:"requireToken"`
		} `json:"effective"`
	}
	_ = json.Unmarshal(raw, &resp)
	fmt.Println("已保存。")
	if resp.Moved {
		// 端口与绑定地址当场就生效了，不用重启（docs/04）。
		fmt.Printf("新地址已经上线：http://%s:%d\n", resp.Effective.Host, resp.Effective.Port)
	}
	if resp.RestartRequired {
		fmt.Printf("记录文件位置要重启才生效（重启后是 http://%s:%d）。\n", resp.Effective.Host, resp.Effective.Port)
	}
	if resp.Effective.RequireToken {
		fmt.Println("访问密码已开启：AI 要带上它才连得上（kb-lite token --show 看）。")
	} else {
		fmt.Println("访问密码已关闭：这台电脑上的任何程序都能读你的知识库。")
	}
	return exitOK
}

// runPause 处理 `stop`（暂停）与 `start`（恢复）。
//
// 暂停的是**给 AI 的那一面**：`/kb/*` 与 `/skill.md` 返回 503，界面与 /api/* 照常。
// 监听不关 —— 否则 `start` 就没有可以唤醒它的通道（docs/05）。
func runPause(args []string, paused bool) int {
	fs := flag.NewFlagSet("pause", flag.ContinueOnError)
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	c, err := newClient()
	if err != nil {
		return fail(err)
	}
	path := "/api/resume"
	if paused {
		path = "/api/pause"
	}
	if _, err := c.admin(http.MethodPost, path, nil); err != nil {
		return fail(err)
	}
	if paused {
		fmt.Println("服务已经暂停，AI 现在连不上。用 kb-lite start 恢复。")
	} else {
		fmt.Println("已恢复，AI 现在可以查了。索引还在，没有重新整理。")
	}
	return exitOK
}

// runQuit 请求 daemon 退出（关监听、关记录文件、进程结束）。
func runQuit(args []string) int {
	fs := flag.NewFlagSet("quit", flag.ContinueOnError)
	flagArgs, _ := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	c, err := newClient()
	if err != nil {
		return fail(err)
	}
	if _, err := c.admin(http.MethodPost, "/api/shutdown", nil); err != nil {
		return fail(err)
	}
	fmt.Println("已请求退出。")
	return exitOK
}

// ---- add / remove / enable / disable / rename / reindex ----

// runAdd 优先走管理接口（daemon 在跑时由它负责整理），daemon 没跑就退回改配置。
//
// 回落是过渡期的妥协：v0.3 的向导还没做，用户第一次用时手上没有服务可调。
// 等 UI 出来，`add` 会像 docs/05 写的那样直接要 daemon（退出码 4）。
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
	kbName := *name
	if kbName == "" {
		kbName = filepath.Base(root)
	}

	// 1) daemon 在跑：交给它，建完立刻开始整理。
	if c, err := newClient(); err == nil && c.apiToken != "" {
		raw, apiErr := c.admin(http.MethodPost, "/api/kbs", map[string]any{
			"name": kbName, "rootPath": root, "ignore": ignore, "include": include,
		})
		if apiErr == nil {
			printCreatedKB(raw)
			return exitOK
		}
		var down daemonDownError
		if !errors.As(apiErr, &down) {
			return fail(apiErr)
		}
	}

	// 2) 没在跑：直接改配置，等下次 serve 生效。
	return runAddOffline(root, kbName, ignore, include)
}

func printCreatedKB(raw []byte) {
	var resp struct {
		Name    string `json:"name"`
		State   string `json:"state"`
		Warning string `json:"warning"`
	}
	_ = json.Unmarshal(raw, &resp)
	name := resp.Name
	fmt.Printf("已添加「%s」，正在整理。\n", name)
	if resp.Warning != "" {
		fmt.Printf("  注意：%s\n", resp.Warning)
	}
	fmt.Printf("看进度：kb-lite reindex %s --wait\n", name)
}

func runAddOffline(root, kbName string, ignore, include []string) int {
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

// runRemove 删掉关联与索引条目，**不动目录里的文件**。
func runRemove(args []string) int {
	fs := flag.NewFlagSet("remove", flag.ContinueOnError)
	yes := fs.Bool("yes", false, "")
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	if len(rest) < 1 {
		fmt.Fprintln(os.Stderr, "用法：kb-lite remove <名字> [--yes]")
		return exitError
	}
	name := rest[0]

	if !*yes {
		fmt.Printf("要把「%s」从列表里移除吗？只会删掉登记信息和整理结果，目录里的文件一个都不会动。[y/N] ", name)
		if !confirmStdin() {
			fmt.Println("已取消。")
			return exitOK
		}
	}

	c, err := newClient()
	if err != nil {
		return fail(err)
	}
	raw, err := c.admin(http.MethodDelete, "/api/kbs/"+url.PathEscape(name), nil)
	if err != nil {
		return fail(err)
	}
	var resp struct {
		Removed struct {
			Entries  int `json:"entries"`
			Sections int `json:"sections"`
		} `json:"removed"`
	}
	_ = json.Unmarshal(raw, &resp)
	fmt.Printf("已移除「%s」，删掉 %d 条索引记录 / %d 节。目录里的文件没有动。\n",
		name, resp.Removed.Entries, resp.Removed.Sections)
	return exitOK
}

// runEnable / runDisable：禁用的知识库检索面回 503，索引留着。
func runEnable(args []string, enabled bool) int {
	verb, action := "启用", "enable"
	if !enabled {
		verb, action = "关闭", "disable"
	}
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	if len(rest) < 1 {
		fmt.Fprintf(os.Stderr, "用法：kb-lite %s <名字>\n", action)
		return exitError
	}
	name := rest[0]

	c, err := newClient()
	if err != nil {
		return fail(err)
	}
	if _, err := c.admin(http.MethodPost, "/api/kbs/"+url.PathEscape(name)+"/"+action, nil); err != nil {
		return fail(err)
	}
	if enabled {
		fmt.Printf("已启用「%s」。\n", name)
	} else {
		fmt.Printf("已%s「%s」。索引还留着，重新启用不用重新整理。\n", verb, name)
	}
	return exitOK
}

// runRename 只改显示名，条目不用重切。
func runRename(args []string) int {
	fs := flag.NewFlagSet("rename", flag.ContinueOnError)
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}
	if len(rest) < 2 {
		fmt.Fprintln(os.Stderr, "用法：kb-lite rename <旧名字> <新名字>")
		return exitError
	}
	oldName, newName := rest[0], rest[1]

	c, err := newClient()
	if err != nil {
		return fail(err)
	}
	raw, err := c.admin(http.MethodPatch, "/api/kbs/"+url.PathEscape(oldName), map[string]string{"name": newName})
	if err != nil {
		return fail(err)
	}
	var resp struct {
		Name string `json:"name"`
	}
	_ = json.Unmarshal(raw, &resp)
	fmt.Printf("已改名：「%s」→「%s」。\n", oldName, resp.Name)
	return exitOK
}

// runReindex 触发整理。不传名字就是全部知识库（一个接一个，全局只允许一个任务）。
func runReindex(args []string) int {
	fs := flag.NewFlagSet("reindex", flag.ContinueOnError)
	force := fs.Bool("force", false, "")
	wait := fs.Bool("wait", false, "")
	flagArgs, rest := splitFlags(args, boolFlagNames)
	if err := fs.Parse(flagArgs); err != nil {
		return exitError
	}

	c, err := newClient()
	if err != nil {
		return fail(err)
	}

	names := rest
	if len(names) == 0 {
		raw, listErr := c.admin(http.MethodGet, "/api/kbs", nil)
		if listErr != nil {
			return fail(listErr)
		}
		var resp struct {
			KBs []struct {
				Name string `json:"name"`
			} `json:"kbs"`
		}
		_ = json.Unmarshal(raw, &resp)
		for _, kb := range resp.KBs {
			names = append(names, kb.Name)
		}
		if len(names) == 0 {
			fmt.Println("还没有知识库。")
			return exitOK
		}
	}

	for _, name := range names {
		raw, err := c.admin(http.MethodPost, "/api/kbs/"+url.PathEscape(name)+"/reindex",
			map[string]bool{"force": *force})
		if err != nil {
			var he httpError
			if errors.As(err, &he) && he.status == http.StatusConflict {
				// 已经有一个在跑：等它完再发这一条。
				if *wait {
					if waitErr := waitIndexed(c, name); waitErr != nil {
						fmt.Fprintln(os.Stderr, waitErr)
						return exitError
					}
				} else {
					fmt.Fprintf(os.Stderr, "「%s」暂时没排上：已经有整理任务在跑。加 --wait 可以等它完成。\n", name)
					continue
				}
			} else {
				return fail(err)
			}
		}
		_ = raw
		if *wait {
			if err := waitIndexed(c, name); err != nil {
				fmt.Fprintln(os.Stderr, err)
				return exitError
			}
			fmt.Printf("「%s」整理完成。\n", name)
		} else {
			fmt.Printf("「%s」已开始整理。\n", name)
		}
	}
	return exitOK
}

// waitIndexed 轮询到某个知识库不再整理中。
func waitIndexed(c *client, name string) error {
	deadline := time.Now().Add(30 * time.Minute)
	for time.Now().Before(deadline) {
		raw, err := c.admin(http.MethodGet, "/api/kbs/"+url.PathEscape(name)+"/index", nil)
		if err != nil {
			return err
		}
		var st struct {
			State    string `json:"state"`
			Error    string `json:"error"`
			Progress *struct {
				Done  int `json:"done"`
				Total int `json:"total"`
			} `json:"progress"`
		}
		_ = json.Unmarshal(raw, &st)
		switch st.State {
		case "indexing":
			time.Sleep(200 * time.Millisecond)
		case "error":
			return fmt.Errorf("「%s」整理失败：%s", name, st.Error)
		default:
			return nil
		}
	}
	return fmt.Errorf("等「%s」整理超时", name)
}

// confirmStdin 读一行 stdin，认 y / yes / 是。
func confirmStdin() bool {
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "是":
		return true
	}
	return false
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
	Name     string `json:"name"`
	ID       string `json:"id"`
	RootPath string `json:"rootPath"`
	State    string `json:"state"`
	Error    string `json:"error,omitempty"`
	Entries  int    `json:"entries"`
	Sections int    `json:"sections"`
	Bytes    int64  `json:"bytes"`
	// Reused / Parsed 是本次建索引的增量统计（缓存命中 / 真正重切）。
	Reused   int              `json:"reused"`
	Parsed   int              `json:"parsed"`
	Warnings []warningDTO     `json:"warnings,omitempty"`
	Skipped  []skippedDTO     `json:"skipped,omitempty"`
	Items    []statusEntryDTO `json:"items,omitempty"`
}

// runStatus 建一遍索引并打印统计。
//
// 它是 docs/05 里「daemon 未运行时自动降级为离线只读」的形态：进程内扫目录、切片、报数，
// 不起服务。有缓存时 sha1 没变的文件不会被重新切片 —— 所以它同时也是「二次启动快不快」
// 的自测口。
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

	// status 也吃缓存（只读）：否则「看一眼统计」每次都要全量重切，库大了就受不了。
	opts := daemon.BuildOptions{}
	if store, err := daemon.OpenStoreReadOnly(cfg.DBPath); err == nil {
		defer store.Close()
		opts.Store = store
	}
	started := time.Now()
	mgr := daemon.NewManager(cfg, opts)
	if err := mgr.Rebuild(); err != nil {
		fmt.Fprintln(os.Stderr, "建索引失败：", err)
		return exitError
	}
	took := time.Since(started)

	var out []statusKBDTO
	for _, st := range mgr.States() {
		dto := statusKBDTO{Name: st.Name, ID: st.ID, State: st.State, Error: st.Error}
		if kb, ok := mgr.KB(st.Name); ok {
			dto.RootPath = kb.KB.RootPath
			dto.Entries = len(kb.Entries)
			dto.Sections = kb.Sections
			dto.Bytes = kb.Bytes
			dto.Reused = kb.Stats.Reused
			dto.Parsed = kb.Stats.Parsed
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
		if kb.Reused+kb.Parsed > 0 {
			fmt.Printf("  复用缓存 %d 篇，重新切片 %d 篇，用时 %d ms\n", kb.Reused, kb.Parsed, took.Milliseconds())
		}
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

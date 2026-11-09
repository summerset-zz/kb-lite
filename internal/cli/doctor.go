package cli

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/summerset-zz/kb-lite/internal/daemon"
)

// checkLine 是 doctor 的一行输出。
type checkLine struct {
	what string
	got  string
	// problem 为空表示一切正常；非空就是用户要处理的事。
	problem string
}

// runDoctor 是 `kb-lite doctor`（docs/05）：只读自检，不改任何东西。
//
// 它存在的意义是“用户说不清哪里坏了”时一次问清楚。所以每一项都给具体值与下一步，
// 而不是只打一个叉。
func runDoctor(args []string) int {
	dir, err := daemon.ConfigDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "找不到配置目录：", err)
		return exitError
	}

	fmt.Println("kb-lite " + daemon.Version + " 自检")
	fmt.Println()

	var lines []checkLine
	add := func(what, got, problem string) {
		lines = append(lines, checkLine{what: what, got: got, problem: problem})
	}
	addAll := func(ls ...checkLine) {
		lines = append(lines, ls...)
	}

	// 1. 配置文件
	cfg, cfgErr := daemon.Load(dir)
	if cfgErr != nil {
		add("配置", dir, "config.json 读不了："+cfgErr.Error())
		cfg = daemon.DefaultConfig(dir)
	} else if _, err := os.Stat(filepath.Join(dir, daemon.ConfigFileName)); err != nil {
		add("配置", dir, "还没有 config.json，第一次运行 serve 时会生成")
	} else {
		add("配置", fmt.Sprintf("%s（schema v%d，%d 个知识库）", dir, cfg.SchemaVersion, len(cfg.KBs)), "")
	}

	// 2. 端口：本机这个端口上无人应答，就说明服务没在跑
	if probeDaemon() {
		add("端口", fmt.Sprintf("%d，有服务在跑", cfg.Port), "")
	} else {
		add("端口", fmt.Sprintf("%d，现在没有人监听", cfg.Port), "")
	}

	// 3. 绑定地址与访问密码是否成对
	if daemon.IsLoopbackHost(cfg.Host) {
		add("绑定地址", cfg.Host+"，只有这台电脑能访问", "")
	} else if cfg.EffectiveRequireToken() {
		add("绑定地址", cfg.Host+"，同一网络都能访问，访问密码已强制开着", "")
	} else {
		add("绑定地址", cfg.Host, "绑了非回环地址却没开访问密码，同网段的人都能读你的知识库")
	}

	// 4. 两份凭据文件
	lines = append(lines, checkTokenFiles(dir)...)

	// 5. 记录文件可写
	addAll(checkDBPath(cfg.DBPath))

	// 6. SQLite 驱动
	if daemon.SQLiteAvailable() {
		add("SQLite", "这个二进制里有驱动", "")
	} else {
		add("SQLite", "没有驱动（nosqlite 构建）", "能用，但每次启动都要重新整理一遍；换正式版就好")
	}

	// 7. 每个知识库的根目录还在不在
	for _, kb := range cfg.KBs {
		addAll(checkKB(kb)...)
	}
	if len(cfg.KBs) == 0 {
		add("知识库", "还没有添加任何知识库", "打开界面添加一个，或者在命令行跑 kb-lite add <目录>")
	}

	// 8. 索引里有没有源文件已经消失的记录
	addAll(checkStaleEntries(cfg)...)

	printChecks(lines)

	problems := 0
	for _, l := range lines {
		if l.problem != "" {
			problems++
		}
	}
	if problems == 0 {
		fmt.Println("没发现问题。")
		return exitOK
	}
	fmt.Printf("有 %d 处要注意，见上面标了“注意”的行。\n", problems)
	return exitError
}

// checkTokenFiles 检查两份凭据文件在不在、内容是否非空、权限是否收紧。
func checkTokenFiles(dir string) []checkLine {
	var out []checkLine

	readPath := filepath.Join(dir, daemon.ReadTokenFileName)
	if b, err := os.ReadFile(readPath); err == nil {
		note := ""
		if strings.TrimSpace(string(b)) == "" {
			note = "文件是空的，下次启动会重新生成一把"
		} else if mode, ok := fileMode(readPath); ok && mode != 0o600 {
			note = fmt.Sprintf("权限是 %o，建议 600（同机其他用户能读到）", mode)
		}
		out = append(out, checkLine{"访问密码文件", readPath, note})
	} else {
		out = append(out, checkLine{"访问密码文件", "还没有（没开访问密码时不需要）", ""})
	}

	apiPath := filepath.Join(dir, daemon.APITokenFileName)
	if _, err := os.Stat(apiPath); err != nil {
		out = append(out, checkLine{"管理凭据文件", "还没有，第一次启动 serve 时会生成", ""})
	} else {
		out = append(out, checkLine{"管理凭据文件", apiPath, ""})
	}
	return out
}

// checkDBPath 检查记录文件所在目录能不能写。
//
// 探法跟设置页一样：建个临时文件写完就删。**不能**真去打开 index.db，
// daemon 可能正在用（Windows 上会直接失败）。
func checkDBPath(dbPath string) checkLine {
	probeDir := filepath.Dir(dbPath)
	f, err := os.CreateTemp(probeDir, ".kb-lite-doctor-*")
	if err != nil {
		return checkLine{"记录文件", probeDir, "这个目录不能写入，去设置页换一个位置"}
	}
	name := f.Name()
	_, werr := f.WriteString("ok")
	cerr := f.Close()
	_ = os.Remove(name)
	if werr != nil || cerr != nil {
		return checkLine{"记录文件", probeDir, "写临时文件失败，目录可能只读"}
	}

	got := dbPath + "，可写"
	if info, err := os.Stat(dbPath); err == nil {
		got += "，当前 " + formatBytes(info.Size())
	} else {
		got += "，还没建（第一次整理时生成）"
	}
	return checkLine{"记录文件", got, ""}
}

// checkKB 看一个知识库的根目录还在不在，顺便数一下里面有多少 markdown。
func checkKB(kb daemon.KB) []checkLine {
	info, err := os.Stat(kb.RootPath)
	switch {
	case err != nil:
		return []checkLine{{"知识库 " + kb.Name, kb.RootPath, "找不到这个文件夹了，可能被删掉或改了名字"}}
	case !info.IsDir():
		return []checkLine{{"知识库 " + kb.Name, kb.RootPath, "这不是一个文件夹"}}
	}

	n := 0
	_ = filepath.WalkDir(kb.RootPath, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(d.Name())) {
		case ".md", ".markdown", ".txt":
			n++
		}
		return nil
	})
	return []checkLine{{"知识库 " + kb.Name, fmt.Sprintf("%s，%d 篇 markdown", kb.RootPath, n), ""}}
}

// checkStaleEntries 数一数缓存里登记过、但磁盘上已经没有的文件。
//
// 只读打开缓存库（query_only），所以 daemon 在跑时也能读。
func checkStaleEntries(cfg *daemon.Config) []checkLine {
	if !daemon.SQLiteAvailable() {
		return nil
	}
	if _, err := os.Stat(cfg.DBPath); err != nil {
		return []checkLine{{"索引", "还没有整理过", ""}}
	}
	store, err := daemon.OpenStoreReadOnly(cfg.DBPath)
	if err != nil {
		return []checkLine{{"索引", cfg.DBPath, "打不开缓存库：" + err.Error() + "（删掉它会自动重建）"}}
	}
	defer store.Close()

	total, stale := 0, 0
	for _, kb := range cfg.KBs {
		paths, err := store.StoredPaths(kb.ID)
		if err != nil {
			continue
		}
		total += len(paths)
		for _, rel := range paths {
			if _, err := os.Stat(filepath.Join(kb.RootPath, filepath.FromSlash(rel))); err != nil {
				stale++
			}
		}
	}

	got := fmt.Sprintf("%d 条记录", total)
	if stale > 0 {
		return []checkLine{{
			what: "索引", got: fmt.Sprintf("%s，其中 %d 条的源文件已经不见了", got, stale),
			problem: "在界面里点一次“更新”就会把这些记录清掉",
		}}
	}
	return []checkLine{{"索引", got + "，源文件都在", ""}}
}

// printChecks 对齐打印。名字列按显示宽度补空格（中文算两格）。
func printChecks(lines []checkLine) {
	width := 0
	for _, l := range lines {
		if w := displayWidth(l.what); w > width {
			width = w
		}
	}
	for _, l := range lines {
		pad := strings.Repeat(" ", width-displayWidth(l.what)+2)
		fmt.Printf("%s%s%s\n", l.what, pad, l.got)
		if l.problem != "" {
			fmt.Printf("%s注意：%s\n", strings.Repeat(" ", width+2), l.problem)
		}
	}
	fmt.Println()
}

// probeDaemon 看本机配置的地址上有没有服务答话。
func probeDaemon() bool {
	c, err := newClient()
	if err != nil {
		return false
	}
	_, err = c.admin("GET", "/api/status", nil)
	return err == nil
}

// fileMode 返回权限位。Windows 上 Chmod 本来就不生效，返回 ok=false 表示“查不了”。
func fileMode(path string) (os.FileMode, bool) {
	if runtime.GOOS == "windows" {
		return 0, false
	}
	info, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return info.Mode().Perm(), true
}

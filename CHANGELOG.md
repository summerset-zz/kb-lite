# CHANGELOG

本文件按 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 组织，版本号按 [SemVer](https://semver.org/lang/zh-CN/)。
`0.x` 期间允许破坏性变更，但必须写在这里。

## [Unreleased]

v0.3 = **界面**：一个不想碰命令行的人，从双击 exe 到跑通向导、装上 SKILL、让 AI 查上，全程不用终端。

### Added

- **Web 界面**（`web/`）：Vite + Vue 3 + TypeScript + Tailwind v4，产物拷进 `internal/daemon/static/`
  再 `//go:embed`。用户侧不需要 Node。
  - 首次启动向导三步：记录文件位置、端口与访问范围、第一个知识库。完成页给四个“复制”
    （命令示例、给 AI 的说明书、MCP 配置、整理说明），只复制不下载文件。
  - 知识库页：条目与节数、占用、上次整理时间、告警数、整理进度；行尾操作有更新、全部更新、
    启用或关闭、改名、移除。移除的二次确认写明“目录里的文件一个都不会动”，按钮写“移除”。
  - 设置页：记录文件位置、端口、谁可以打开、访问密码（可复制可直接粘给 AI、可换一个）、
    暂停服务、重新运行向导、配色、退出。
  - 弹层用原生 `<dialog>`（焦点陷阱与 Esc 由浏览器负责），暂不引 Reka UI；图标暂用文字按钮。
- **`GET /authoring.md`**：与 `/skill.md` 平级的第二份说明（教 AI 怎么写），同属读取面，
  开了访问密码就要带。两份分开是 ADR-017 的要求。
- **`POST /api/auth/token`**：`show` 把当前读取密码回给设置页，`rotate` 换一把并**立刻生效**
  （旧的作废），新密码落到 `<配置目录>/read-token`。
- **`POST /api/settings` 支持 `setupCompleted`**：向导最后一屏靠它收尾，否则每次打开都回向导。
- 界面端口显式写死：daemon 7331、`pnpm dev` 5199（`strictPort`），避开本机常见的 3311 / 5173 占用。
- **暂停与恢复**（`POST /api/pause` / `/api/resume`、`kb-lite stop` / `start`、设置页与页头按钮）：
  暂停只挡**给 AI 的那一面**（`/kb/*`、`/skill.md`、`/authoring.md` 返回 `503 paused`），
  界面、管理接口、索引、进程全都保留，恢复不重新整理。
  监听不关是刻意的：否则用户点不到“继续使用”，`kb-lite start` 也没有可以唤醒它的通道。
- `GET /api/status` 增加 `paused`。
- `kb-lite quit` 从 `stop` 里分出来：`stop` = 暂停，`quit` = 退出。docs/05 一直是这么写的，
  之前实现把两者并成了一个，CLI 与文档对不上。

### Fixed

- 读取密码可以热更新了（`Handler.ReadToken` / `SetReadToken` + 一把 RWMutex）。
  换密码后 `/kb/*` 与 `/skill.md` 立刻按新密码判定，不用重启。

### Changed

- **设置页改端口或绑定地址不再需要重启**：新增 `daemon.Server`，先把新地址真的绑上，
  成功了再关旧的；绑不上就什么也不动，旧地址继续服务。响应里多了 `moved`，
  界面据此提示新地址并给出跳转（`docs/04` 里“热重开排在 UI 那一刀”兑现）。
  现在唯一还要重启的是记录文件位置（换库要重开 SQLite）。
- `ApplySettings` 改成“校验全部通过才落到内存配置”，修掉一个旧问题：
  端口已写进内存、目录探测再失败时，内存配置会与磁盘不一致。

## [0.2.0] - 2026-11-09

v0.2 = **落盘与增量**：缓存可以随时删，删了只损失启动时间（ADR-003）。

### Added

- **SQLite 缓存**（`internal/daemon/sqlite.go`）：`modernc.org/sqlite` v1.29.10，纯 Go 无 cgo，五个平台仍然交叉编译通过。
  `PRAGMA user_version` 管版本，遇到更高版本拒绝启动（不尝试降级）。缓存打不开 / `--no-db` / `-tags nosqlite` 构建时退回纯内存，功能不减。
- **增量索引**：文件原始字节的 sha1 与缓存相同就**不解析**，只读文件比解析一篇便宜一个量级。
  1000 篇实测：冷启动 179 ms、二次启动 **53 ms**（`go test -bench BenchmarkRebuild ./internal/daemon`）。
- `serve --force`：忽略缓存全部重切。`serve` 启动会打印「复用缓存 N 篇，重新切片 M 篇」。
- `status` 与离线 `search` / `read` / `toc` 也吃缓存（**只读连接**，写索引仍然只允许 daemon）。
- 删掉的文件在 `tombstone` 留痕，便于诊断「为什么这篇不见了」。
- **`GET /api/status`**（docs/04）：版本 / 端口 / `dbMode` / 每个 KB 的 `state`、条目数、`lastIndexedAt`、告警数，
  索引中还会有 `progress`。`state == "indexing"` 时返回的检索结果仍是上一次的 —— 调用方据此判断新鲜度。
- **`GET /kb/{name}/manifest`**（docs/04）：按**字符预算**裁剪的分层概览，给 agent 在「不知道搜什么词」时用。
  `?budget=&prefix=&depth=&format=text|json`；默认文本形态（JSON 的括号是冤枉钱）。
  预算算的是渲染出来的字符数（含末尾折叠提示），实测 `budget=1200` 就在 1200 字以内。
- `kb-lite manifest <库名> [--budget] [--prefix] [--depth] [--json]`，daemon 没跑时自动离线渲染。
- `/skill.md` 里 manifest 那一段现在会渲染出来（路由存在才渲染，`DefaultFeatures` 统一管）。

### Changed

- 产物体积 8.44 MB → **11.46 MB**（windows/amd64，zip 4.93 MB）。SQLite 的纯 Go libc 转译层占大头，换来的是二次启动从全量重切变成增量。
- 「空白容忍匹配」：标识符型关键词在普通匹配失败后，再试一次「文本在关键词相邻字符之间插入了空白」的匹配。
  `GB18030` 现在能命中写成 `GB 18030` 的地方（半角空格 / 制表 / 换行 / 全角空格），边界规则完全不变
  （仍不命中 `xGB 18030`、`GB 18030x`）。排序快照逐字节未变 —— 现有语料的相对顺序没有被这次改动影响。

### Fixed

- 复用缓存时告警不会消失：`warn` 表跟着条目一起存取。否则会出现「第一次启动 8 条 frontmatter 告警，第二次 0 条」——那正是 v0.1 验收里误导 agent 的那个坑的变种。
- `kb-lite search` 少给参数时静默退出 1，现在打印用法。`kb-lite search GB18030`（忘了写库名）原本看起来就像「搜不到」。

## [0.1.0] - 2026-11-09

第一个可用版本：**core + daemon + CLI + `/skill.md`**，没有 UI（按 `docs/09` 的顺序）。

### Added

- **切片**（`internal/core/slice.go`）：逐行 ATX 扫描，代码围栏内不认标题；章节编号全局扁平、`0` 为导言、空节保留；标题取值链与「低信息量文件名」清单（`readme` / `index` / `untitled` / 语言码）按 `docs/02` 实现。
- **检索**（`internal/core/search.go`）：关键词 × 字段位置的加权计数，七档权重与 `docs/03` 逐字一致；ASCII 标识符手写词边界（RE2 无 lookbehind，不用 `\b`）；CJK bigram 只加分、不参与 `strong`；多查询各自算分再按 `rel_path` 合并相加。
- **frontmatter**（`internal/core/frontmatter.go`）：`name`/`description`/`keywords` 别名；严格 YAML 失败时按行宽松解析（`in: a TTF` 这类裸冒号不再整篇丢弃）。
- **daemon**：`config.json`（`enabled` 默认 true、原子写、token 32 字节 hex 0600）、目录扫描（BOM / 严格 UTF-8 / GB18030 回退 / U+FFFD 复查）、`atomic.Pointer` 快照索引、worker pool + 按索引位回填保证并发确定性。
- **HTTP**（`docs/04`）：`GET /skill.md`、`GET /kb/{name}/`，子资源 `/section` `/nearby` `/toc`；中间件顺序 Host(421) → Sec-Fetch-Site(403) → Origin(403) → token(401/429)；**不发任何 CORS 头**。
- **CLI**：`serve` / `add` / `list` / `remove` / `status` / `search` / `toc` / `read` / `skill` / `token` / `version`；退出码 0/1/2/3/4；flags 可跟在位置参数之后。
- **离线降级**：`search` / `toc` / `read` / `skill` 在服务没起时自己建内存索引，行为与走 HTTP 完全一致（同一份 `AnswerSection`）。
- **`skill --out`**：自己写 UTF-8 无 BOM（PowerShell 的 `>`/`Out-File` 会写 UTF-16 或带 BOM，打断 frontmatter）；离线路径也会生成 token。

### Changed

- **语言选型 TS → Go**（ADR-013）：用户侧零运行时安装，单机交叉编译五个平台，产物约 8 MB。
- 正文为空（只含 frontmatter）的条目整篇跳过，不再产出一个只有标题的空壳。
- 告警带类别（`too-large` / `encoding` / `frontmatter` / `read`），CLI 按类别汇总。

### Validated

- `go vet ./...` 与 `go test ./...` 干净；五个目标平台在 Windows 上交叉编译通过。
- 真实语料：fontcover-content **213 篇 / 2059 节 / 1.5 MB**（232 个 md = 213 进索引 + 19 篇空正文跳过），fontcover-v2/docs 34 篇 / 485 节。
- **agent 验收通过**：DeepSeek Harness 只读 `skill.md`，一次多词查询后按 `n=` 读节，引用了路径与节号，并识破了一处「`unicode-range` 其实是 OS/2 表的 `ulUnicodeRange`」的假阳性。

### Known limitations

- `GB18030` 搜不到文中写作 `GB 18030` 的地方（不做模糊匹配，见 `docs/03`「刻意不做」）。
- `skill.md` 里的端口/token 是生成时的快照：`add` / 改端口 / `token --rotate` 之后要重新 `skill --out`（v0.2 提供 `kb-lite skill --install`）。
- 无 SQLite、无增量索引、无 UI。

### Validated

- 真实语料（251 个文件 / 1.5 MB）：冷启动 185 ms → 二次启动 **64 ms**；复用缓存时 frontmatter 告警一条不少。
- 五个目标平台交叉编译通过；`-tags nosqlite` 瘦身构建 8.24 MB 也能跑。

[Unreleased]: https://github.com/summerset-zz/kb-lite/compare/v0.2.0...HEAD
[0.2.0]: https://github.com/summerset-zz/kb-lite/releases/tag/v0.2.0
[0.1.0]: https://github.com/summerset-zz/kb-lite/releases/tag/v0.1.0

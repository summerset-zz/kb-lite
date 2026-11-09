# CHANGELOG

本文件按 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/) 组织，版本号按 [SemVer](https://semver.org/lang/zh-CN/)。
`0.x` 期间允许破坏性变更，但必须写在这里。

## [Unreleased]

### Added

- **开机自启**（设置页复选框、`kb-lite autostart status|enable|disable`、`GET|POST /api/startup`）。
  v0.4 只做 Windows：写 `HKCU\…\CurrentVersion\Run`，不需要管理员。写进去的命令是
  `"<exe>" serve`（不带 `--open`），并原样显示给用户看。
  界面会提醒两种坑：程序还在下载或临时目录里（随时会被清掉）、自启指向的是另一份 kb-lite。
- **创建桌面快捷方式**（设置页按钮、`kb-lite shortcut`）：桌面建 `KBLite.lnk`，
  目标 `"<exe>" serve --open`。桌面目录向系统要，不拼 `~/Desktop`（OneDrive 接管后路径会变）。
- **检查更新**（设置页、`kb-lite update [--refresh]`、`GET /api/update`）：读 GitHub 的
  `releases/latest`，结果缓存 10 分钟。**这是唯一会访问外网的地方**，只在用户点按钮时发。
  查不到时回 `ok:false` 加一句人话，不当成接口错误。
- **底部提示条**（`web/src/toast.ts` + `components/ToastHost.vue`）：保存成功、换了密码、
  开关自启、建了快捷方式、暂停与恢复都给一句反馈，几秒后自己消失。
  自己实现，不引整套组件库（ADR-016），也**不需要 Provider 包住整个应用**：
  容器在 `App.vue` 里挂一次，任何组件 `useToast()` 就往同一个队列里推。
  以前保存成功什么都不说，用户只看到页面没变，会以为没保存上。
- **目录选择器可以直接输入路径**：顶上那条从只读文本变成输入框，可以把路径粘进来
  （从资源管理器的地址栏复制），回车或“前往”跳过去。逐层点到大盘深处的目录太慢。

### Changed

- **`GET /api/fs/list` 的候选目录不再带 `hasMarkdown` / `markdownCount`**（破坏性）。
  之前为了显示“这里有 12 个 markdown 文件”，每列一层都要递归走一遍每个子目录，
  在大盘根目录上一换层就卡好几秒。那行信息对“选哪个文件夹”帮助也有限，去掉。

### Fixed

- **重新运行向导时，第三步看不到已有的知识库**。以前这一步只让选文件夹，已经加好的库一个都不显示，
  看着像“之前的知识库条目没了”；而且不选文件夹就没法往下走（再选一次同一个目录还会撞上「重名」）。
  现在先列出已有的库（名字 + 目录），下面才是「再加一个（可选）」；已经有库的人可以直接往下走，
  同一个文件夹重复选中会提示「这个文件夹已经是知识库「X」了」，不会再提交一次。
- **目录选择器第一次打开什么都不出来**，下面挂着一句「缺少 path 参数」。`GET /api/fs/list`
  以前不接受空 `path`；现在空 `path` 表示「给我一个起点」：Windows 回盘符列表，其它平台回一遍家目录。
- **界面里存不了任何配置**（`serve` 建索引管理器时漏了配置目录）。从 v0.3 起，界面上每一次保存
  （开关访问密码、建库、改端口、换记录文件位置、向导收尾）都回「不知道配置目录，改不了配置」，
  因为落盘要用到配置目录，而这个参数只有 `serve` 该填、却一直没填。
  加了一个真的把 `serve` 跑起来再调管理接口的测试 —— 之前的测试都自己把配置目录摆好了，
  所以没人抓到。
- **补上单实例锁**（`docs/06` 第二节早就写了，代码里一直没有）。重复双击、或开机自启与手动打开撞在一起时，
  第二个进程以前会默默占下一个端口（端口自动 +1 让它看起来成功），于是两个实例、两个托盘图标、
  配置被反复改写。现在启动前先看 `<配置目录>/lock`：pid 还活着或那条端口上有人应答，就只把界面打开并退出自己。
  热重开改端口后锁也会跟着改。

## [0.3.0] - 2026-11-09

v0.3 = **界面**：一个不想碰命令行的人，从双击 exe 到跑通向导、装上 SKILL、让 AI 查上，全程不用终端。

### Added

- **`.github/workflows/ci.yml`**：三个 OS 跑 `go vet` + `go test`；五个目标逐交叉编译
  （另加一次 `-tags nosqlite`）；界面单独一个 job 跑 `typecheck` + `build`。
  `setup-go` 用 `go-version-file: go.mod`，所以“还支持 1.22”每次都被验证一遍。
- **`.github/workflows/release.yml`**：打 `v*` tag 就发版。Windows 出 zip、Unix 出 tar.gz，
  附 `SHA256SUMS.txt`；正文是固定开头三段（是什么 / 下载哪个 / 被拦了怎么办）+ CHANGELOG 对应段落。
  凭据用 Actions 自带的 `GITHUB_TOKEN`，不需要任何 PAT。
  **界面在 `go build` 之前构建**：`web/dist` 是编译时 embed 进二进制的，
  少了这步会发出带「界面还没有构建」占位页的包。
- README 的快速开始改成“下载 → 解压 → 双击”三步，并把 SmartScreen / Gatekeeper 的点击路径写清楚。
- **Agent 视角页**（导航第三个页签「给 AI 看」）：把 AI 真正收到的原文摊开
  （说明书、manifest、一次检索的 JSON、一次按节阅读），每块自己点“取一次”。
  看不懂是正常的，这一页的目的就是让人不用信我们的描述，自己看。
- **`POST /api/preview`**（管理面）：服务端带着读取密码代取一次，回原样的
  `status / contentType / body`。界面拿不到读取密码（ADR-015），自己发一定 401；
  代取才能看到真的响应，包括暂停时的 `503 paused`。只允许 `skill / authoring /
  manifest / search / section` 五个 kind，路径必须是相对路径且不含 `..`，
  绝不能变成任意 URL 代理。
- **搜索调试页**（导航第二个页签「搜索」）：回答“为什么搜不到”“为什么这条排第一”。
  每条结果下面直接给出“为什么排在这里”的命中表（不做下拉，这一页存在的理由就是看它），
  列按 `docs/03` 固定的顺序
  （标题 / 条目标题 / 路径 / 章节 / 摘要 / 全文）打勾，下面给出强命中、弱命中、
  只在全文里三个计数；有 bigram 近似命中时单独说明加分。
- **`POST /api/search`**（管理面）：与 `/kb/{name}/search` **共用 `searchPayload`**，所以分数不会漂。
  界面手上只有管理凭据，拿不到读取密码，因此搜索调试页不能直接打 agent 那一面。
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
- **Windows 托盘**（`internal/tray`，纯 Go + `syscall`，无 cgo，交叉编译五个平台照旧）：
  两态图标（服务在跑 / 已暂停，图标在内存里画，不带任何图片资源）、
  右键菜单（地址可点复制并弹气泡、暂停或启动、索引进度只在进行中出现、设置、在命令行中打开、退出）、
  双击打开界面。菜单每次右键现建，所以永远等于此刻状态；tooltip 每 3 秒才可能更新一次，
  且内容没变就不发 `NIM_MODIFY`。托盘起不来不影响服务，`serve --no-tray` 可关掉。
- 界面页签跟 URL 锚点走（`/#settings`、`/#about`）：托盘的“设置”能直接落到设置页，刷新也不丢当前页。
- **`kb-lite doctor`**：只读自检。看配置、端口有没有人应答、绑定地址与访问密码是否成对、
  两份凭据文件、记录文件目录能不能写、SQLite 驱动、每个知识库的目录还在不在、
  以及缓存里有多少条记录的源文件已经消失。每项都给具体值与下一步，有一条不对就以 1 退出。
- 自助诊断需要的两个小接口：`Store.StoredPaths`（只读、只取一列）与 `SQLiteAvailable()`。
- `kb-lite quit` 从 `stop` 里分出来：`stop` = 暂停，`quit` = 退出。docs/05 一直是这么写的，
  之前实现把两者并成了一个，CLI 与文档对不上。

### Fixed

- `/kb`（没带库名）不再落到 UI 兜底：返回 `404 kb_name_required` 并指向 `/skill.md`。
  以前调错地址的 agent 会拿到一页 HTML，以为接口通了，然后在解析阶段莫名失败。
- 向导完成页的“命令示例”原来写的是 `curl .../kb`（不存在这个接口，拿回来是 HTML），
  改成 `curl .../skill.md`，与启动横幅里那句一致。
- 读取密码可以热更新了（`Handler.ReadToken` / `SetReadToken` + 一把 RWMutex）。
  换密码后 `/kb/*` 与 `/skill.md` 立刻按新密码判定，不用重启。

### Changed

- 开发期间版本号带 `-dev`（如 `0.3.0-dev`），别人报 issue 时一眼看出是未发布的中间态；
  发版时改成与 tag 一致的数字，下一轮开发再带上新的 `-dev`。
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

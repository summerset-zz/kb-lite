# AGENTS.md

给（人类或 AI）实现者的入口。**`docs/` 是规格书，代码必须服从文档；文档与代码不一致时，先改文档再改代码，并且要说明为什么。**

## 这是什么

`kb-lite`：把本地 markdown 目录变成只读 HTTP 知识库，供用户的 agent 用 `curl` 检索与按节阅读。零 LLM、零向量、目录即真相。

不是笔记应用，不是 RAG 框架，不是聊天工具。

## 必读顺序

1. [`docs/00-vision.md`](./docs/00-vision.md) —— 边界。**先看「明确不做」那一节。**
2. [`docs/01-architecture.md`](./docs/01-architecture.md)
3. [`docs/02-slicing.md`](./docs/02-slicing.md) —— 契约，逐字实现
4. [`docs/03-search.md`](./docs/03-search.md) —— 算法，逐字实现
5. [`docs/04-http-api.md`](./docs/04-http-api.md) —— 接口与文案
6. [`docs/05-cli.md`](./docs/05-cli.md) · [`docs/06-desktop-ui.md`](./docs/06-desktop-ui.md) · [`docs/07-security.md`](./docs/07-security.md)
7. [`docs/08-build-release.md`](./docs/08-build-release.md) · [`docs/09-roadmap.md`](./docs/09-roadmap.md) · [`docs/10-decisions.md`](./docs/10-decisions.md)
8. 给 agent 的两份模板：[`docs/skill-template.md`](./docs/skill-template.md)（教**读**）· [`docs/authoring-template.md`](./docs/authoring-template.md)（教**写**）

## 技术约束（硬）

**语言选型的第一原则是「对用户影响最小」**：用户拿到的应该是一个双击即用的二进制，不该为了跑它去装任何运行时。理由见 [ADR-013](./docs/10-decisions.md#adr-013-服务端用-go)。

- **服务端（core / daemon / cli）= Go ≥ 1.22**，产物是**单个静态二进制**，用户侧零安装。
- **`internal/core` 不得 import `net/http` / `os` / `path/filepath`，不得有运行时依赖。** 切片与打分必须是纯函数（输入字符串 → 输出对象）。
- HTTP：`net/http` + Go 1.22 的 `ServeMux`（原生支持方法匹配与 `{path...}` 通配）。**不要引入三方路由或 Web 框架**。
- 正则**只用 RE2 能表达的部分**：Go 的 `regexp` 没有 lookbehind / lookahead。词边界一律手写 rune 检查（见 03）。
- 三方依赖只允许**纯 Go、无 cgo、可交叉编译**的那种，且加之前先说明标准库为什么做不到：
  `gopkg.in/yaml.v3`（frontmatter）、`golang.org/x/text`（GB18030，必要时 collation）、
  `modernc.org/sqlite`（v0.2 起，纯 Go）。**不要 `mattn/go-sqlite3`**（要 cgo，交叉编译立刻全废）。
- SQLite 拿不到时降级为**纯内存**并正常运行。
- 前端：`web/`，**Vite + Vue 3 + TS + Tailwind v4**，交互原语用 `reka-ui`（无样式），图标 `@iconify-json/lucide`。
  **禁止 SSR / Nuxt、CSS-in-JS、Element/Naive/Ant 这类整套组件库**（ADR-016）。**禁止 `innerHTML` 渲染用户内容**。
  产物用 `//go:embed` 打进二进制 —— **用户侧永远不需要 Node**。
- 托盘：**纯 Go 自己实现**（Windows 先行的 `Shell_NotifyIcon` + 消息循环），**不引入 cgo，不上 Tauri/Electron**（ADR-014）。
  平台代码用 `_windows.go` 包起来，其他平台编空实现。
- 凭据分两级（ADR-015）：读取面（`/kb/*`、`/skill.md`）的 **token 默认关**，用户可在设置页打开；
  管理面（`/api/*`）**永远**要 admin 凭据（`HttpOnly; SameSite=Strict` cookie / CLI 的 `X-KB-Admin`），
  并且要求 `Sec-Fetch-Site: same-origin` 或 `Origin: <自己>`。**绑定非回环地址时强制开启读取 token。**
- 两份 skill **分开**（ADR-017）：`/skill.md`（教读）与 `authoring.md`（教写）不得合并。
- 端口/路径/凭据 **一律从 `config.json` 读**，不硬编码。

## 不能自己发挥的地方

以下内容**照抄文档**，不允许「顺手优化」：

| 项 | 见 |
|---|---|
| 章节编号规则（全局扁平、0 = 导言、空节保留） | 02 |
| 标题取值链与「低信息量文件名」清单 | 02 |
| 打分权重与字段名 | 03 |
| `toc` 的文本格式（`<编号> <标题> · 268 字` / `· 空`） | 04 |
| 三种 `note` 文案 | 04 |
| `200 + ok:false + candidates` 的错误形态 | 04 / ADR-010 |
| 只读（没有改知识库文件内容的接口） | ADR-004 |
| 不发任何 CORS 头 | 07 |
| 界面文案：**假定用户只会用 AI、不懂电脑**，术语对照表照抄 | 06 §8 |
| 托盘菜单/暂停与退出的语义、CLI 与 GUI 必须一一对应 | 06 §2 / §10 |

## 实现顺序

严格按 [`docs/09-roadmap.md`](./docs/09-roadmap.md)：**v0.1 只做 core + daemon + CLI + `/skill.md`，不要提前写 UI。**

v0.1 的验收标准是「一个真 agent 能靠 `skill.md` 独立完成检索」，达不到就不要进入 v0.2 —— 后面的所有工作都建立在这个协议是对的之上。

## 开发命令

```bash
go vet ./...              # 只读，随时可跑
go test ./...             # 只读，随时可跑
go run ./cmd/kb-lite serve   # 本地起 daemon（前台运行）
go build -o dist/ ./cmd/kb-lite   # 产出二进制，不要在没有确认的情况下跑
```

前端（`web/`，只在改 UI 时需要）用 `pnpm`。它的产物是给 `//go:embed` 吃的，**不进入用户的分发物**。

## 提交与发布

- Conventional Commits（`feat:` / `fix:` / `docs:` / `refactor:`），`CHANGELOG.md` 由它生成。
- 改切片或打分的 commit **必须带排序快照的 diff 说明**（为什么这个排序变了、变对了吗）。
- 版本按 SemVer；`0.x` 期间允许破坏性变更，但必须写进 CHANGELOG。

## 每个 PR 的自检

- [ ] `go vet ./...` 与 `go test ./...` 干净
- [ ] 没有引入需要用户安装运行时 / 原生依赖 / cgo 的东西
- [ ] 新增/修改路径有测试（尤其路径越界、只读性）
- [ ] 若改了接口字段：`docs/04-http-api.md` 与 `docs/skill-template.md` 同步更新
- [ ] 没有引入 ADR 里明确否掉的方案（向量、FTS5、写接口、后台维护任务）

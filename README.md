# kb-lite

> 你的 markdown 目录，一个 curl 就能查的知识库。
>
> **零 LLM、零依赖、只读、目录即真相。服务端自带写给 agent 的使用说明。**

把一堆 `.md` 文件指给 kb-lite，它切好、索引好、在本地起一个 HTTP 服务；你的 Agent、聊天机器人、harness 用 `curl` 就能检索和阅读，而且拿到的不是一个语无伦次的片段，而是一份**结构化的目录**，再按编号精确读某一节。

不接向量库，不调大模型，不要 API key。删掉索引文件也不会丢一个字的资料。你的 markdown 就是数据库。

除了一件事：**默认不联网**。只有你自己点「检查更新」时，它才会去 GET 一次 GitHub 的 releases 接口。
没有后台同步、没有自动上传、没有遥测。

---

## 为什么不用现成的

| 你可能想用的 | 为什么这里还是值得有一个 kb-lite |
|---|---|
| **basic-memory** | 强，但 MCP-first（每个客户端 spawn 一份）、要 Python/uv、要求按它的 `Observations/Relations` 语法写笔记、语义检索要 embedding |
| **obsidian-local-rest-api** | 最像，但必须常驻 Obsidian、搜索是 Obsidian 自己的模糊搜索、而且**全量可写**（给 agent 用太危险） |
| **Trilium / SilverBullet / Khoj** | 都是“笔记应用自带 API”，不是“给你的目录加一个只读的 agent 接口”，而且体量重 |
| **llm-wiki / 各类 MCP 小项目** | 理念对了（wiki over RAG），但依赖 LLM 跑后台任务、排序靠 BM25、几乎没有中文优化 |

kb-lite 只做一件事：**把一个 markdown 目录，变成 agent 能高效导航的只读接口。**

## 它长什么样

```
$ kb-lite add ~/notes --name 我的笔记
已索引“我的知识库”：128 篇 / 743 节 / 2.1 MB

$ curl "$BASE/kb/我的笔记/search?q=部署流程&q=deploy"
[
  { "title": "运维手册", "path": "ops/runbook.md", "score": 33,
    "summary": "线上环境的上线、回滚与排障流程。",
    "toc": "1 上线流程 · 1203 字\n2 回滚 · 380 字\n  3 常见故障 · 空" }
]

$ curl "$BASE/kb/我的笔记/entry/ops/runbook.md/section?n=2"
{ "sectionTitle": "回滚", "path": ["上线与回滚", "回滚"], "content": "…" }
```

两段式：**搜索只回候选（标题 + 摘要 + 文本目录），正文按编号或标题再读一次。** 这样 1000 篇笔记也不会一次把上下文塞满。

## 快速开始

去 [Releases](../../releases) 下载对应平台的一个文件，解压，**双击**。

| 你的电脑 | 下载 |
|---|---|
| Windows 普通电脑 | `kb-lite-win-x64.zip` |
| Windows on ARM | `kb-lite-win-arm64.zip` |
| Mac（Apple 芯片） | `kb-lite-macos-arm64.tar.gz` |
| Mac（Intel） | `kb-lite-macos-x64.tar.gz` |
| Linux x64 | `kb-lite-linux-x64.tar.gz` |

第一次打开会进向导，四屏：**东西放在哪 → 端口与谁可以访问 → 选一个放着资料的文件夹 → 总结**。
配置目录不让你选（它就和程序的数据放在一起，ADR-018），你只要看一眼、知道备份时搬哪个目录。

走完向导，最后一屏直接给你两个「安装 SKILL」按钮；下面折叠区里另有三段可以直接粘给 AI 的东西
（说明书全文、说明书地址、整理说明）。以后要暂停、启动、复制地址，都在顶栏那一行状态上。

**不需要 Node、不需要 Python、不需要 Docker、不需要 API key。** 一个 11 MB 左右的静态二进制（含纯 Go 的 SQLite 缓存），界面的静态资源也打在里面。

### 第一次打开被拦了？

这个软件没有买代码签名证书（每年几百美金）。所以：

- **Windows**：弹「Windows 已保护你的电脑」时，点「更多信息」→「仍要运行」。
- **macOS**：右键点图标 →「打开」；或 `xattr -d com.apple.quarantine ./kb-lite-macos-arm64`。

源码都在这仓库里，不放心就自己 `go build`。

### 命令行用户的用法

```bash
kb-lite serve --open          # 起服务并打开界面
kb-lite add ~/notes           # 加一个知识库（会立刻开始整理）
kb-lite doctor                # 哪儿不对就让它先看一眼
kb-lite quit                  # 退出（stop 只是暂停，AI 连不上但进程还在）
```

## 本地开发与构建

只改 Go 的话，装个 Go 1.22 以上就够了。要改界面才需要 Node 22 与 `pnpm`。
**这两样只在你的开发机上用，用户那边永远不需要。**

### 一条命令

```bash
cd web
pnpm serve
```

它按这个顺序做完四件事，顺序是有讲究的：

1. 调 `dist/kb-lite.exe quit` 停掉旧进程。Windows 上正在运行的 exe 不能覆盖，不停下来 `go build` 会报拒绝访问；不停的话新进程还会因为端口被占自动换一个，浏览器却停在旧地址。
2. `pnpm build`：类型检查、构建界面、把产物拷进 `internal/daemon/static/`。
3. `go build -o dist/kb-lite.exe ./cmd/kb-lite`。
4. 前台起服务并打开浏览器。`Ctrl+C` 停。

只动了 Go 的话加 `--no-web` 跳过第 2 步，快很多。

### 分步跑

```bash
cd web && pnpm build          # 界面：类型检查 + 构建 + 拷到 static
go build -o dist/kb-lite.exe ./cmd/kb-lite   # 二进制
./dist/kb-lite.exe serve --open              # 跑起来
```

**界面必须在 `go build` 之前构建好。** 界面是 `//go:embed` 打进二进制的，嵌入发生在编译那一刻；反过来做（先编译再建界面）编出来的还是旧界面。

### 产物在哪

| 路径 | 是什么 | 谁生成 |
|---|---|---|
| `web/dist/` | 界面产物：`index.html`、`assets/`、`public/` 里的静态文件 | `vite build` |
| `internal/daemon/static/` | 上面那份的拷贝，加一个 `.gitkeep` | `scripts/sync-static.mjs` |
| `dist/kb-lite.exe` | 二进制，界面已经在里面了 | `go build` |

三个都不进版本库。`dist/` 这个目录本身要留着，`go build -o` 不会自己建父目录。

### 改界面时的快速循环

```bash
cd web
pnpm dev
```

开在 `http://localhost:5199`，把 `/api`、`/kb`、`/skill.md`、`/authoring.md` 代理到 `127.0.0.1:7331` 的 daemon。所以先把 daemon 起起来，改界面时不用重编二进制。daemon 端口改过的话，`web/vite.config.ts` 里那个 `daemon` 常量也要跟着改。

`pnpm preview` 只预览静态产物，没配代理，`/api` 一律 404，别看它验功能。

### 检查

```bash
go vet ./...
go test ./...
cd web && pnpm typecheck
```

三项都是只读的，随时可以跑。

### 换图标

源图只有两份，512×512、正方形、透明底：

```
internal/tray/assets/normal.png    运行中
internal/tray/assets/warn.png      已暂停
```

替换之后：

```bash
cd web
pnpm icons
```

它会更新浏览器标签页的 `web/public/favicon.png`，以及 exe 的图标与版本信息
（`cmd/kb-lite/rsrc_windows_*.syso`，由 `go-winres` 生成一次、随仓库提交，CI 不需要装它）。
托盘图标不用重新生成：二进制里存的是原图，运行时按系统 DPI 缩。

## 文档

给实现者读的完整规格在 [`docs/`](./docs/)：

| 文档 | 内容 |
|---|---|
| [00-vision](./docs/00-vision.md) | 定位、目标用户、竞品、明确不做什么 |
| [01-architecture](./docs/01-architecture.md) | 进程模型、数据流、包边界、配置 |
| [02-slicing](./docs/02-slicing.md) | 切片契约：标题、编号、导言、超长节、忽略规则 |
| [03-search](./docs/03-search.md) | 检索与打分（完整算法，可直接实现） |
| [04-http-api](./docs/04-http-api.md) | 全部端点与请求/响应结构 |
| [05-cli](./docs/05-cli.md) | 命令行契约 |
| [06-desktop-ui](./docs/06-desktop-ui.md) | 桌面/Web UI 评估与功能规格 |
| [07-security](./docs/07-security.md) | 本地服务的安全边界 |
| [08-build-release](./docs/08-build-release.md) | 仓库结构、构建、发布、宣传页 |
| [09-roadmap](./docs/09-roadmap.md) | 分阶段范围 |
| [10-decisions](./docs/10-decisions.md) | 已定决策与被否掉的方案及理由 |
| [11-writing-style](./docs/11-writing-style.md) | 行文风格（文档与界面文案都适用） |
| [12-backlog](./docs/12-backlog.md) | 现在到哪儿了、下一步做什么 |
| [skill-template](./docs/skill-template.md) | 服务端吐出、给使用侧 agent 的说明文档 |
| [authoring-template](./docs/authoring-template.md) | 给“把 docx / pdf 转成知识库”的 AI 的整理说明 |
| [maintain-template](./docs/maintain-template.md) | 给能跑 shell 的 agent 的维护说明（增删改知识库登记） |

## 许可

[MIT](./LICENSE)。免费、开源，没有商业版，没有云服务，也不打算有。

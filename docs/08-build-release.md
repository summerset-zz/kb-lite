# 08 · 仓库结构、构建与发布

## 仓库

`github.com/summerset-zz/kb-lite`，MIT，公开。**没有商业版、没有云服务、不接受赞助作为承诺**（可以在 README 放一句“如果有用，star 就够了”）。

```
kb-lite/
  go.mod                  # module github.com/summerset-zz/kb-lite，go 1.22
  cmd/kb-lite/main.go     # 入口 + 子命令分发
  internal/
    core/                 # 切片 + 打分（纯函数，不 import net/http / os / path/filepath）
      types.go
      slice.go            # 逐行 ATX 扫描
      search.go           # 加权计数
      frontmatter.go
      build.go            # 目录 → 条目（由 daemon 传进来，自己不读盘）
      *_test.go           # 含切片与排序快照
    daemon/
      config.go           # config.json + 配置目录
      scan.go             # 发现 + 解码 + 越界校验
      index.go            # 内存索引（atomic.Pointer 快照）+ 整理调度
      sqlite.go           # 缓存（目录即真相，ADR-003）
      driver_modernc.go   # 带 build tag，纯 Go SQLite
      driver_none.go      # 无驱动时的纯内存降级
      http.go             # 路由 + 中间件（token / Host / Origin）+ /kb/* 分发
      api_*.go            # 管理面各接口
      server.go           # 监听、热重开、暂停
      skill_template.md   # //go:embed，docs/skill-template.md 的镜像
      authoring_template.md
      static/             # //go:embed 的 UI 产物（只有 .gitkeep 进版本库）
    tray/                 # Windows 托盘，纯 Go + syscall
    cli/                  # 子命令，每个一个文件
  web/                    # Vite + Vue 3，产物拷进 internal/daemon/static
  website/                # GitHub Pages 宣传页：纯静态 HTML + CSS，不构建
  docs/                   # 本套文档 = 规格书
  .github/workflows/      # ci.yml（测试 + 交叉编译 + 界面）、release.yml（打 tag 发版）、pages.yml
  AGENTS.md
  CHANGELOG.md
  LICENSE
  README.md
```

**没有 `Makefile`，也没有 `desktop/`。** 构建脚本在 `web/package.json`
（`pnpm build` 里已经包含 `vue-tsc --noEmit`、`vite build` 与拷进 `static/` 三步）。
Tauri 桌面壳还没开始（见 [09](./09-roadmap.md)），先不要在文档里假装它存在。

- **服务端没有任何 JS 工具链。** 切片器就是 02 里那套逐行 ATX 扫描（`slice.go`，约 200 行），**不用 `goldmark` 之类的 Markdown 库**。我们要的是“按行切、不解析行内语法”，一个完整解析器在这里是净负担，而且它的行为不完全可控。
- **Go ≥ 1.22**（`net/http` 的 `ServeMux` 增强路由是 1.22 才有的）。CI 跑 1.22 与最新稳定版。
- `web/` 用 pnpm + Vite；它的 `package.json` **只服务于构建**，不产生任何用户需要安装的东西。

## 构建

```
cd web && pnpm build   # vue-tsc --noEmit → vite build → 拷进 internal/daemon/static/
go build -o dist/kb-lite.exe ./cmd/kb-lite
```

开发时 `cd web && pnpm serve`：停掉旧进程、构建界面、编译并起服务，一条命令。
（`pnpm dev` 只起 Vite，5199，代理到 daemon 的 7331，改 UI 时用它。）

产物：

| 产物 | 用途 | 备注 |
|---|---|---|
| `dist/kb-lite-<os>-<arch>[.exe]` | 发布用 | Go 静态二进制，**~11 MB**（实测见下表） |
| `internal/daemon/static/` | UI 产物 | `//go:embed` 编进二进制，**不单独分发** |

### 交叉编译（这是换 Go 换来的最大简化）

```bash
GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/kb-lite-win-x64.exe ./cmd/kb-lite
GOOS=windows GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/kb-lite-win-arm64.exe ./cmd/kb-lite
GOOS=darwin  GOARCH=arm64 go build -trimpath -ldflags="-s -w" -o dist/kb-lite-macos-arm64 ./cmd/kb-lite
GOOS=darwin  GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/kb-lite-macos-x64 ./cmd/kb-lite
GOOS=linux   GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o dist/kb-lite-linux-x64 ./cmd/kb-lite
```

**一台机器出全部产物**，CI 不需要 5 个 runner（对比：Node SEA 不能交叉编译，每个平台必须在自己的 runner 上构建）。

`-trimpath` 去掉本机绝对路径，`-s -w` 去掉符号表（体积约减 30%）。不打 `-buildmode=pie`，也不要 `CGO_ENABLED=1`。

v0.2 的实测体积（在 Windows 上一台机器交叉编译全部五个目标，含 `x/text` + `yaml.v3` + **纯 Go 的 `modernc.org/sqlite`**）：

| 目标 | 体积 |
|---|---|
| `windows/amd64` | 11.46 MB（zip 后 4.93 MB）|
| `windows/arm64` | 10.56 MB |
| `darwin/arm64` | 10.75 MB |
| `darwin/amd64` | 11.42 MB |
| `linux/amd64` | 11.20 MB |

对比 v0.1（没有 SQLite）：windows/amd64 8.44 MB。**SQLite 花了约 3.0 MB**（纯 Go 的 libc 转译层占大头），
换掉的是“每次启动都要全量重切”，1000 篇从 179 ms 降到 53 ms（`go test -bench BenchmarkRebuild ./internal/daemon`）。
不想要这 3 MB 可以 `-tags nosqlite` 构建：功能全在，只是每次都纯内存重切（见 `driver_none.go`）。

不裁剪（带符号）的同一产物是 16.90 MB，`-s -w` 省下约 32%。

只要 `modernc.org/sqlite` 这个纯 Go 依赖还在，上面每一条都能在 Windows 上跑。**一旦有人换成 `mattn/go-sqlite3`，交叉编译会立刻全部失败**，而且错误发生在链接阶段、很难一眼看出原因。这是必须在 code review 里拦住的事。

任何一个目标平台构建失败都是**阻塞问题**（不像 SEA 时代可以退化成“要求用户装 Node”。那正是我们要消灭的东西）。

## CI

`.github/workflows/ci.yml`，**已实现**。三个 job：

| job | 干什么 | 为什么 |
|---|---|---|
| `test` | 三个 OS 各跑 `go vet` 与 `go test`（`CGO_ENABLED=0`） | 路径越界、大小写、Windows 短名这些坑是平台特异的 |
| `cross-build` | 五个目标逐交叉编译（另加一次 `-tags nosqlite`） | 换语言换来的最大收益，断了就没意义了 |
| `web` | `pnpm install --frozen-lockfile` → `typecheck` → `build` | 界面挂了 release 也发不出去 |

`setup-go` 用 `go-version-file: go.mod` 而不是 `stable`：这样“我们真的还支持 1.22”每次都被验证一遍。

**测试也要 `CGO_ENABLED=0`**，与发出去的二进制一致。不设的话，macOS 上 `net` 与
`database/sql` 会拉进 cgo 解析器，测试二进制走外部链接（clang），在 macOS 15 的 dyld 上直接
`missing LC_UUID load command` 然后 `abort trap`。那个报错与我们的代码无关，但会把 CI 天天打红。
第一次遇到时只红 `internal/cli` 与 `internal/daemon`，`internal/core` 反而是绿的 ——
因为只有前两个 import 了 `net`。

三个 OS 都要跑：路径越界、大小写、8.3 短名这些坑是平台特异的。

**测试里必须有**：

- 切片快照（含代码围栏里的假标题、空父节、超长节、无标题长文、GB18030 文本、BOM、CRLF）；
- 排序快照（至少 15 个查询，覆盖：ASCII 标识符词边界、中文多字词、重复标题、`README.md` 陷阱、全文弱命中不该霸榜）；
- **路径越界测试**（`../../../etc/passwd`、Windows 短名、symlink 指向根外、`.git/config`）；
- **只读测试**：`DELETE /api/kbs/{name}` 之后断言目录里的文件一个没少。

## 发布

`.github/workflows/release.yml`：`on: push: tags: ['v*']`，**已实现**。

矩阵产出（Windows 打 zip，Unix 打 tar.gz：裸二进制在浏览器里下下来往往丢掉可执行位）：

```
kb-lite-win-x64.zip          （里面是 kb-lite-win-x64.exe）
kb-lite-win-arm64.zip
kb-lite-macos-arm64.tar.gz
kb-lite-macos-x64.tar.gz
kb-lite-linux-x64.tar.gz
SHA256SUMS.txt
```

**凭据：不需要任何 PAT。** 上传 Release 用 Actions 自动注入的 `GITHUB_TOKEN`，
workflow 里声明 `permissions: contents: write` 就够。tag 由人来打，workflow 只负责构建与上传。

**构建顺序有个坎（漏了会发错包）**：界面必须在 `go build` **之前**构建好。
`web/dist` 是编译时 `//go:embed` 进二进制的，而 `internal/daemon/static/` 里除了
`.gitkeep` 都不进版本库 —— 新 clone 下来直接 `go build`，产出的二进制会显示
「界面还没有构建」。所以 release 的步骤固定为：pnpm 构建界面 → 再交叉编译。
CI 里也有一份同样的检查（`web` job）。

Release 正文 = **固定的开头三段** + `CHANGELOG.md` 里这个版本那一段（用 awk 摘）。
固定三段：

1. 一句话是什么；
2. 下载哪个文件、要不要安装（答案是**不用，双击即用**）；
3. 遇到 SmartScreen / Gatekeeper 怎么办（写清点击路径，附截图）。

没有对应 CHANGELOG 段落时 workflow 会告警：正文只剩标题的发版比不发更糟。

**不再有 npm 包。** `npx kb-lite serve` 这条路径已经不需要了。二进制本身就是零安装，而且更快、不依赖网络。仓库里给 `web/` 用的 `package.json` 不要发布。

## GitHub Pages 宣传页

- 仓库 Settings → Pages → Source = **GitHub Actions**，由 `.github/workflows/pages.yml` 发布
  `website/` 目录的**原样内容**。
- 站点地址是 `https://summerset-zz.github.io/kb-lite/`，仓库名就是 `kb-lite`，所以站点挂在
  一个子路径下。**页内资源一律用相对路径**（`./style.css`）：相对路径天然不受 base path 影响，
  那个“本地看得见、线上 404”的坑就不存在了。

技术选择：**纯静态 HTML + CSS，不建构建链**。

- 这一页只有 HTML、CSS 与几张图片。上 Vite 的收益是 0（没有 TS、没有组件、没有需要拆包的依赖），
  成本是 CI 里多一份 Node 安装与一条要长期升级的工具链。
- 更要紧的是，`web/` 已经有一份 Node 工具链了。宣传页再引一份，等于把“升级依赖”这件事变成两处。
- 将来真需要打包（比如把 `docs/` 转成一组 HTML）再上 Vite，那时把 `base` 设成 `/kb-lite/`。
  在那之前**不要**在文档里写 `vite.config.ts`，它不存在。
- **仍然不要 Astro / Next**：一页静态站的框架负担是纯成本。

内容顺序（照这个顺序写，不要按“功能列表”写）：

1. **一句话 + 一段 curl 的真实输出**（第一屏就要让人看到“哦这是给我 agent 用的”）；
2. 三个终端块：`add` → `search` → `read`，真实的输出，不美化；
3. “为什么不用 basic-memory / obsidian-local-rest-api”的对比表（**沿用 README 那张**，两边必须一致）；
4. 安装（Windows / macOS / Linux 各下载一个文件，双击即用）；
5. 截图：主界面 + **Agent 视角页**（这张最有说服力）；
6. FAQ：会不会上传我的笔记（不会）/ 要不要 API key（不要）/ 支持 PDF 吗（不）/ 能多人用吗（用 git）；
7. 底部：GitHub、Releases、License。

**宣传页与 README 的定位句必须逐字一致。** 两处说法不同的项目，用户会认为两边都在夸张。

现在只有一页。将来想把 `docs/` 里的几篇做成 HTML，那是另一页的事，到时再定怎么生成；
在那之前不要把文档站塞进这一页。

### 加文件（图片、图标、字体）

`website/` 里放什么都行，引用时用相对路径即可：`<img src="./img/hero.png" alt="">`。
workflow 把整个目录原样上传，没有清单要维护，也不用改任何配置。

几条实际会碰到的限制：

- 整站不超过 **1 GB**（Pages 的发布上限）。
- **文件名不要带空格与中文**。URL 里要转义，图片最容易在复制粘贴时坏掉而又不报错。
- Pages 对静态文件的缓存大约 10 分钟。文件名不变时，重新发布后浏览器可能还拿着旧图，
  加个版本后缀（`hero-v2.png`）比自己清缓存省事。
- 图片给 `width` / `height`，首屏之外的加 `loading="lazy"`，不然首屏会跳。

**不要用 base64 内联**。base64 把体积撑大约三分之一，而且 HTML 没法单独缓存：
改一个字节，整页连同那段 base64 都要重新下载。截图压到 200 KB 以内、存成 WebP 更划算。
只有几 KB 的小图标（favicon 这类）才值得内联。

### 如果哪天真的上了 Vite

要发布的是 `vite build` 输出的**整个 `dist/`**，不是“三个文件”。`index.html` 引用的是
带 hash 的 css / js，被 import 的图片也会变成 `assets/` 下的独立文件；少传一个就是白屏。

那时要改两处：workflow 里加装 Node 与构建两步，`path` 从 `website` 改成 `website/dist`；
`vite.config.ts` 里 `base: './'`，这样不必把仓库名写死进配置。

## 社区

- `CONTRIBUTING.md` 里写清：切片与打分是核心，改它们必须带排序快照的 diff；
- Issue 模板：`bug`（含 `kb-lite doctor` 输出）、`检索不准`（含 `--explain` 输出）、`新功能`；
- **`检索不准` 模板是本项目最重要的一个**。它同时是产品反馈渠道和权重调参的数据来源。

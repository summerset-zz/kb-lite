# 05 · CLI

CLI 是 daemon 的**薄客户端**。除了 `serve`，所有命令都翻译成一次 HTTP 调用。

> 为什么不让 CLI 直接读写 SQLite：会与正在运行的 daemon 抢 WAL 锁，并且等于把业务逻辑实现两遍。CLI 唯一的“本地能力”是**离线只读模式**（见文末）。

```
kb-lite <command> [options]
```

## serve

```
kb-lite serve
  --port <n>          默认取 config，其次 7331；被占则自动 +1
  --host <addr>       默认 127.0.0.1。改成 0.0.0.0 时打印醒目警告
  --open              启动后打开浏览器
  --force             启动时忽略 sha1，把所有知识库全部重切一遍
  --no-tray           不挂 Windows 托盘（服务器、容器、脚本用）
  --no-db             纯内存模式（不落盘）
  --log-level <l>     error|warn|info|debug，默认 info
```

**没有 `--db`**：记录文件永远是 `<配置目录>/index.db`（ADR-018）。要换地方就是换配置目录、整包搬家。
启动前还会看 `<配置目录>/lock`，已经有实例在跑就只把界面打开、自己退出（`docs/06` 第二节）。

前台运行，Ctrl+C 退出。**不做后台守护化**。需要常驻请用系统服务（见 08）或桌面版（见 06）。

## 知识库管理

```
kb-lite add <dir> [--name <n>] [--include <glob>]… [--ignore <glob>]…
   # daemon 在跑：存进配置并**立刻开始整理**
   # daemon 没跑：退回直接改 config.json，下次 serve 生效（退回是有意保留的，见下面那张表）
kb-lite list [--json]
kb-lite remove <name> [--yes]
kb-lite enable <name>
kb-lite disable <name>
kb-lite rename <old> <new>

kb-lite reindex [name] [--force] [--wait]
   # 不传 name = 全部 KB（一个接一个，全局只允许一个任务）；--wait 阻塞到完成并打印统计
kb-lite status [--json]
kb-lite settings [--port <n>] [--host <addr>] [--password on|off]
   # 改完立即生效（需要时自己重新监听），不需要重启进程
   # 不带参数时只打印当前设置：端口、绑定地址、访问密码开没开、配置目录、记录文件
kb-lite doctor
   # 自检：端口、凭据文件权限、DB 可写、每个 KB 的根目录是否还在、
   #      索引里有多少条记录的源文件已消失、SQLite 驱动是否可用、-h 返回的 Host 校验配置
```

`remove` 的确认文案必须写明“只删除关联与索引条目，不会删除 `D:\notes` 里的任何文件”，并且需要 `--yes` 或交互确认。

## 检索（调试与自用）

```
kb-lite search <name> <query…> [--limit 5] [--explain] [--json]
kb-lite toc    <name> <path>
kb-lite read   <name> <path> [--section <n> | --title <t>] [--offset <n>] [--max-chars <n>]
kb-lite manifest <name> [--budget 2000] [--prefix <p>]
```

- 每个位置参数是一次查询（可以给多个做对照）。
- `--explain` 打印关键词、每个关键词命中的位置（标题/条目标题/路径/章节/摘要/全文），格式见 [03](./03-search.md#--explain-输出cli-与-ui-共用)。**这是用户自助排查的唯一入口，必须做好。**
- 默认输出是**人读的表格**（不是 JSON）；`--json` 时原样打印服务端 JSON。

示例输出：

```
我的笔记 ← "部署 回滚"          关键词 = ["部署","回滚"]   返回 2 条
  1.  42  运维手册            ops/runbook.md
         部署: 标题 路径 章节 全文    回滚: 路径 分支 章节
  2.  11  服务器迁移记录       ops/migrate.md
         部署: 全文                   回滚: 全文
```

## 其它

```
kb-lite skill [--url <base>] [--out <path>]
   # 打印 /skill.md 的内容（“让 AI 能查我的笔记”那份）
   # 说明书里不写库列表，所以没有 --kb：库由 agent 每次 GET /kb 自己取
   # --out 由进程自己写文件（UTF-8 无 BOM）。不要用 shell 的 `>`：
   #   PowerShell 的 `>` 会写成 UTF-16，`Out-File -Encoding utf8` 会带 BOM，
   #   两者都会让 YAML frontmatter 失效，skill 就读不出来了。
kb-lite skill --install [--target dsh|claude|both]
   # 装到 ~/.agents/skills/kb-lite/SKILL.md（默认）与 ~/.claude/skills/kb-lite/SKILL.md
kb-lite skill --install-authoring [--target dsh|claude|both]
   # 另一份：教 AI **怎么写**（~/.agents/skills/kb-lite-authoring/）
   # 两份**分开装**（ADR-017）：合成一份后 agent 会在只想查东西的时候也去改笔记
kb-lite token [--show] [--rotate]
   # 只针对读取面那把“访问密码”。管理凭据（api-token）不提供打印命令（ADR-015）
   # ⚠️ `--show` 在开启读取密码时才有意义；关着时直接告诉你“当前没有启用访问密码”
kb-lite mcp
   # v0.4 尚未实现（ADR-006：把 stdio 的 MCP 调用转发到本地 HTTP 的薄壳）
kb-lite sync <name> [--wait]
   # v0.5 尚未实现。git -C <root> pull --ff-only，然后 reindex。
   # 只 pull，不 add/commit/push。冲突或非 fast-forward 时不强行处理，
   # 打印 git 的输出并退出码 2，让用户自己解决。
kb-lite stop
   # 暂停服务：AI 那一面（/kb/*、/skill.md）返回 503，**进程、索引、托盘、管理面都保留**；
   # 再 `start` 立刻恢复，不重切。监听不关 —— 否则 `start` 就没有可以唤醒它的通道
kb-lite start
   # 从暂停状态恢复（打 /api/resume；不传参时是前台进程，所以不要指望它后台跑）
kb-lite quit
   # 关监听 → 关记录文件 → 进程结束
kb-lite autostart [status|enable|disable]
   # 开机自启。目前只有 Windows（HKCU Run，不需要管理员）。
   # 直接在**本机**改，不需要 daemon 在跑 —— 用户完全可能在想配自启时服务没开
kb-lite shortcut
   # 在桌面建一个 KBLite 快捷方式（Windows）
kb-lite update [--refresh]
   # 查一次有没有新版本。**这是唯一会访问外网的命令**，只在明确敲它时发一次请求
kb-lite version
```

> **`stop` 与 `quit` 是两个动作**：托盘菜单、UI、CLI 三处叫法必须一致（`docs/06` 第二节）。

## 退出码

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 一般错误 |
| 2 | 外部依赖失败（git 冲突、端口全被占、DB 版本过新） |
| 3 | 查无此物（KB / 条目 / 章节），便于脚本分支 |
| 4 | daemon 未运行（需要它的写操作） |

## daemon 未运行时的行为

| 命令 | 行为 |
|---|---|
| `search` / `toc` / `read` / `manifest` / `status` / `skill` / `list` | **自动降级为离线只读**：进程内扫目录，并用 SQLite（**只读连接**）按 sha1 跳过没变过的文件。SQLite 不存在或 `--no-db` 时现场全量切片（慢一点，但结果一致） |
| `add` | 优先走管理接口；daemon 没跑时退回直接改 `config.json`（过渡行为） |
| `remove` / `enable` / `disable` / `rename` / `reindex` / `settings` / `skill --install` / `sync` / `stop` / `start` / `quit` | 报错退出（码 4）并提示 `先运行 kb-lite serve`。**不要静默替你起一个后台 daemon**。用户不知道它在跑，之后端口占用、日志找不到、升级时进程还在，全是麻烦 |

> `settings` / `skill --install` 之所以也必须走 daemon：它们要改的是**正在运行的那份配置**，
> 自己改 `config.json` 会和内存里的状态对不上（端口、监听地址、skill 渲染日期）。

## 实现注意

- 参数解析用标准库 `flag`（子命令各建一个 `FlagSet`），**不要引入 cobra / urfave/cli**。命令数量撑不起一个框架的依赖链与错误文案。
- 所有输出走 `stdout`；错误与警告走 `stderr`（保证 `--json` 时可以管道）。
- 中文对齐：表格列宽按**显示宽度**算（CJK 算 2），否则 `--explain` 输出的表格会歪。
- 不要把 token 打进 `debug` 日志。`--verbose` 打印请求 URL 时也要把 `Authorization` 头隐去。

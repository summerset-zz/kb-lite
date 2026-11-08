# 05 · CLI

CLI 是 daemon 的**薄客户端**。除了 `serve`，所有命令都翻译成一次 HTTP 调用。

> 为什么不让 CLI 直接读写 SQLite：会与正在运行的 daemon 抢 WAL 锁，并且等于把业务逻辑实现两遍。CLI 唯一的「本地能力」是**离线只读模式**（见文末）。

```
kb-lite <command> [options]
```

## serve

```
kb-lite serve
  --port <n>          默认取 config，其次 7331；被占则自动 +1
  --db <path>         覆盖数据库路径
  --host <addr>       默认 127.0.0.1。改成 0.0.0.0 时打印醒目警告
  --open              启动后打开浏览器
  --no-db             纯内存模式（不落盘）
  --log-level <l>     error|warn|info|debug，默认 info
```

前台运行，Ctrl+C 退出。**不做后台守护化** —— 需要常驻请用系统服务（见 08）或桌面版（见 06）。

## 知识库管理

```
kb-lite add <dir> [--name <n>] [--include <glob>]… [--ignore <glob>]…
kb-lite list [--json]
kb-lite remove <name> [--yes]
kb-lite enable <name>
kb-lite disable <name>
kb-lite rename <old> <new>

kb-lite reindex [name] [--force] [--wait]
   # 不传 name = 全部 KB；--wait 阻塞到完成并打印统计
kb-lite status [--json]
kb-lite doctor
   # 自检：端口、token 文件权限、DB 可写、每个 KB 的根目录是否还在、
   #      索引里有多少条记录的源文件已消失、SQLite 驱动是否可用、-h 返回的 Host 校验配置
```

`remove` 的确认文案必须写明「只删除关联与索引条目，不会删除 `D:\notes` 里的任何文件」，并且需要 `--yes` 或交互确认。

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
kb-lite skill [--url <base>] [--kb <name>]
   # 打印 /skill.md 的内容，可直接重定向成 SKILL.md
kb-lite token [--show] [--rotate]
kb-lite sync <name> [--wait]
   # git -C <root> pull --ff-only，然后 reindex。
   # 只 pull，不 add/commit/push。冲突或非 fast-forward 时不强行处理，
   # 打印 git 的输出并退出码 2，让用户自己解决。
kb-lite stop
kb-lite version
```

## 退出码

| 码 | 含义 |
|---|---|
| 0 | 成功 |
| 1 | 一般错误 |
| 2 | 外部依赖失败（git 冲突、端口全被占、DB 版本过新） |
| 3 | 查无此物（KB / 条目 / 章节）—— 便于脚本分支 |
| 4 | daemon 未运行（需要它的写操作） |

## daemon 未运行时的行为

| 命令 | 行为 |
|---|---|
| `search` / `toc` / `read` / `manifest` / `status` / `skill` | **自动降级为离线只读**：进程内直接读 SQLite（不需要 daemon）。SQLite 不存在或 `--no-db` 时，现场扫描目录切片（慢一点，但结果一致） |
| `add` / `remove` / `enable` / `disable` / `rename` / `reindex` / `sync` / `stop` | 报错退出（码 4）并提示 `先运行 kb-lite serve`。**不要静默替你起一个后台 daemon** —— 用户不知道它在跑，之后端口占用、日志找不到、升级时进程还在，全是麻烦 |

## 实现注意

- 参数解析用标准库 `flag`（子命令各建一个 `FlagSet`），**不要引入 cobra / urfave/cli** —— 命令数量撑不起一个框架的依赖链与错误文案。
- 所有输出走 `stdout`；错误与警告走 `stderr`（保证 `--json` 时可以管道）。
- 中文对齐：表格列宽按**显示宽度**算（CJK 算 2），否则 `--explain` 输出的表格会歪。
- 不要把 token 打进 `debug` 日志。`--verbose` 打印请求 URL 时也要把 `Authorization` 头隐去。

# 01 · 架构

## 进程模型

**一个常驻进程**（daemon），做四件事：

```
        ┌──────────────────────────────── kb-lite daemon ────────────────────────────────┐
        │                                                                              │
文件系统 ─┼─▶ watcher(可选) ─┐                                                          │
        │                   ├─▶ 切片器(core) ─▶ 索引(内存) ─┬─▶ HTTP 检索接口           │
        │  reindex 命令 ────┘                              │                           │
        │                                                   └─▶ SQLite(仅缓存) ──┐     │
        │                                                                         │     │
        │  HTTP 管理接口 ◀── UI(浏览器 / Tauri 壳) ◀───────────────────────────────┘     │
        └──────────────────────────────────────────────────────────────────────────────┘
```

- **只有一个进程能写索引**（daemon）。CLI 不直接碰 SQLite，而是调 daemon 的 HTTP 接口 —— 避免 WAL/锁竞争，且代码只有一份。
- 管理面（`/api/*`）也只存在于 daemon 里；UI 与 CLI 都是它的客户端（`docs/06` 第十节是平权清单）。
- CLI 在 daemon 未启动时会**自己起一个短暂的 in-process 实例**做只读操作（`search` / `read`），或者在需要写的时候提示用户先 `serve`。见 [05-cli](./05-cli.md)。
- UI 是纯静态资源，由 daemon 托管在 `/`（Tauri 壳只是换了承载方式，见 [06-desktop-ui](./06-desktop-ui.md)）。

## 数据流

1. **发现**：遍历每个 KB 的根目录，按 [02-slicing](./02-slicing.md#文件发现与忽略) 的规则得到文件清单。
2. **切片**：对每个文件算 `sha1`；与索引里的记录相同则跳过，否则重新解析为 `Entry + sections[]`。
3. **落盘**：写入 SQLite（可关）。
4. **装载**：把「检索需要用到的字段」全部载入内存（每篇一份 `search_text` + 目录 + 摘要）。
5. **服务**：HTTP 请求全部走内存索引，不查 SQLite。

> **为什么内存里全量装**：一个 5000 篇的中文笔记库，正文总量通常在 10–50 MB，`search_text` 压到几 MB 级别；全表扫 5 个关键词是**毫秒级**。这条约束换来的是「不需要 FTS5、不需要中文分词器、不需要分档捞候选」—— 排序行为与线上版完全一致且更可控。

## 包边界

```
cmd/kb-lite/     入口：serve 与各子命令分发
internal/core/   纯函数，零 I/O，零三方依赖（除 frontmatter 的 yaml）
                 切片：ParseMarkdownSections() / BuildEntry()
                 检索：TokenizeQuery() / BuildMatchers() / ScoreEntry() / RankEntries()
                 类型：Entry / Section / SearchHit / TocEntry
internal/daemon/ Go 进程：文件遍历、哈希、SQLite、内存索引、HTTP 服务、静态 UI 托管
internal/cli/    argv 解析 + 调 HTTP + 输出（含 --explain 表格）
web/             前端源码（Vite + Vue，产物用 //go:embed 进 daemon）
desktop/         Tauri 壳（可选产物，见 06）
```

**`internal/core` 不许 import `net/http` / `os` / `path/filepath`** —— 这条是为了 ① 让它能被独立测试（不需要 mock 文件系统）、② 把「读取」和「理解」分开。

> UI 不再直接复用 core 做实时切片预览（那是 JS 时代的设计）；预览改走 daemon 的 `POST /api/preview/slice`，本地回环 < 1 ms，对用户无感知。见 [ADR-013](./10-decisions.md#adr-013-服务端用-go)。

## 存储

### 配置文件

| 平台 | 目录 |
|---|---|
| Windows | `%APPDATA%\kb-lite\` |
| macOS | `~/Library/Application Support/kb-lite/` |
| Linux | `${XDG_CONFIG_HOME:-~/.config}/kb-lite/` |

```
config.json      # 端口、绑定地址、数据库路径、KB 列表、读取面是否要密码
read-token       # 读取面凭据，32 字节随机 hex，0600（仅 requireToken 打开时有意义）
api-token        # 管理面凭据，同上；UI 靠 HttpOnly cookie 拿，界面上不显示（ADR-015）
index.db         # SQLite（默认位置，用户可改）
lock             # pid + 实际端口，给单实例唤醒用
logs/daemon.log  # 轮转，10 MB × 3
```

`config.json`：

```jsonc
{
  "schemaVersion": 2,
  "setupCompleted": true,        // 向导跑完了没；false 时 UI 进向导
  "port": 7331,
  "host": "127.0.0.1",           // 非回环时 requireToken 强制 true
  "requireToken": false,         // 读取面是否需要密码（默认关）
  "dbPath": "C:\\Users\\me\\AppData\\Roaming\\kb-lite\\index.db",
  "kbs": [
    {
      "id": "k1f3a9c2",             // 稳定短 id，名字可以改，id 不变
      "name": "我的笔记",            // 展示名，唯一
      "rootPath": "D:\\notes",      // 绝对路径，posix 化后再存也可以
      "enabled": true,
      "ignore": [".git", "node_modules", "_drafts"],
      "include": ["**/*.md", "**/*.txt"]
    }
  ]
}
```

> **KB 的对外标识用 `name`**（对人和 agent 都友好，`curl .../kb/我的笔记/...`），内部用 `id`。改名要同步改 `config.json` 里的 `name`，`id` 不动，索引无需重建。

### SQLite（缓存，可随时删）

```sql
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT);
-- meta: schema_version, last_index_at

CREATE TABLE entry (
  kb_id       TEXT NOT NULL,
  rel_path    TEXT NOT NULL,   -- posix 相对路径，条目对外 id
  sha1        TEXT NOT NULL,   -- **文件原始字节**的 sha1，用来判断「变没变」
  mtime_ms    INTEGER NOT NULL,
  size_bytes  INTEGER NOT NULL,
  title       TEXT NOT NULL,
  summary     TEXT NOT NULL,
  search_text TEXT NOT NULL,   -- 见 03：全文小写合并，只给匹配用（= Entry.SearchFull）
  char_count  INTEGER NOT NULL,
  indexed_at  INTEGER NOT NULL,
  PRIMARY KEY (kb_id, rel_path)
  -- 实现补齐的列：rev、search_title / search_path / search_path_leaf /
  -- search_meta、toc_text、toc(json)、tags(json)。理由见下面的「实现注记」。
);

CREATE TABLE section (
  kb_id       TEXT NOT NULL,
  rel_path    TEXT NOT NULL,
  section_no  INTEGER NOT NULL, -- 0 = 导言；全局扁平连续
  level       INTEGER NOT NULL,
  title       TEXT NOT NULL,
  path        TEXT NOT NULL,    -- 面包屑的 json 数组 ["上线与回滚","回滚"]
  path_text   TEXT NOT NULL,    -- 面包屑 "上线与回滚 / 回滚"
  chars       INTEGER NOT NULL, -- 本节自己的正文字数
  content     TEXT NOT NULL,
  PRIMARY KEY (kb_id, rel_path, section_no)
);

CREATE TABLE warn (             -- 每篇的解析告警（frontmatter 不合法等），见 02
  kb_id TEXT NOT NULL, rel_path TEXT NOT NULL, kind TEXT NOT NULL, message TEXT NOT NULL
);

CREATE TABLE skipped (          -- 整篇跳过的文件 + 当时的 sha1（没变就不必再解析）
  kb_id TEXT NOT NULL, rel_path TEXT NOT NULL, sha1 TEXT NOT NULL, reason TEXT NOT NULL,
  PRIMARY KEY (kb_id, rel_path)
);

CREATE TABLE tombstone (        -- 删掉的 KB / 文件的残留，用于诊断
  kb_id TEXT NOT NULL, rel_path TEXT, deleted_at INTEGER NOT NULL
);
```

`PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;`

> **实现注记（为什么比上面多出几列）**：内存索引要能**只靠 DB 重建**，否则二次启动还是得重新切片，
> 「1000 篇 < 500 ms」不成立。所以 `Entry` 里参与打分的字段（`rev`、五个 search 字段、`toc_text`、`toc`）
> 都得落盘。`warn` / `skipped` 两张表同理：不复用它们，第二次启动会突然少掉告警、
> 并且把空文件重新解析一遍 —— 「第一次 8 条告警、第二次 0 条」比多一张表危险得多。
>
> **`sha1` 与 `rev` 不是一回事**：`sha1` 是文件原始字节的 sha1（判断变没变，早于解码）；
> `rev` 是解码后正文的 sha1 前 8 位（对外可见，见 04）。BOM / CRLF / GB18030 的文件两者不同，
> 不要拿其中一个去顶另一个。

**驱动**：`modernc.org/sqlite`（**纯 Go，无 cgo**）。不用 `mattn/go-sqlite3` —— 它要 cgo，会把「Windows 上开发、一条命令出 macOS/Linux 二进制」这件事直接杀掉。驱动不可用、或传了 `--no-db` 时，**纯内存模式**运行（仍然可用，只是每次启动要重切）。

## 重建（reindex）

```
1. 读取 config，标记 kb.state = "indexing"
2. 遍历文件 → 计算 sha1
3. 卸载：sha1 不在当前清单里的 entry/section 删除
4. 装载：sha1 变化或新增的解析 → 写临时表 → 单事务提交
5. 用新数据整体替换内存索引（原子指针交换，读请求不会被阻塞）
6. 标记 kb.state = "ready"
```

- 索引期间**继续提供旧数据**，`/api/status` 里 `state: "indexing"` + `progress`，UI 显示进度条。
- `sha1` 相同的文件**不做任何解析**。这是「几千篇也要秒级」的关键。
- `--force` 忽略 sha1 全部重切。

### 索引版本漂移

条目的 `rev`（= `sha1` 前 8 位）会随内容变。**给 agent 的响应里带上 `rev`**，这样调用方可以把「我读的是哪一版」记下来。详见 [04-http-api](./04-http-api.md#rev-与漂移)。

## 并发

- 切片是 CPU 密集的。用**固定大小的 worker 池**（`runtime.GOMAXPROCS(0)`）并行解析，但结果按文件清单顺序回填 —— 这样 `search` 的排序在任何机器上都一样。每批 50 个文件检查一次取消信号，避免 reindex 期间 HTTP 请求全部超时。
- 同一时刻只允许一个 reindex 任务（`sync.Mutex` + `atomic` 状态）；重复请求返回 `409` 并附当前进度。
- 读请求只读**不可变的索引快照**（`atomic.Pointer[Index]`）：reindex 完成后整体替换指针，读侧永不加锁。

## 配置变更与迁移

`config.json` 有 `schemaVersion`。升级时用「读旧 → 迁移 → 写新 + 备份 `config.json.bak`」。SQLite 走 `PRAGMA user_version`。

**当心**：DB 可能由新版本写过、被旧版本打开。启动时比对 `user_version` 高于自己能处理的版本 → 拒绝启动并提示升级，不要尝试降级。

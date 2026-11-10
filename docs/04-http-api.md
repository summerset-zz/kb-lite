# 04 · HTTP API

Base：`http://127.0.0.1:7331`（端口可配；端口被占时自动 +1 并把实际端口写回 `config.json`，UI 与 CLI 都从配置读）。

认证分成两级（ADR-015，细节见 [07-security](./07-security.md)）：

> **状态**：这是 **v0.3 的契约**。v0.2 的实现仍然只有一把 token、读取面也必须带（`<配置目录>/token`）。
> 改动顺序见 `docs/09`：先把 `/api/*` 与管理凭据做出来，再切读取面的默认值。不要反过来。

**读取面** `/kb/*`、`/skill.md`、`/authoring.md`。`requireToken` **默认关**（只绑回环地址时），界面里叫“访问密码”。开了之后要带：

```
Authorization: Bearer <读 token>
```

或（给不方便设头的老客户端）

```
X-KB-Token: <读 token>
```

读 token 在 `<配置目录>/read-token`，`kb-lite token --show` 可打印，给用户的只有这一把。
**管理面** `/api/*` **永远**需要 admin 凭据：UI 靠同源页面下发的 `HttpOnly; SameSite=Strict` cookie，
CLI 读 `<配置目录>/api-token` 并带 `X-KB-Admin` 头。`X-KB-Admin` 这条路还要求 `Sec-Fetch-Site: same-origin`
或 `Origin: <自己>`（curl 默认两个都不带 → `403`）；cookie 那条不需要，因为 `SameSite=Strict` 已经保证
跨站请求带不上它。**agent 手里的读取密码打不开管理面。**

通用约定：

- 一律 `Content-Type: application/json; charset=utf-8`，`Cache-Control: no-store`。
- 不支持 CORS，**任何情况都不发 `Access-Control-Allow-*`**。见 07。
- 路径参数里的中文、空格、`#`、`?` 必须由调用方 percent-encode；服务端按段解码。

## 两种错误形态（有意为之）

| 形态 | 用在哪 | 为什么 |
|---|---|---|
| **`4xx` + `{"error":{"code","message"}}`** | 请求本身不合法（凭据不对、KB 不存在、路径越界） | 调用方是程序，需要能分支 |
| **`200` + `{"ok":false,"error":"人话","candidates":[…]}`** | “你查的这个东西不存在，但这是为什么、你可以试这些” | 调用方是**模型**，它需要看到候选才能纠正自己，而不是收到一个异常 |

第二种是本项目的核心设计之一，**不要改成抛异常**。线上版的血泪教训：用 404 回“这一节不存在”时，模型会反复重试同一个调用。

---

## 管理接口 `/api/*`

### `GET /api/status`

```jsonc
{
  "version": "0.3.0",
  "port": 7331,
  "configDir": "C:\\Users\\me\\AppData\\Roaming\\kb-lite",   // config.json / index.db / token 都在这
  "dbPath": "C:\\Users\\me\\AppData\\Roaming\\kb-lite\\index.db", // 只读展示，不可改
  "dbMode": "sqlite",              // "sqlite" | "memory"
  "setupCompleted": true,
  "lanExposed": false,
  "requireToken": false,           // 读取面是否需要 token
  "kbs": [
    { "name": "我的笔记", "id": "k1f3a9c2", "rootPath": "D:\\notes",
      "enabled": true, "state": "ready",       // ready | indexing | error | disabled
      "entries": 128, "sections": 743, "bytes": 2201398,
      "lastIndexedAt": 1762473600000, "warnings": 2,
      "progress": { "done": 120, "total": 530 }   // 仅 state == indexing 时出现
    }
  ]
}
```

**要 admin 凭据**（见上面两级认证）。读取面是否要凭据看 `requireToken`：关着时 `/kb/*` 与 `/skill.md` 直接可用。

`state == "indexing"` 时，`entries/sections` 还是**上一次**索引的数字。调用方据此判断新鲜度（docs/01）。

### `GET /api/kbs` / `POST /api/kbs`

```jsonc
// POST body
{ "name": "我的笔记", "rootPath": "D:\\notes",
  "include": ["**/*.md"], "ignore": ["_drafts"] }
```

校验：`rootPath` 必须存在且是目录；`name` 唯一、长度 1–64、不含 `/` `\` 与控制字符。成功后**立即在后台开始首次 reindex**，返回 `201` + kb 对象（`state: "indexing"`）。

目录里一个 md 都没有 → 仍然创建成功，但响应里带 `"warning": "这个目录里没有找到 markdown 文件"`。

### `PATCH /api/kbs/{name}`

允许改 `name`、`rootPath`、`include`、`ignore`、`enabled`。改 `rootPath` 或 `include/ignore` 会自动触发 reindex。

### `DELETE /api/kbs/{name}`

**只删关联与已入库条目，绝不碰用户的目录。** 返回 `{ "removed": { "entries": 128, "sections": 743 } }`。
UI 上必须二次确认，且确认文案要写明“目录里的文件不会被删除”。

### `POST /api/kbs/{name}/enable` · `POST /api/kbs/{name}/disable`

禁用后该 KB 的 `/kb/{name}/*` 全部返回 `503 {"error":{"code":"kb_disabled"}}`。**索引保留**，重新启用不需要重切。

### `POST /api/kbs/{name}/reindex`

```jsonc
{ "force": false }        // true = 忽略 sha1，全部重切
```

- `202 {"jobId":"...","state":"indexing"}`；
- 已有任务在跑 → `409 {"error":{"code":"indexing","progress":{"done":120,"total":530}}}`。

进度查询：`GET /api/kbs/{name}/index` → `{"state":"indexing","progress":{"done":120,"total":530},"startedAt":…}`。

**索引期间照常提供旧数据**（读到的是上一次的结果），`/api/status` 里的 `state` 是 `indexing`。调用方可以据此判断数据新鲜度。

### `GET /api/fs/list?path=<绝对路径>`

给 UI 的目录选择器用（浏览器拿不到真实路径，只能由 daemon 代劳）。

```jsonc
{ "path": "D:\\notes", "parent": "D:\\",
  "dirs": [ { "name": "ops", "path": "D:\\notes\\ops" } ],
  "drives": ["C:\\", "D:\\"],
  "underHome": false }   // 这个位置在当前用户的用户目录里吗
```

`underHome` 只用来决定界面要不要提醒一句“放在自己用户目录里的资料，换个电脑用户就读不到、索引也更新不了”。
它不是安全判断：真正能不能读由系统权限决定。

每个候选只有名字和路径。**不数里面有多少 markdown**：那是递归走整棵子树，
在大盘根目录上换一层就要等好几秒，而“有几篇”对“选哪个文件夹”帮助有限。

**`path` 可以不给**，那就是“要个起点”：Windows 回盘符列表（`path` 为空、`dirs` 为空），
其它平台回一遍家目录的内容。目录选择器第一次打开时手上没有任何路径，这时回 400 只会让界面空着。

限制：只列目录不列文件内容；跳过 `.` 开头的目录与 `node_modules`；单层最多 500 项；不接受网络路径（`\\server\share`）以外的非本地路径时不做特殊处理，但要求路径可 `realpath`。

### `POST /api/fs/mkdir`

在指定目录下建一个**空文件夹**（目录选择器里的“新建文件夹”）。

```jsonc
{ "path": "D:\\notes", "name": "产品文档" }   // → { "ok": true, "path": "D:\\notes\\产品文档" }
```

- 名字按知识库名字的规则校验（1 到 64 字，不含 `/` `\` 与控制字符，不是 `.` / `..`）；
- 父目录必须存在且是目录；同名已存在 → `409 already_exists`；父目录不在了 → `400 invalid_path`；
  系统层面拒绝的（`CON`、`PRN` 这类保留名、没权限）→ `400 mkdir_failed` 并把系统的话带回去。
- **这是管理面上唯一会写磁盘的接口**，而且只建空目录（ADR-004 的例外）：不写文件、不改内容、不删东西。

### `POST /api/search`

搜索调试页用（`docs/06` §5.3）。**与 `/kb/{name}/search` 是同一套打分**，只是走管理面：
界面手上只有管理凭据，拿不到读取密码。

```jsonc
{ "kb": "笔记", "q": ["上线 回滚"], "limit": 5 }
```

响应与 `GET /kb/{name}/search?format=json&explain=1` **完全一致**（包括 `debug` 里每个关键词
命中在哪个字段），所以两边不会漂。`q` 最多 5 个，每个不超过 200 字。

### `POST /api/preview`

Agent 视角页用（`docs/06` §5.4）：**以 agent 的身份取一次**，把原样响应回给界面。

```jsonc
{ "kind": "skill" }                                   // skill | authoring
{ "kind": "manifest", "kb": "笔记" }
{ "kind": "search",   "kb": "笔记", "q": "部署 回滚" }
{ "kind": "section",  "kb": "笔记", "path": "ops/runbook.md", "n": 1 }
```

响应：

```jsonc
{ "ok": true, "kind": "skill", "url": "http://127.0.0.1:7331/skill.md",
  "status": 200, "contentType": "text/markdown; charset=utf-8", "body": "…" }
```

- 为什么由服务端代取：界面手上只有管理凭据，拿不到读取密码（ADR-015），自己发一定 401。
  代取才能让人看到**真的**响应，包括 `Content-Type`、状态码，以及暂停时的 `503 paused`。
- **必须白名单**：只允许上表那几个 `kind`，`kb` 必须真的存在，`path` 必须是相对路径
  （不以 `/` 开头、不含 `..`）。绝不能变成任意 URL 代理，否则两把钥匙就白分开了。
- `body` 超过 200 KB 截断并回 `"truncated": true`。

### `GET /api/startup` · `POST /api/startup`

开机自启（`docs/06` §5.5）。**目前只有 Windows 支持**（`HKCU\...\CurrentVersion\Run`，
不需要管理员）；其他平台 `supported: false` 并给一句人话解释。

```jsonc
{ "supported": true, "enabled": false,
  "command": "\"C:\\…\\kb-lite.exe\" serve",
  "path": "HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run" }
```

`POST` body：`{ "enabled": true | false }`，返回同样的结构。

- 写进去的命令是 `"<exe>" serve`，**不带 `--open`**：开机自动弹浏览器属于打扰，托盘会自己出现。
- `command` 要原样展示给用户：这条路到底启动了什么，用户有权知道。

### `POST /api/shortcut`

在桌面建一个 `KBLite.lnk`（目标是 `"<exe>" serve --open`），返回它的路径。

- 桌面目录向系统要（`[Environment]::GetFolderPath('Desktop')`），**不能拼 `~/Desktop`**：
  OneDrive 接管桌面后真实路径会变，写死家目录会建到用户看不见的地方。
- 只做 Windows。建 `.lnk` 要走 COM，借用系统自带的 PowerShell 比手搓 `IShellLink` 稳。

### `GET /api/update`

查一次有没有新版本。**这是 kb-lite 唯一会访问外网的地方**，只在用户点按钮时发，
不随启动、不后台定时。

```jsonc
{ "ok": true, "current": "0.3.0", "latest": "v0.4.0", "newer": true,
  "dev": false, "url": "https://github.com/…/releases/tag/v0.4.0",
  "publishedAt": "…", "checkedAt": 1762690000000 }
```

- 数据来源是 GitHub 的 `releases/latest`（匿名，60 次/小时/IP）。结果缓存 10 分钟，
  `?refresh=1` 强制重查，免得用户连点把配额打满。
- **查不到时仍然回 `200`**，只是 `ok:false` 并带一句人话 `error`。
  这是“这次没查到”，不是接口错误，界面照常显示一行提示即可。
- `dev` 为真表示当前跑的是 `-dev` 构建；这种情况下数字版本一样时不说“有更新”。

### `POST /api/settings`
改端口 / 绑定地址 / 读 token 开关 / 向导是否完成。**只有这里能改配置**，UI 与 CLI 都是它的客户端。

```jsonc
{ "port": 7400, "host": "127.0.0.1", "requireToken": true, "setupCompleted": true }
```

**没有 `dbPath`**：记录文件永远是 `<配置目录>/index.db`（ADR-018）。旧客户端带上这个字段
不报错，它只会被忽略。

字段都可选，没传的不动。行为：

- 先校验（端口真 bind 一次 / `host` 非回环时把 `requireToken` 改成 `true`），
  失败 `400` 并指向具体字段（`invalid_port` / `port_in_use`）。**校验全部通过才落盘**，
  中途失败等于什么都没改。
- 落盘 `config.json`（原子写）；
- **访问密码当场生效**：响应回来后 `/kb/*` 就按新开关判定；
- **端口与绑定地址当场重开监听**（“热重开”）：先把新地址真的绑上，成功了再关旧的。
  绑不上就回 `500 reload_failed`，配置回滚，旧地址继续服务 —— 否则用户连「改回去」都做不到。
  响应里回 `"moved": true` 与 `effective.port`，UI 据此提示新地址并跳过去。
  端口被占时沿用「自动 +1」并写回 `config.json`。
- `restartRequired` 只在“热重开不可用”（没有 Reload 回调的嵌入场景）时为真。
  能当场重开的都当场做掉，用户不该为一个能自动完成的事被要求重启。

### `POST /api/auth/token`

```jsonc
{ "action": "show" }              // 把当前读取密码回给 UI（设置页要展示它，好让用户粘给 AI）
{ "action": "rotate" }            // 重新生成读取密码；旧的立即失效
```

两个动作都只碰**读取**密码。它走管理面，所以界面上不需要再输一次口令。
管理凭据（`api-token`）界面上不显示、不可换，也不进任何文档（ADR-015）。

### `POST /api/skill/install`

把两份 skill 之一写到 agent 的目录（`docs/06` 的 SKILL 区块）。

```jsonc
{ "kind": "read" }     // read | authoring，两份**分开装**（ADR-017）
{ "targets": ["dsh", "claude"] }   // 可选；默认只 dsh（~/.agents/skills）
```

- `read` → `~/.agents/skills/kb-lite/SKILL.md`（及 `~/.claude/skills/kb-lite/SKILL.md`）；
- `authoring` → `~/.agents/skills/kb-lite-authoring/SKILL.md`（及 claude 对应目录）；
- 只允许写这两棵子树，不跟随符号链接；写入前把旧文件备份成 `.bak`，失败就还原。
- 返回 `{ "installed": [ { "path": "…", "bytes": 3402 } ] }`。

### `POST /api/pause` · `POST /api/resume`

暂停与恢复**给 AI 用的那一面**（`/kb/*`、`/skill.md`、`/authoring.md`）。

暂停后这些路径一律 `503`：

```jsonc
{ "error": { "code": "paused", "message": "服务已经暂停，AI 现在连不上。界面里点“继续使用”就能恢复。" } }
```

**监听不关，管理面与 UI 照常可用**。这是刻意的：

- UI 要能在暂停时把「继续使用」按钮送到用户眼前（`docs/06` §8.4 的状态文案就是这么写的）；
- `kb-lite start` 就是靠 `/api/resume` 把服务叫醒的。若暂停时连监听都关了，`start`
  根本没有可以说话的通道，`stop` 与 `start` 就不可能对称。

索引、记录文件、进程都保留，恢复不需要重新整理（`docs/06` 第二节）。暂停状态进 `/api/status` 的 `paused`。

### `POST /api/shutdown`

关监听、关记录文件、进程结束。UI 的“退出”按钮与 `kb-lite quit` 用它。

> `stop` 与 `quit` 不是一回事：前者是暂停（进程还在），后者才是退出（`docs/05`）。

---

## 检索接口 `/kb/{name}/*`

这是给 agent 的部分，**响应大小要斤斤计较**。

> `/kb` 单独一个路径**不是接口**：它返回 `404 kb_name_required` 并指向 `/skill.md`。
> 不这么拦的话它会落到 UI 兜底，调错地址的 agent 会拿到一页 HTML 以为接口通了。
> agent 的入口永远是 `/skill.md`。

### `GET /kb/{name}/search`

| 参数 | 说明 |
|---|---|
| `q` | 查询，可重复，最多 5 个；每个 ≤ 200 字符 |
| `limit` | 1–10，默认 5（**合并后**的总数） |
| `format` | `text`（默认，`toc` 是渲染好的文本） / `json`（`toc` 是数组） |
| `explain` | `1` 时附带 `debug.hits`（给 UI 与 CLI，不要给 agent 用） |

响应：

```jsonc
{
  "ok": true,
  "query": ["部署 回滚"],
  "results": [
    {
      "title": "运维手册",
      "path": "ops/runbook.md",       // 读取时原样回传
      "rev": "a1b2c3d4",
      "score": 42,
      "summary": "线上环境的操作手册。",
      "tags": ["ops", "上线"],
      "chars": 4210,
      "toc": "1 上线流程 · 1203 字\n2 回滚 · 空\n  3 快速回滚 · 380 字"
    }
  ]
}
```

`toc` 文本格式（**照抄，不要美化**）：

- 每行 `<编号> <标题><字数后缀>`，行首缩进 = 相对最浅层的层级（2 空格/级）；
- 行首编号就是 `section`，可以直接回灌给 `section` 接口；
- 字数后缀：` · 空`（`chars === 0`）或 ` · 268 字`；拿不到字数时**什么都不写**（"不知道" ≠ "空"）；
- `0`（导言）不在 toc 里。它是单独的节，直接传 `n=0` 读。

> 线上实测：JSON 形式的 toc 占搜索返回字节的 78%，换成“编号 + 标题”的文本后每条约 20 B，模型照样认得出。

### `GET /kb/{name}/manifest`

`?budget=2000&prefix=&depth=3&format=text|json`

按**字符预算**返回分层概览，给 agent 在“不知道去哪找”时用：

```
我的笔记 · 128 篇 / 743 节
▸ ops/         12 篇   3.4k 字
   运维手册     4 节    1.2k 字
▸ guides/      31 篇  22.1k 字
   … 还有 19 篇没列出（budget=2000，调大 ?budget= 可以看更多）
```

- 预算算的是**渲染出来的字符数**（含末尾那句折叠提示），不是字节。实测 `budget=1200` 就是 1200 字以内；
- 目录按条目数降序（同数量按路径升序），条目按 `chars` 降序（同字数按路径升序）；
- 目录层级超过 `depth` 的合并到前 `depth` 段（`depth=1` 时 `docs/en/a.md` 归到 `docs`）；
- 超预算时**优先折叠叶子**：一个目录的条目一旦放不下就整组剩下的折掉（不跳着挑短的，那样列出来的就不再是前缀，渲染会对不齐）；
  连目录行都放不下时整组不出现（`dirs[].shown = false`），只在末尾那一行里计数；
- 条目标题超过 **40 字截断加 `…`**：实测语料里有 frontmatter 没换行导致标题变成一整段的条目，一个就能把预算吃光；
- **默认 `format=text`**：这个接口存在的意义就是便宜，JSON 的括号与键名都是冤枉钱。
  `format=json` 返回 `{ok, name, entries, sections, budget, prefix, depth, folded, dirs, items, text}`。
  `text` 就是默认形态，CLI 与 UI 都用它，**不要各自再写一遍渲染**。
- `budget` / `depth` 传了非法值不报 400，退回默认（一个写错的参数不值得让调用方去处理错误分支）。

### `GET /kb/{name}/list`

`?prefix=ops/&limit=50&after=ops/runbook.md`

分页列出条目（游标是 `path`，与 SQLite 主键同序，不做内存排序）。返回：

```jsonc
{ "ok": true, "items": [ { "title": "运维手册", "path": "ops/runbook.md", "summary": "…", "chars": 4210 } ],
  "hasMore": true, "nextAfter": "ops/runbook.md" }
```

### `GET /kb/{name}/entry/{*path}`

`{*path}` = 从 `/entry/` 之后到结尾的全部内容，按段 percent-decode 后拼成相对路径。
**为了 shell 友好，同时支持** `GET /kb/{name}/entry?path=ops%2Frunbook.md`（当路径里有 `?`/`#` 时用它）。

```jsonc
{
  "title": "运维手册", "path": "ops/runbook.md", "rev": "a1b2c3d4",
  "chars": 4210, "tags": ["ops"], "summary": "…",
  "sections": 4,
  "toc": "1 上线流程 · 1203 字\n2 回滚 · 空\n  3 快速回滚 · 380 字"
}
```

**默认不返回正文**。`?content=1` 才返回 `"content"` 字段（原始 markdown，已去 frontmatter），且上限 200 KB。

### `GET /kb/{name}/entry/{*path}/toc`

只取目录（`{"toc": "...", "sections": 4, "rev": "..."}`）。单独提供是因为“只想看一眼这篇有哪些节”很常见，不值得拖整条记录。

### 读一节的正文

```
GET /kb/{name}/entry/{*path}/section?n=3
GET /kb/{name}/entry/{*path}/section?title=回滚
GET /kb/{name}/entry/{*path}/section?title=帮助:解析器函数#var系列函数
```

| 参数 | 说明 |
|---|---|
| `n` \| `section` | 编号（0 = 导言）。**给了 n 就绝不看 title** |
| `title` | 章节标题（可直接抄 toc 里的文字，不用数编号） |
| `offset` | 从本节正文第几个字符开始（默认 0） |
| `maxChars` | 本次最多返回多少字（默认 `maxSectionChars`，上限 20000） |
| `fallback` | 默认 `1`：本节为空时换第一个有内容的子节 |

成功：

```jsonc
{
  "ok": true, "title": "运维手册", "path": "ops/runbook.md", "rev": "a1b2c3d4",
  "section": 3, "level": 3, "sectionTitle": "快速回滚",
  "sectionPath": ["回滚", "快速回滚"], "pathText": "回滚 / 快速回滚",
  "chars": 380, "content": "回滚到上一个 tag。",
  "note": "第 2 节「回滚」自身没有正文（那是父标题），这里给的是它第一个有内容的子节：第 3 节「快速回滚」的正文。"
}
```

`note` 的三种文案（**照抄，它们直接影响模型行为**）：

> [!NOTE]
> 这三段与下面的 `error` 文案里的角括号是**代码里的原文**（`internal/daemon/notes.go` 用测试逐字锁住）。
> 行文风格要统一到“你那一侧，得等改代码时一起换；不要只改文档，否则 agent 读到的与规格书不一致。

1. 空父节换子节：`第 N 节「X」自身没有正文（那是父标题），这里给的是它第一个有内容的子节：第 M 节「Y」的正文。` + 若还有别的子节：`这一节下面还有 K 个子节，要读别的就按编号再读一次。`
2. 子节也全空：`第 N 节「X」只有标题、没有正文（它下面那 K 个子节也都没有正文）—— 这一块就是没有内容，**别在这一节上重试**。这一页别的节按目录里的编号再读（目录行尾带着每节的字数）。`
3. 超长节被截断：`本节共 4213 字，这里给了前 1500 字。继续读传 offset=1500。`

标题定位失败（**200 + ok:false**）：

```jsonc
{
  "ok": false,
  "error": "《运维手册》里没有标题对得上「部署」。下面是这一页的章节清单，n 只能用 candidates 里的编号（不要自己数）：",
  "candidates": [ { "section": 1, "sectionTitle": "上线流程", "sectionPath": "上线流程", "chars": 1203 } ],
  "sectionCount": 4
}
```

多个同名章节命中时 `error` 换成：`章节标题「外部链接」在《X》里对上了 2 节，没法替你挑（同名章节很常见）。请把 n 换成下面 candidates 里的编号再读一次。`

**只做“一模一样”和“剥掉重名后缀 / 残留转义后一模一样”两档匹配，不做相似度**。归一化规则（照抄）：

1. 百分号解码；
2. 全角字符（U+FF01–FF5E）转半角；
3. U+3000 / U+00A0 / 各种窄空格 → 普通空格；
4. 还原 Markdown 残留转义 `\([\\`*_{}[\]()#+.!~<>|=-])`；
5. 空白折叠、`toLowerCase()`；
6. 再算“紧凑键”= 去掉全部空白；
7. 直连失败时剥掉 `_\d+$` 后缀再比一次。

### `GET /kb/{name}/entry/{*path}/nearby?n=3&radius=1`

取目标节及其前后各 `radius`（目前只允许 1）节。给“答案跨了两个相邻小节”的场景用。

```jsonc
{ "ok": true, "path": "ops/runbook.md", "center": 3, "radius": 1,
  "items": [ { "section": 2, "sectionTitle": "回滚", "content": "" }, { "section": 3, … } ] }
```

### `rev` 与漂移

所有检索/读取响应都带 `rev`（内容 sha1 前 8 位）。调用方把 `rev` 回传给 `section` 接口（`?rev=`）时：

- 一致 → 正常返回；
- 不一致 → 正常返回新内容，但`note` 里加一句 `内容已更新（rev 由 a1b2c3d4 变为 e5f6a7b8），你上次读到的编号可能已经变了，建议先重新看 /toc。`

这条让“读目录 → 读正文”这种两步操作在文件被人改掉时不至于静默读到别的内容。

## `GET /skill.md`

返回给使用侧 agent 的说明文档（`text/markdown`）。内容模板见 [skill-template](./skill-template.md)，
`{{BASE_URL}}`、`{{AUTH}}`、`{{AUTH_NOTE}}`、`{{KB_LIST}}`、`{{KB_SAMPLE}}` 由服务端在渲染时填入。

**token 是条件渲染的**：访问密码关着时，`{{AUTH}}` 渲染成空串，文档里一句鉴权头都不留。
不能只把值换成空而留下 `-H "Authorization: Bearer "`，那会让 agent 以为自己的头写错了。
同理还有 `{{#if MANIFEST}}` 这类能力门控，没实现的东西一律不出现在文档里。

**必须与实现同源**：这段文本里描述的参数名、返回字段、`note` 文案，都要从同一份常量生成，不许手写第二份。加了参数忘了改 skill，比没有 skill 更糟。

## `GET /authoring.md`

与 `/skill.md` 平级的第二份说明（`text/markdown`，同样的条件鉴权）：教 AI 把文件**写成**能进知识库的格式。
两份必须分开（ADR-017）：合成一份，agent 在只想查东西的时候也会去改用户的文件。
UI 的完成页与设置页各给一个“复制”按钮，取的就是这两份全文。

## 其它

- `GET /` → UI 静态页。页面用 `//go:embed` 打进去，**不需要任何凭据**（否则用户连「去设置里关掉密码」都做不到）。
  打开页面时顺手下发 `kb-admin` cookie（`HttpOnly; SameSite=Strict`），UI 之后靠它调 `/api/*`。
  没构建过 UI 时返回一个占位页，说明怎么构建，服务照常可用。
- 访问密码只约束 `/kb/*`、`/skill.md`、`/authoring.md` 这几类**给 agent 的**路径（`isReadPath`）。
- `GET /openapi.json`（可选）：便于用户把接口喂给自己的工具链。
- **不要**提供 `/kb/{name}/raw?path=` 之类无 ACL 的万能文件读接口。

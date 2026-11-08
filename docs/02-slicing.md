# 02 · 切片契约

切片是「一个 markdown 文件 → 一个 Entry + 若干 Section」的过程。**这里是本项目最需要严格遵循的部分**：检索质量、agent 的读取体验、以及跨版本的一致性都建立在它上面。

实现位置：`internal/core/slice.go`。**纯函数**，输入字符串，输出对象，不碰文件系统。

## 文件发现与忽略

```ts
type ScanOptions = {
  include: string[];   // 默认 ["**/*.md", "**/*.markdown", "**/*.txt"]
  ignore: string[];    // 在默认之上叠加
  maxFileBytes: number;// 默认 2 * 1024 * 1024
};
```

默认忽略（**永远忽略，不可通过配置放开**）：

```
**/.git/**
**/node_modules/**
**/.svn/**
**/.hg/**
```

默认忽略（可通过 `ignore` 之外的「取消」机制放开，但 UI 不暴露）：

```
**/.obsidian/**      **/.trash/**      **/.vscode/**
**/.idea/**          **/.DS_Store      **/Thumbs.db
```

**其他以 `.` 开头的目录默认跳过**（`.attachments`、`.templates` 这类不该进知识库）。
但**以 `.` 开头的文件不跳过**（`.env.md`、`.eslintrc.md` 是用户的真实内容）。

限制与告警：

- 单个文件 > `maxFileBytes` → 跳过，并记一条 `warn`（UI 里可看）。
- 软链接：**不跟随目录软链接**（防环），文件软链接跟随。
- 路径统一转 POSIX（`\` → `/`），`rel_path` 是**相对 KB 根目录**的 POSIX 路径，它同时是条目的对外 ID。

## 解码

1. 有 UTF-8 BOM → 去掉。
2. 按 **UTF-8 严格解码**；失败 → 按 **GB18030** 解码（中文用户的 TXT 大概率是这个）。
3. 仍然失败 → 跳过并记 `warn`（`encoding`）。
4. `\r\n` / `\r` 统一成 `\n`。

## Frontmatter

只识别文件**第一行**就是 `---` 的 YAML 块，取以下键（其余忽略，但保留原对象以备将来）：

| 键 | 类型 | 用途 |
|---|---|---|
| `title` | string | 条目标题（最高优先级） |
| `summary` | string | 摘要（人工写的，覆盖自动摘要） |
| `tags` | string[] \| string | 参与「轻命中」打分，见 03 |
| `draft` / `publish: false` | bool | **`true` 则整篇跳过**（用户用 Obsidian 草稿时的约定） |
| `kb_ignore` | bool | `true` 则整篇跳过 |

### 别名

主键缺失时才看别名（主键优先）：

| 别名 | 等价于 | 为什么 |
|---|---|---|
| `name` | `title` | 实体卡片（厂商 / 作者 / 角色）常用 |
| `description` | `summary` | Hugo / VitePress / Nextra / Obsidian 社区的事实标准 |
| `keywords` | `tags` | 同上；常见为逗号分隔字符串 |

> 别名不是「顺手支持」，是实测出来的：fontcover-content 的 217 篇里，161 篇有 `description`、146 篇有 `keywords`、14 篇用 `name`。
> 不支持的话，这些笔记最值钱的两个检索字段（摘要档 + 标签档）**全是空的**，而用户得不到任何提示。
> 逗号串里的每一项都要去掉首尾引号（`"@font-face", src` → `@font-face`、`src`）。

### 宽松回退

严格 YAML 解析失败时，**按行**解析 `^([A-Za-z_][A-Za-z0-9_-]*):\s*(.*)$`：键取冒号前，值取该行剩下部分（去首尾引号）；
缩进行（嵌套对象 / 块序列）一律忽略。

理由：手写 frontmatter 里「值中间出现未加引号的 `:`」非常常见（实测 `in: a TTF`、`involved: neither`），
严格模式会把整篇降级成「没有 frontmatter」，白白丢掉标题与摘要 —— 而这只需要宽容一点就能救回来。

宽松解析也拿不到任何键时，才当真的「解析失败」：正文里那段 `---` 原样保留（不要静默吞掉用户内容）。
**两种情况都记 `warn`**，措辞不同（「不是合法 YAML，已按行宽松解析」/「解析失败，已当作正文保留」）。

> **函数边界**：frontmatter 块在切片**之前**整体剥掉 —— `ParseMarkdownSections()` 收到的
> markdown 不包含它（也可以没有标题行，那就没有 section 0）。跟导言那段 `---` 的“保留”不矛盾：
> 那是「解析失败」的情况，这时根本没有可剥的 frontmatter。

## 标题取值链

```
frontmatter.title
  → 文档第一个非空块**就是** ATX 标题时的那个标题（即「导言为空」）
  → 文件名（去扩展名）
```

注意第 2 条的两个细节：

- 必须是**第一个非空块**，不是「文档里出现的第一个标题」—— 否则一篇以 `# 前言` 开头的长文，条目名会变成「前言」。
- `README.md` / `index.md` / `Untitled.md` 这类**没有信息量的文件名**，若前两条都取不到，则用「父目录名」代替（`docs/api/README.md` → `api`）。文件名白名单见 `LOW_INFORMATION_STEMS`。
  同一张白名单里还有**语言代码**（`en.md` / `zh.md` / `ja.md` …）：`creds/adobe-systems/en.md` 的标题取 `adobe-systems`，
  而不是毫无意义的 `en` —— `<slug>/en.md` + `<slug>/zh.md` 的双语目录是很常见的组织方式。

## 分节算法

```ts
parseMarkdownSections(markdown: string): {
  summary: string;        // 第一个标题之前的原始正文
  sections: Section[];    // 已含 section 0
  toc: TocEntry[];
}
```

逐步：

1. **逐行扫描**，维护「是否在代码块内」的状态：以 ``` 或 ~~~ 开头的围栏行切换状态（围栏内的 `#` 不算标题）。未闭合到文件末尾同样有效。
2. 不在围栏内、且满足下面全部条件的行是标题：
   - **顶格**的 1–6 个 `#`（前导空格会把它变成正文 —— 别照 CommonMark 的「最多 3 空格缩进」放宽，那会让 `  # ` 缩进列表里的伪标题变成真标题）；
   - `#` 后面**至少一个空格或制表符**（`#标题` 不是标题）；
   - 去掉尾部闭合序列后标题**非空**。

   尾部闭合：一串 `#` 只有在**前面是空白**时才算闭合（`## 标题 ##` → `标题`，`## 标题##` → `标题##`，`# C#` → `C#`）。
   ⚠️ 原文这里写的是正则 `^(#{1,6})\s+(.+?)\s*#*\s*$`，它会把 `# C#` 削成 `C` —— 和 CommonMark 不符，实现以本条为准。

   标题文本要清洗：链接留文字、去图片只留 alt、去掉 `` ` ``、去掉当强调用的 `*` / `_`（判据：两侧都是 ASCII 字母数字时保留，否则去掉 —— 这样 `snake_case` 能留下、`**粗体**` 和 `用*斜体*举例` 能清掉）。

   - `title` 清洗后为空（`#`、`# ###`、`## ![](a.png)`）→ 视为普通正文行。
3. 每遇到一个标题就**结算上一节**：把累积的行 `trim()` 成该节 `content`。
4. 维护标题栈：弹出所有 `level >= 当前 level` 的项，然后 push 当前项。`path` = 栈内标题 + 当前标题；`pathText` = `path.join(' / ')`。
   - 第一个标题若是 `h3`（栈为空），`path` 就是 `[title]`，不做补齐。
5. 文件末尾结算最后一节。
6. **编号**：按出现顺序，**全局扁平连续递增**（1, 2, 3 …），**不因层级跳号、不出现 1.1**。这是 agent 读取时唯一需要记的编号空间。
7. **导言**：第一个标题之前那段（`summary`）如果非空，包成 `section: 0`，`title: "导言"`、`level: 1`、`path: ["导言"]`、`pathText: "导言"`。若为空则不存在 section 0。
8. **空节保留**：一个标题下面没有正文时，仍然产生一个 `content: ""` 的节（`chars: 0`）。**不要**把它和子节合并 —— 「父标题无正文、内容在子节里」在真实笔记里非常常见，检索层和读取层各有兜底（见 03 / 04）。

`Section`：

```ts
type Section = {
  section: number;   // 0 = 导言；其余全局扁平连续
  level: number;     // 1..6（section 0 固定为 1）
  title: string;
  path: string[];    // ["上线与回滚", "回滚"]
  pathText: string;  // "上线与回滚 / 回滚"
  content: string;   // 本节**自己的**正文（不含子节）
  chars: number;     // content.trim() 的 **rune 数**（中文一字算 1、emoji 算 1）
};
```

> `chars` 按 **rune** 计，不是 JS 的 `.length`（UTF-16 码元）：否则一个 emoji 算 2、一个生僻字算 2，
> toc 里那行「字数」和 `maxSectionChars` 的截断点都会跟着漂。Go 里就是 `utf8.RuneCountInString`。

`TocEntry` 由 `Section` 派生，供搜索返回使用：

```ts
type TocEntry = { section: number; level: number; title: string; pathText: string; chars: number; long?: boolean };
```

## 超长节

一篇没有标题的 4000 字笔记会变成**一个** 4000 字的节，直接塞进模型上下文。处理方式（**不改编号**）：

- `content.trim().length > maxSectionChars`（默认 **1500**）→ `toc.long = true`。
- 读取接口支持 `?offset=&maxChars=`（见 [04](./04-http-api.md#读一节的正文)）；不传时截断到 `maxSectionChars`，并在响应的 `note` 里说明「本节共 4213 字，这里给了前 1500 字，传 offset=1500 继续」。
- **不要**在切片阶段把长节拆成 3.1 / 3.2 这种子编号。编号一旦不稳定，agent 手上的编号立刻失效。

## 摘要

```
frontmatter.summary
  → 导言里第一段非空、非表格、非代码块的文字（最多 200 字）
  → 空串
```

摘要给人和模型看，所以要洗：

- 去掉所有链接目标（`[文字](url)` → `文字`），图片整段删除；
- 去掉开头连续的表格行（那是从网页/维基转过来的 infobox，当简介毫无意义）；
- 去掉 `[^cite]` 引用角标与 `（未创建）` 这类站点残留；
- 连续 3 个以上换行压成 2 个。

**导言节（section 0）本身不洗** —— 那里是给 agent 读的正文，链接和表格都保留。

## 检索字段的构造

`Entry` 里与检索相关的字段，逐个来源说清楚（打分权重见 03）：

| 字段 | 来源 | 说明 |
|---|---|---|
| `title` | 标题取值链 | 小写化后参与匹配 |
| `tags` | `frontmatter.tags` | 数组，各自独立匹配 |
| `summary` | 摘要 | 只保留这一段，避免大文件无脑霸榜 |
| `tocText` | 所有 `Section` 的 `title + pathText` 拼起来 | 「这一篇里有一节叫这个名字」是很强的信号 |
| `relPath` | 相对路径 | 目录名往往有信息（`docs/api/`） |
| `full` | **整篇正文** | 只在「最弱一档」参与打分，见 03 |

`full` 与 `tocText` 都存小写形式，长度各封顶 **20000** 字符（再长对召回的边际收益为零，而且占内存）。

> 字段名注意：这里的 `full` 就是 [03](./03-search.md) 权重表里的 `full` 档。
> **不要把它叫 `body`** —— 03 的 `body` 指的是「摘要 + 标签」那一档（对应实现里的 `SearchMeta`），
> 两者不是一回事。原文这两处都叫 `body`，是笔误，已按 03 统一。

## 示例

输入 `ops/runbook.md`：

```markdown
---
title: 运维手册
tags: [ops, 上线]
---

线上环境的操作手册。详见[内部文档](https://example.com)。

## 上线流程

1. 打 tag
2. `deploy.sh`

## 回滚

### 快速回滚

回滚到上一个 tag。

## 附录

````markdown
# 这不是标题
````
```

输出：

```jsonc
{
  "title": "运维手册",                 // frontmatter
  "summary": "线上环境的操作手册。详见内部文档。",
  "sections": [
    { "section": 0, "level": 1, "title": "导言", "pathText": "导言", "chars": 40,
      "content": "线上环境的操作手册。详见[内部文档](https://example.com)。" },
    { "section": 1, "level": 2, "title": "上线流程", "pathText": "上线流程", "chars": 23 },
    { "section": 2, "level": 2, "title": "回滚",     "pathText": "回滚",     "chars": 0 },
    { "section": 3, "level": 3, "title": "快速回滚", "pathText": "回滚 / 快速回滚" },
    { "section": 4, "level": 2, "title": "附录",     "pathText": "附录" }
  ]
}
```

注意：`## 回滚` 是空节（`chars: 0`），正文在它的子节里；代码块里的 `# 这不是标题` 没有产生新节。

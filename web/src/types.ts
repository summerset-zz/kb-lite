// 与 daemon 的数据形状对齐（docs/04）。字段名就是线上字段名，别在这里改名。

export interface Progress {
  done: number
  total: number
}

export interface KB {
  name: string
  id: string
  rootPath: string
  enabled: boolean
  state: 'ready' | 'indexing' | 'error' | 'disabled'
  error?: string
  entries: number
  sections: number
  bytes: number
  lastIndexedAt: number
  warnings: number
  /** 这次真的没进知识库的文件篇数（含整篇跳过的与读不了的）。 */
  notIndexed: number
  progress?: Progress
}

/** 一次收录的结果（`GET /api/kbs/{name}/report`，与读取面同一份数据）。 */
export interface KBReport {
  ok: boolean
  name: string
  entries: number
  sections: number
  bytes: number
  /** 毫秒；建了但还没整理过时没有这个字段。 */
  updatedAt?: number
  skippedTotal: number
  warningTotal: number
  /** 服务端封顶 100 条，总数看 `skippedTotal`。 */
  skipped: { path: string; reason: string }[]
  warnings: { kind: string; message: string }[]
}

export interface Status {
  version: string
  port: number
  /** 跑这个服务的系统用户名，第 1 屏用它说“这个目录只有 <名字> 能访问”。 */
  user: string
  /** 本机的局域网 IPv4 地址（可能为空）。 */
  lanAddrs?: string[]
  /** 配置目录：config.json、index.db 与两个 token 文件都在这里。 */
  configDir: string
  /** 记录文件的位置，永远等于 <configDir>/index.db。 */
  dbPath: string
  dbMode: 'sqlite' | 'memory'
  setupCompleted: boolean
  lanExposed: boolean
  requireToken: boolean
  adminAuth: boolean
  /** 暂停时给 AI 的那一面返回 503；界面与 /api/* 照常。 */
  paused: boolean
  kbs: KB[]
}

export interface FsDir {
  name: string
  path: string
}

export interface FsList {
  path: string
  parent?: string
  dirs: FsDir[]
  drives?: string[]
  /** 这个位置在当前用户的用户目录里：换一个电脑用户就读不到，索引也更新不了。 */
  underHome?: boolean
}

/** 安装目标的现状（`GET /api/skill/status`）。 */
export interface SkillState {
  kind: 'read' | 'authoring' | 'maintain'
  target: 'dsh' | 'claude'
  path: string
  installed: boolean
  /** 已装的內容与此刻应该装的是否逐字相同。 */
  upToDate: boolean
  modifiedAt: number
}

/** 添加目录之前的探测结果（`GET /api/fs/probe`）。 */
export interface FsProbe {
  path: string
  isRoot: boolean
  markdownCount: number
  /** 数到上限就停了，count 只能当“至少这么多”看。 */
  capped: boolean
}

export interface SkillTarget {
  kind: string
  target: string
  path: string
  bytes: number
}

// ---- 检索（/api/search 与 /kb/{name}/search 共用同一套形状）----

export interface TocEntry {
  section: number
  level: number
  title: string
  pathText: string
  chars: number
  long?: boolean
}

/** 一个关键词在一条候选上的命中位置（docs/03）。 */
export interface Hit {
  keyword: string
  title: boolean
  path: boolean
  toc: boolean
  leaf: boolean
  body: boolean
  full: boolean
  /** CJK bigram 近似命中：只加分，不算要点。 */
  approx?: boolean
}

/** 一条候选在一个查询下的得分明细。 */
export interface Detail {
  hits: Hit[]
  strong: number
  weak: number
  fullOnly: number
  inTitle: number
  inPath: number
  inToc: number
  inLeaf: number
  approx: number
  score: number
}

export interface SearchResult {
  title: string
  path: string
  rev: string
  score: number
  summary: string
  tags: string[]
  chars: number
  toc: TocEntry[]
}

export interface SearchResponse {
  ok: boolean
  query: string[]
  results: SearchResult[]
  debug?: { path: string; details: Detail[]; hits: Hit[][] }[]
}

/** 服务端代取的一次原始响应（Agent 视角页）。 */
export interface PreviewResponse {
  ok: boolean
  kind: string
  url: string
  status: number
  contentType: string
  body: string
  truncated?: boolean
}

/** 开机自启的状态（`GET /api/startup`）。 */
export interface StartupInfo {
  supported: boolean
  enabled: boolean
  /** 写进系统的启动命令，界面上要给用户看。 */
  command?: string
  path?: string
  error?: string
  /** 注册表里记的 exe（可能与当前这份不同）。 */
  exePath?: string
  /** 自启指向的是另一份 kb-lite。 */
  mismatch?: boolean
  /** 当前 exe 在下载/临时目录里，开自启可能下次开机就失效。 */
  fragile?: boolean
}

/** 桌面快捷方式的状态（`GET /api/shortcut`）。 */
export interface ShortcutInfo {
  supported: boolean
  exists: boolean
  path?: string
  /** .lnk 现在指向哪个 exe。 */
  target?: string
  arguments?: string
  /** 指向另一份 kb-lite：双击会跑起旧版本。 */
  mismatch: boolean
  /** 指向的文件已经不在了。 */
  missing: boolean
  error?: string
}

/** 一条访问记录（`GET /api/logs`）。 */
export interface AccessEntry {
  seq: number
  /** 毫秒时间戳。 */
  at: number
  ip: string
  /** 来自本机（回环地址）。 */
  self: boolean
  /** 反查到的设备名，尽力而为，拿不到就是空。 */
  host?: string
  /** 推断出来的客户端类型。other 时用 ua 展示。 */
  client: 'cli' | 'curl' | 'browser' | 'other' | 'unknown'
  ua?: string
  method: string
  path: string
  status: number
  ms: number
  /** 一句人话。 */
  action: string
}

export interface LogsResponse {
  ok: boolean
  items: AccessEntry[]
  /** 下次请求拿它当 since。 */
  latest: number
  total: number
  /** 环形缓冲覆盖掉的旧条目数。 */
  dropped: number
  max: number
}

/** 检查更新的结果（`GET /api/update`）。查不到时 ok 为假、error 是人话。 */
export interface UpdateInfo {
  ok: boolean
  current: string
  latest?: string
  newer: boolean
  dev: boolean
  url?: string
  publishedAt?: string
  checkedAt: number
  error?: string
}

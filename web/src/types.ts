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
  progress?: Progress
}

export interface Status {
  version: string
  port: number
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

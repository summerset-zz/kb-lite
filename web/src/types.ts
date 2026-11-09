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
  hasMarkdown: boolean
  markdownCount: number
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

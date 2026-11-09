import type { FsList, KB, SearchResponse, SkillTarget, Status } from './types'
import { s } from './strings'

/*
 * 所有网络调用都在这一层。组件里不出现裸 fetch（docs/06 第三节的硬规则）。
 *
 * 管理接口 /api/* 用的是同源页面下发的 HttpOnly cookie（`kb-admin`），
 * 所以这里只需要 `credentials: 'same-origin'`，既不用也拿不到 token。
 */

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message)
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  let resp: Response
  try {
    resp = await fetch(path, {
      method,
      credentials: 'same-origin',
      headers: body === undefined ? undefined : { 'Content-Type': 'application/json' },
      body: body === undefined ? undefined : JSON.stringify(body),
    })
  } catch {
    throw new ApiError(0, 'network', s.errors.daemonDown)
  }

  const text = await resp.text()
  let data: unknown
  try {
    data = text ? JSON.parse(text) : null
  } catch {
    data = null
  }

  if (!resp.ok) {
    const err = (data as { error?: { code?: string; message?: string } } | null)?.error
    throw new ApiError(resp.status, err?.code ?? 'http_error', err?.message ?? s.errors.generic)
  }
  return data as T
}

const q = (params: Record<string, string | number | undefined>) => {
  const sp = new URLSearchParams()
  for (const [k, v] of Object.entries(params)) {
    if (v !== undefined && v !== '') sp.set(k, String(v))
  }
  const out = sp.toString()
  return out ? `?${out}` : ''
}

export const api = {
  status: () => request<Status>('GET', '/api/status'),

  listKBs: () => request<{ ok: boolean; kbs: KB[] }>('GET', '/api/kbs'),

  createKB: (name: string, rootPath: string) =>
    request<KB>('POST', '/api/kbs', { name, rootPath }),

  patchKB: (name: string, patch: Record<string, unknown>) =>
    request<KB>('PATCH', `/api/kbs/${encodeURIComponent(name)}`, patch),

  removeKB: (name: string) =>
    request<{ ok: boolean; removed: { entries: number; sections: number } }>(
      'DELETE',
      `/api/kbs/${encodeURIComponent(name)}`,
    ),

  setEnabled: (name: string, enabled: boolean) =>
    request<KB>('POST', `/api/kbs/${encodeURIComponent(name)}/${enabled ? 'enable' : 'disable'}`),

  reindex: (name: string, force = false) =>
    request<{ ok: boolean; state: string }>(
      'POST',
      `/api/kbs/${encodeURIComponent(name)}/reindex`,
      { force },
    ),

  indexState: (name: string) =>
    request<{ ok: boolean; state: string; error?: string; progress?: { done: number; total: number } }>(
      'GET',
      `/api/kbs/${encodeURIComponent(name)}/index`,
    ),

  fsList: (path: string) => request<FsList>('GET', `/api/fs/list${q({ path })}`),

  /** 搜索调试页用：与 agent 面同一套打分，但走管理面（界面只有管理凭据）。 */
  search: (kb: string, query: string, limit = 5) =>
    request<SearchResponse>('POST', '/api/search', { kb, q: [query], limit }),

  settings: (patch: {
    port?: number
    host?: string
    dbPath?: string
    requireToken?: boolean
    setupCompleted?: boolean
  }) =>
    request<{
      ok: boolean
      restartRequired: boolean
      /** moved 为真表示端口/绑定地址已经当场重开，页面要换到新地址去。 */
      moved: boolean
      effective: { port: number; host: string; requireToken: boolean; dbPath: string }
    }>('POST', '/api/settings', patch),

  installSkill: (kind: 'read' | 'authoring', targets: string[]) =>
    request<{ installed: SkillTarget[] }>('POST', '/api/skill/install', { kind, targets }),

  /** 暂停与恢复只影响给 AI 的那一面；索引与进程都不动。 */
  setPaused: (paused: boolean) =>
    request<{ ok: boolean; paused: boolean; message: string }>(
      'POST',
      paused ? '/api/pause' : '/api/resume',
    ),

  /** 读取密码：show 拿当前那把，rotate 换一把（旧的立刻失效）。 */
  authToken: (action: 'show' | 'rotate') =>
    request<{ ok: boolean; token: string; rotated?: boolean }>('POST', '/api/auth/token', { action }),

  shutdown: () => request<{ ok: boolean }>('POST', '/api/shutdown'),

  /** 取 `/skill.md`（教 AI 怎么查）或 `/authoring.md`（教 AI 怎么写）的原文。 */
  async docText(kind: 'skill' | 'authoring'): Promise<string> {
    const path = kind === 'skill' ? '/skill.md' : '/authoring.md'
    const resp = await fetch(path, { credentials: 'same-origin' })
    if (!resp.ok) throw new ApiError(resp.status, 'doc_failed', s.errors.generic)
    return resp.text()
  },
}

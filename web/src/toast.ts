// 一句提示，浮在页面下方，几秒后自己消失。
//
// 为什么自己写：AGENTS.md 禁止整套组件库（Element/Naive/Ant），而这个东西加起来只有
// 一个队列加一个容器。也**不需要 Provider 包住整棵树**（不像 NMessageProvider）：
// 容器只在 App.vue 里挂一次，任何组件 `useToast()` 就往同一个队列里推。
import { readonly, ref } from 'vue'

export type ToastKind = 'ok' | 'warn' | 'fail'

export interface ToastItem {
  id: number
  kind: ToastKind
  text: string
}

/** 最多同时显示几条：再多就盖住页面了，而且堆在一起没人会读。 */
const MAX_VISIBLE = 3
/** 默认停留多久（毫秒）。够读完一句短句，又不至于挡着下一步操作。 */
const DEFAULT_MS = 3200

const items = ref<ToastItem[]>([])
let seq = 0

function push(kind: ToastKind, text: string, ms: number): number {
  const id = ++seq
  // 同一句话不堆两条：连点两次保存只会看到一句。
  const next = items.value.filter((t) => t.text !== text)
  next.push({ id, kind, text })
  items.value = next.slice(-MAX_VISIBLE)
  if (ms > 0) window.setTimeout(() => dismiss(id), ms)
  return id
}

export function dismiss(id: number) {
  items.value = items.value.filter((t) => t.id !== id)
}

/** 只读队列，给容器渲染用。要加就往 useToast() 推。 */
export const toasts = readonly(items)

export interface Toast {
  ok: (text: string, ms?: number) => number
  warn: (text: string, ms?: number) => number
  /** 失败留得久一点：用户多半要读一下才知道怎么办。 */
  fail: (text: string, ms?: number) => number
  dismiss: (id: number) => void
}

export function useToast(): Toast {
  return {
    ok: (text, ms = DEFAULT_MS) => push('ok', text, ms),
    warn: (text, ms = DEFAULT_MS) => push('warn', text, ms),
    fail: (text, ms = DEFAULT_MS * 2) => push('fail', text, ms),
    dismiss,
  }
}

import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { api, ApiError } from './api'
import { s } from './strings'
import type { Status } from './types'

/*
 * 全站共享一份状态（模块级单例，不是每个组件各拉一次）。
 * 注意：这里在 setup 期间会碰 window，但本应用不跑 SSR，没有影响。
 */

const status = ref<Status | null>(null)
const loading = ref(true)
const error = ref('')

/*
 * 掉线状态：曾经连上过、现在连不上了。
 *
 * 两种场景必须区分开：
 *   1. 首次打开就连不上 → `error`，整页只有一张“重试”卡片；
 *   2. 已经用上了、服务中途没了（从托盘退出、进程崩了、被任务管理器结束）→ `offline`。
 *      页面还停在屏幕上，按钮看起来都是好的，点下去全部会失败。
 *      所以这里不是弹个 toast 就算数，App 会整页换成“已关闭”的提示。
 */
const offline = ref(false)

/** 连续失败几次才认定掉线。一次抖动、一次系统更新网卡都不该糊一层遮罩。 */
const OFFLINE_AFTER = 2
/** 平时的心跳间隔。整理中会缩到 1 秒（要显示进度）。 */
const HEARTBEAT_MS = 5000

let failures = 0
let poller: number | undefined
let holders = 0

async function fetchStatus(silent: boolean) {
  if (!silent) loading.value = true
  try {
    status.value = await api.status()
    error.value = ''
    failures = 0
    offline.value = false
  } catch (e) {
    if (!silent) error.value = e instanceof ApiError ? e.message : s.errors.daemonDown
    // 只有“请求根本发不出去”才算掉线。服务端能回 4xx/5xx 说明它还在，
    // 那是另一种问题（凭据、参数），不该说成“已关闭”。
    if (e instanceof ApiError && e.status === 0) {
      failures += 1
      if (failures >= OFFLINE_AFTER) offline.value = true
    }
  } finally {
    if (!silent) loading.value = false
  }
}

/**
 * 排下一次探测。
 *
 * 这是全站唯一的心跳：页面可见时才发，页面在后台就停（省电，也让日志干净）。
 * 没有它的话，服务一退出，页面会一直停在最后那一帧上，什么都不会发生。
 */
function schedule() {
  window.clearTimeout(poller)
  if (holders === 0 || document.visibilityState !== 'visible') return
  const busy = status.value?.kbs.some((kb) => kb.state === 'indexing') ?? false
  poller = window.setTimeout(() => {
    void refresh(true)
  }, busy ? 1000 : HEARTBEAT_MS)
}

// 切回前台立刻探一次，不用等下一个心跳。模块级只注册一次。
document.addEventListener('visibilitychange', () => {
  if (document.visibilityState === 'visible') void refresh(true)
  else window.clearTimeout(poller)
})

export async function refresh(silent = false) {
  await fetchStatus(silent)
  schedule()
}

export function useStatus() {
  onMounted(() => {
    holders += 1
    if (status.value === null) void refresh()
    else schedule()
  })
  onBeforeUnmount(() => {
    holders -= 1
    if (holders === 0) window.clearTimeout(poller)
  })

  return {
    status,
    loading,
    error,
    offline,
    refresh,
    needWizard: computed(() => status.value !== null && !status.value.setupCompleted),
  }
}

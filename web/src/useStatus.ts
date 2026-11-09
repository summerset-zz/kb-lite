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

let poller: number | undefined
let holders = 0

async function fetchStatus(silent: boolean) {
  if (!silent) loading.value = true
  try {
    status.value = await api.status()
    error.value = ''
  } catch (e) {
    if (!silent) error.value = e instanceof ApiError ? e.message : s.errors.daemonDown
  } finally {
    if (!silent) loading.value = false
  }
}

/** 有知识库还在整理时每秒拉一次；这是全站唯一的轮询。 */
function schedule() {
  window.clearTimeout(poller)
  const busy = status.value?.kbs.some((kb) => kb.state === 'indexing') ?? false
  if (!busy || holders === 0) return
  poller = window.setTimeout(() => {
    void refresh(true)
  }, 1000)
}

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
    refresh,
    needWizard: computed(() => status.value !== null && !status.value.setupCompleted),
  }
}

<script setup lang="ts">
/*
 * 访问日志页（`#logs`，不进导航）。
 *
 * 回答一个问题：AI 到底有没有来查，查了什么。
 * 数据只存在服务端内存里，重启就空；这里每 1.5 秒拉一次增量（`since`），
 * 页面不可见时不拉，切回来立刻补一次。
 */
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { AccessEntry } from '../types'
import { useToast } from '../toast'

const toast = useToast()
const rows = ref<AccessEntry[]>([])
const latest = ref(0)
const dropped = ref(0)
const total = ref(0)
const max = ref(0)
const error = ref('')

/** 界面上最多留多少行：再多浏览器就开始卡，而没人会往上翻几百条。 */
const MAX_ROWS = 500

let timer: number | undefined

async function pull() {
  try {
    const resp = await api.logs(latest.value)
    if (resp.items.length > 0) {
      // 新的放最上面：看日志的人关心的是刚才那一下。
      rows.value = [...resp.items].reverse().concat(rows.value).slice(0, MAX_ROWS)
    }
    latest.value = resp.latest
    dropped.value = resp.dropped
    total.value = resp.total
    max.value = resp.max
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  }
}

onMounted(async () => {
  await pull()
  timer = window.setInterval(() => {
    // 页面在后台时不拉：用户看不见，白费请求，还会把日志页自己刷成常客。
    if (document.visibilityState === 'visible') void pull()
  }, 1500)
  document.addEventListener('visibilitychange', onVisible)
})

onBeforeUnmount(() => {
  window.clearInterval(timer)
  document.removeEventListener('visibilitychange', onVisible)
})

function onVisible() {
  if (document.visibilityState === 'visible') void pull()
}

async function clearAll() {
  try {
    await api.clearLogs()
    rows.value = []
    dropped.value = 0
    total.value = 0
    toast.ok(s.logs.cleared)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  }
}

function clock(at: number): string {
  return new Date(at).toLocaleTimeString('zh-CN', { hour12: false })
}

function who(e: AccessEntry): string {
  if (e.self) return s.logs.self
  return e.host || e.ip
}

// client 是 other 时显示原始 UA，写日志的人才知道那是什么程序。
function clientLabel(e: AccessEntry): string {
  return s.logs.client[e.client] ?? e.client
}

function isError(e: AccessEntry): boolean {
  return e.status >= 400
}
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 sm:px-6 py-6">
    <div class="flex items-center gap-2 flex-wrap">
      <h1 class="font-medium flex-1">{{ s.logs.title }}</h1>
      <button class="btn btn-sm" type="button" @click="clearAll">{{ s.logs.clear }}</button>
    </div>
    <p class="muted text-sm mt-1">{{ s.logs.hint }}</p>
    <!-- 用户明确要求这一句要在界面上露出来：日志里有搜索词，先说清它不出本机。 -->
    <p class="note mt-3">{{ s.logs.privacy }}</p>

    <p v-if="error" class="note text-sm mt-3">{{ error }}</p>

    <p v-if="rows.length === 0 && !error" class="card p-6 text-center mt-4 text-sm">
      {{ s.logs.empty }}
    </p>

    <div v-else-if="rows.length > 0" class="card mt-4 divide-y divide-[var(--border)]">
      <div v-for="e in rows" :key="e.seq" class="px-3 py-2 flex items-baseline gap-2 text-xs">
        <span class="mono muted shrink-0">{{ clock(e.at) }}</span>
        <span class="shrink-0" :title="e.ip">
          {{ who(e) }}
          <span v-if="!e.self && e.host" class="muted mono">({{ e.ip }})</span>
        </span>
        <span class="muted shrink-0" :title="e.ua">{{ clientLabel(e) }}</span>
        <span class="flex-1 min-w-0 truncate" :title="e.action">{{ e.action }}</span>
        <span
          class="mono shrink-0"
          :style="{ color: isError(e) ? 'var(--danger)' : 'var(--text-muted)' }"
          :title="`${e.status} · ${e.ms} 毫秒`"
        >
          {{ e.status }}
        </span>
      </div>
    </div>

    <p v-if="dropped > 0" class="muted text-xs mt-3">
      {{ s.logs.dropped(dropped) }}
    </p>
    <p v-if="max > 0" class="muted text-xs mt-1">{{ s.logs.more }} {{ max }} 条（现在 {{ total }} 条）。</p>
  </div>
</template>

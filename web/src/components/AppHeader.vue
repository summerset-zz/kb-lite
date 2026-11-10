<script setup lang="ts">
import { computed } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { Status } from '../types'
import { useMenu } from '../menu'
import { useStatus } from '../useStatus'
import { useTheme } from '../theme'
import { useToast } from '../toast'
import AppIcon from './AppIcon.vue'

const props = defineProps<{ status: Status }>()
// 'help' 与 'agent' 都不在导航里：它们走 #hash，模型类型必须认得这两个值。
const tab = defineModel<'kbs' | 'search' | 'agent' | 'settings' | 'about' | 'help'>('tab', {
  required: true,
})

const menu = useMenu()
const { theme, toggle: toggleTheme } = useTheme()
const { refresh } = useStatus()
const toast = useToast()

const base = computed(() => `http://${location.hostname}:${props.status.port}`)

// 服务状态：一眼看得出 AI 现在连得上还是连不上，以及这条服务对谁可见。
const running = computed(() => !props.status.paused)
const stateText = computed(() =>
  running.value
    ? s.service.running(props.status.port, props.status.lanExposed ? s.service.lanMode : s.service.localMode)
    : s.service.stopped,
)

async function setPaused(paused: boolean) {
  menu.close()
  try {
    await api.setPaused(paused)
    toast.ok(paused ? s.service.stoppedToast : s.service.startedToast)
    await refresh()
  } catch (e) {
    toast.fail(e instanceof ApiError ? e.message : s.errors.generic)
  }
}

// 模板里只能访问组件实例上的东西，`navigator` 得包一层。
async function copyBase() {
  try {
    await navigator.clipboard.writeText(base.value)
    toast.ok(s.common.copied)
  } catch {
    toast.fail('没复制成。手动选中复制吧。')
  }
}

const tabs = [
  { id: 'kbs' as const, label: s.nav.kbs },
  { id: 'help' as const, label: s.nav.help },
  { id: 'search' as const, label: s.nav.search },
  // “给 AI 看”暂时不上导航：那一页是给排查问题的人看的，日常用不到。
  // 组件与路由（#agent）都留着，以后要用直接加回来。
  { id: 'settings' as const, label: s.nav.settings },
  { id: 'about' as const, label: s.nav.about },
]
</script>

<template>
  <header class="border-b" :style="{ borderColor: 'var(--border)' }">
    <div class="mx-auto max-w-3xl px-4 sm:px-6">
      <div class="flex items-center gap-2 h-14">
        <span class="font-semibold tracking-tight">KBLite</span>
        <span class="muted text-sm hidden sm:inline">by Summerset-zz</span>
        <span class="flex-1" />

        <!-- 服务状态：文字 + 下拉。放在顶栏是因为它是全站状态，
             藏在设置页深处时，用户根本不知道 AI 现在是连不上还是没在用。 -->
        <div class="relative">
          <button
            class="btn btn-ghost btn-sm"
            type="button"
            :title="base"
            @click.stop="menu.toggle('service')"
          >
            <span
              class="h-2 w-2 rounded-full shrink-0"
              :style="{ background: running ? 'var(--accent)' : 'var(--text-muted)' }"
            />
            <span class="ml-1.5">{{ stateText }}</span>
            <AppIcon name="chevron-down" class="ml-1 muted" />
          </button>
          <div
            v-if="menu.isOpen('service')"
            class="card absolute right-0 mt-1 z-20 p-1 w-44"
            @click.stop
          >
            <button class="menu-item" type="button" :disabled="running" @click="setPaused(false)">
              {{ s.service.start }}
            </button>
            <button class="menu-item" type="button" :disabled="!running" @click="setPaused(true)">
              {{ s.service.stop }}
            </button>
            <button class="menu-item" type="button" @click="copyBase(); menu.close()">
              {{ s.service.copyAddress }}
            </button>
          </div>
        </div>

        <button
          class="btn btn-ghost btn-sm"
          type="button"
          :title="theme === 'dark' ? s.service.themeToLight : s.service.themeToDark"
          @click="toggleTheme"
        >
          <AppIcon :name="theme === 'dark' ? 'sun' : 'moon'" />
        </button>
      </div>

      <!-- 五个页签在 320px 宽的窗口里放不下：允许横向滚，不靠缩字号、不靠删页签。 -->
      <nav class="flex gap-1 -mb-px overflow-x-auto">
        <button
          v-for="t in tabs"
          :key="t.id"
          type="button"
          class="px-3 py-2 text-sm border-b-2 bg-transparent cursor-pointer whitespace-nowrap shrink-0"
          :style="{
            borderColor: tab === t.id ? 'var(--accent)' : 'transparent',
            color: tab === t.id ? 'var(--text)' : 'var(--text-muted)',
            fontWeight: tab === t.id ? 600 : 400,
          }"
          @click="tab = t.id"
        >
          {{ t.label }}
        </button>
      </nav>
    </div>
  </header>
</template>

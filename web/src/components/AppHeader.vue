<script setup lang="ts">
import { computed, ref } from 'vue'
import { s } from '../strings'
import type { Status } from '../types'

const props = defineProps<{ status: Status }>()
const tab = defineModel<'kbs' | 'search' | 'settings' | 'about'>('tab', { required: true })

const base = computed(() => `http://${location.hostname}:${props.status.port}`)

// 模板里只能访问组件实例上的东西，`navigator` 得包一层。
const copied = ref(false)
async function copyBase() {
  try {
    await navigator.clipboard.writeText(base.value)
    copied.value = true
    window.setTimeout(() => (copied.value = false), 1600)
  } catch {
    copied.value = false
  }
}

const tabs = [
  { id: 'kbs' as const, label: s.nav.kbs },
  { id: 'search' as const, label: s.nav.search },
  { id: 'settings' as const, label: s.nav.settings },
  { id: 'about' as const, label: s.nav.about },
]
</script>

<template>
  <header class="border-b" :style="{ borderColor: 'var(--border)' }">
    <div class="mx-auto max-w-3xl px-4 sm:px-6">
      <div class="flex items-center gap-3 h-14">
        <span class="font-semibold tracking-tight">KBLite</span>
        <span class="muted text-sm hidden sm:inline">by Summerset-zz</span>
        <span class="flex-1" />
        <button
          class="btn btn-ghost btn-sm mono"
          type="button"
          :title="s.common.copy"
          @click="copyBase"
        >
          {{ copied ? s.common.copied : base }}
        </button>
      </div>
      <nav class="flex gap-1 -mb-px">
        <button
          v-for="t in tabs"
          :key="t.id"
          type="button"
          class="px-3 py-2 text-sm border-b-2 bg-transparent cursor-pointer"
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

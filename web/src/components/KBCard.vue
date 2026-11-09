<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { KB } from '../types'
import { humanBytes, humanTime } from '../format'
import ModalShell from './ModalShell.vue'

const props = defineProps<{ kb: KB }>()
const emit = defineEmits<{ changed: [] }>()

const busy = ref('')
const error = ref('')

const renameOpen = ref(false)
const renameTo = ref(props.kb.name)
const removeOpen = ref(false)

const statusLabel = computed(() => {
  if (props.kb.state === 'indexing') return s.kbs.indexing
  if (props.kb.state === 'error') return s.kbs.failed
  if (!props.kb.enabled) return s.kbs.disabled
  return s.kbs.ready
})

const dot = computed(() => {
  if (props.kb.state === 'indexing') return 'var(--accent)'
  if (props.kb.state === 'error') return 'var(--danger)'
  if (!props.kb.enabled) return 'var(--text-muted)'
  return 'var(--accent)'
})

const progress = computed(() => {
  const p = props.kb.progress
  if (!p || p.total === 0) return ''
  return `正在整理 ${p.done} / ${p.total}`
})

async function run(label: string, fn: () => Promise<unknown>) {
  busy.value = label
  error.value = ''
  try {
    await fn()
    emit('changed')
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}

const toggle = () =>
  run('toggle', () => api.setEnabled(props.kb.name, !props.kb.enabled))

const update = () => run('update', () => api.reindex(props.kb.name, false))
const updateAll = () => run('force', () => api.reindex(props.kb.name, true))

async function doRename() {
  const name = renameTo.value.trim()
  if (!name || name === props.kb.name) {
    renameOpen.value = false
    return
  }
  await run('rename', () => api.patchKB(props.kb.name, { name }))
  renameOpen.value = false
}

async function doRemove() {
  await run('remove', () => api.removeKB(props.kb.name))
  removeOpen.value = false
}
</script>

<template>
  <div class="card p-4">
    <div class="flex items-start gap-3 flex-wrap">
      <div class="min-w-0 flex-1">
        <div class="flex items-center gap-2">
          <span class="h-2 w-2 rounded-full shrink-0" :style="{ background: dot }" />
          <span class="font-medium truncate">{{ kb.name }}</span>
          <span class="muted text-xs shrink-0">{{ statusLabel }}</span>
        </div>
        <p class="mono text-xs muted mt-1 break-all">{{ kb.rootPath }}</p>
        <p class="text-sm mt-1">
          {{ kb.entries }} {{ s.kbs.entries }} · {{ kb.sections }} {{ s.kbs.sections }} ·
          {{ humanBytes(kb.bytes) }}
        </p>
        <p class="text-xs muted mt-1">
          {{ s.kbs.addedAt }}：{{ kb.lastIndexedAt ? humanTime(kb.lastIndexedAt) : s.kbs.never }}
          <span v-if="kb.warnings"> · ⚠ {{ kb.warnings }} {{ s.kbs.skipped }}</span>
        </p>
        <p v-if="progress" class="text-xs mt-1" :style="{ color: 'var(--accent)' }">{{ progress }}</p>
        <p v-if="kb.state === 'indexing'" class="text-xs muted mt-1">{{ s.kbs.indexingHint }}</p>
      </div>

      <details class="relative shrink-0">
        <summary class="btn btn-sm list-none">操作 ▾</summary>
        <div class="card absolute right-0 mt-1 z-10 p-1 w-40">
          <button class="menu-item" type="button" :disabled="busy !== ''" @click="update">
            {{ s.kbs.update }}
          </button>
          <button class="menu-item" type="button" :disabled="busy !== ''" @click="updateAll">
            {{ s.kbs.updateAll }}
          </button>
          <button class="menu-item" type="button" :disabled="busy !== ''" @click="toggle">
            {{ kb.enabled ? s.kbs.disable : s.kbs.enable }}
          </button>
          <button
            class="menu-item"
            type="button"
            :disabled="busy !== ''"
            @click="renameTo = kb.name; renameOpen = true"
          >
            {{ s.kbs.rename }}
          </button>
          <button
            class="menu-item"
            type="button"
            :style="{ color: 'var(--danger)' }"
            :disabled="busy !== ''"
            @click="removeOpen = true"
          >
            {{ s.kbs.remove }}
          </button>
        </div>
      </details>
    </div>

    <p v-if="kb.error" class="note mt-3 text-sm">{{ s.kbs.failed }}：{{ kb.error }}</p>
    <p v-if="error" class="note mt-2 text-sm">{{ error }}</p>

    <ModalShell :open="renameOpen" :title="s.kbs.rename" @close="renameOpen = false">
      <input v-model="renameTo" class="input w-full" @keyup.enter="doRename" />
      <template #footer>
        <button class="btn" type="button" @click="renameOpen = false">{{ s.common.cancel }}</button>
        <button class="btn btn-primary" type="button" :disabled="busy !== ''" @click="doRename">
          {{ s.common.save }}
        </button>
      </template>
    </ModalShell>

    <ModalShell :open="removeOpen" :title="`要把 ${kb.name} 从列表里移除吗？`" @close="removeOpen = false">
      <p class="text-sm">只会删掉我们自己的登记信息和整理结果。</p>
      <p class="text-sm font-medium mt-2 mono break-all">{{ kb.rootPath }} 里的文件一个都不会动。</p>
      <template #footer>
        <button class="btn btn-ghost" type="button" @click="removeOpen = false">算了</button>
        <button class="btn btn-danger" type="button" :disabled="busy !== ''" @click="doRemove">
          {{ s.kbs.remove }}
        </button>
      </template>
    </ModalShell>
  </div>
</template>

<style scoped>
summary::-webkit-details-marker {
  display: none;
}
.menu-item {
  display: block;
  width: 100%;
  text-align: left;
  padding: 6px 10px;
  border-radius: 6px;
  font-size: 14px;
  background: transparent;
  border: 0;
  color: inherit;
  cursor: pointer;
}
.menu-item:hover:not(:disabled) {
  background: var(--surface-2);
}
.menu-item:disabled {
  opacity: 0.5;
  cursor: not-allowed;
}
</style>

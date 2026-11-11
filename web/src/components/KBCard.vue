<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, ApiError } from '../api'
import { copyPath, revealPath } from '../actions'
import { s } from '../strings'
import type { KB, KBReport } from '../types'
import { humanBytes, humanTime } from '../format'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'
import SplitButton from './SplitButton.vue'

const props = defineProps<{ kb: KB }>()
const emit = defineEmits<{ changed: [] }>()

const menuId = `kb-${props.kb.id}`

const busy = ref('')
const error = ref('')

const renameOpen = ref(false)
const renameTo = ref(props.kb.name)
const removeOpen = ref(false)

const statusLabel = computed(() => {
  if (props.kb.state === 'indexing') return s.kbs.indexing
  if (props.kb.state === 'error') return s.kbs.failed
  if (!props.kb.enabled) return s.kbs.disabled
  // 从没整理过：既不是“已就绪”也不是“0 篇文档没事”，而是“还没动手”。
  // 用户刚往目录里放了一堆文件、或者让 AI 写完，界面却说“已就绪 0 篇”，只会让人以为坏了。
  if (!props.kb.lastIndexedAt) return s.kbs.neverIndexed
  // 目录里有对不上的文件：别说“已就绪”，用户会以为新写的东西已经能查了。
  if (props.kb.pending > 0) return s.kbs.pending(props.kb.pending)
  // 有文件没进库时，就不说“已就绪”了：AI 确实查不到那几篇。
  if (props.kb.notIndexed > 0) return s.kbs.statusNotIndexed(props.kb.notIndexed)
  return s.kbs.statusReady(humanTime(props.kb.lastIndexedAt))
})

// 行首那个符号：对号 = 没问题，感叹号 = 要看看，时钟 = 正在整理。
const stateIcon = computed<'clock' | 'alert-circle' | 'check'>(() => {
  if (props.kb.state === 'indexing') return 'clock'
  if (!props.kb.lastIndexedAt) return 'alert-circle'
  if (props.kb.pending > 0) return 'alert-circle'
  if (props.kb.state === 'error' || props.kb.notIndexed > 0) return 'alert-circle'
  if (!props.kb.enabled) return 'alert-circle'
  return 'check'
})

const dot = computed(() => {
  if (props.kb.state === 'indexing') return 'var(--accent-bright)'
  if (props.kb.state === 'error') return 'var(--danger)'
  if (!props.kb.enabled) return 'var(--text-muted)'
  if (props.kb.notIndexed > 0) return 'var(--danger)'
  return 'var(--accent-bright)'
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

// ---- 入库日志 ----
// 看点是“这次到底进去了什么、漏了什么”。以前这些数字只在 /api/status 里有一个总数，
// 而“有几个文件没被收录”的名册只有 agent 能看到，用户反而看不到。
const logOpen = ref(false)
const report = ref<KBReport | null>(null)
const logError = ref('')

async function openLog() {
  logOpen.value = true
  report.value = null
  logError.value = ''
  try {
    report.value = await api.kbReport(props.kb.name)
  } catch (e) {
    logError.value = e instanceof ApiError ? e.message : s.errors.generic
  }
}

/** 跳过原因：能翻译的翻译，翻译不了的（比如编码失败那句原文）原样显示。 */
function skipReason(reason: string): string {
  return s.kbs.skipReason[reason] ?? reason
}
</script>

<template>
  <div class="card p-4">
    <!-- 一行的样子：`[√] 字体内容 已就绪 8 分钟前更新 —— [操作|↓]`（docs/06 §5.2） -->
    <div class="flex items-center gap-2 min-w-0">
      <AppIcon :name="stateIcon" :style="{ color: dot }" />
      <span class="font-medium truncate">{{ kb.name }}</span>
      <span class="text-xs shrink-0" :style="{ color: dot }">{{ statusLabel }}</span>
      <span class="flex-1" />
      <SplitButton
        :id="menuId"
        :label="s.kbs.actions"
        menu-only
        :disabled="busy !== ''"
      >
        <template #default="{ close }">
          <button class="menu-item" type="button" :disabled="busy !== ''" @click="close(); update()">
            {{ s.kbs.update }}
          </button>
          <button
            class="menu-item"
            type="button"
            :disabled="busy !== ''"
            @click="close(); updateAll()"
          >
            {{ s.kbs.updateAll }}
          </button>
          <button class="menu-item" type="button" @click="close(); openLog()">
            {{ s.kbs.showLog }}
          </button>
          <button class="menu-item" type="button" @click="close(); revealPath(kb.rootPath)">
            {{ s.common.reveal }}
          </button>
          <button class="menu-item" type="button" @click="close(); copyPath(kb.rootPath)">
            {{ s.common.copyPath }}
          </button>
          <button
            class="menu-item"
            type="button"
            :disabled="busy !== ''"
            @click="close(); toggle()"
          >
            {{ kb.enabled ? s.kbs.disable : s.kbs.enable }}
          </button>
          <button
            class="menu-item"
            type="button"
            :disabled="busy !== ''"
            @click="close(); renameTo = kb.name; renameOpen = true"
          >
            {{ s.kbs.rename }}
          </button>
          <button
            class="menu-item danger"
            type="button"
            :disabled="busy !== ''"
            @click="close(); removeOpen = true"
          >
            {{ s.kbs.remove }}
          </button>
        </template>
      </SplitButton>
    </div>

    <p class="text-sm mt-1 ml-6">
      {{ s.kbs.docCount(kb.entries) }} | {{ s.kbs.sectionCount(kb.sections) }} |
      {{ humanBytes(kb.bytes) }}
    </p>
    <p v-if="progress" class="text-xs mt-1 ml-6" :style="{ color: 'var(--accent)' }">
      {{ progress }}
    </p>
    <p v-if="kb.state === 'indexing'" class="text-xs muted mt-1 ml-6">
      {{ s.kbs.indexingHint }}
    </p>

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
    <ModalShell :open="logOpen" :title="s.kbs.logTitle(kb.name)" wide @close="logOpen = false">
      <p v-if="logError" class="note text-sm">{{ logError }}</p>
      <p v-else-if="!report" class="muted text-sm">{{ s.common.loading }}</p>
      <template v-else>
        <p v-if="!report.updatedAt" class="text-sm">{{ s.kbs.logNever }}</p>
        <template v-else>
          <p class="text-sm">{{ s.kbs.logSummary(report.entries, report.sections) }}</p>
          <p v-if="report.skippedTotal === 0 && report.warningTotal === 0" class="muted text-sm mt-2">
            {{ s.kbs.logClean }}
          </p>

          <template v-if="report.skippedTotal > 0">
            <p class="text-sm mt-3">{{ s.kbs.logSkipped(report.skippedTotal) }}</p>
            <ul class="mt-1 space-y-1">
              <li v-for="it in report.skipped" :key="it.path" class="text-sm">
                <span class="mono break-all">{{ it.path }}</span>
                <span class="muted">：{{ skipReason(it.reason) }}</span>
              </li>
            </ul>
          </template>

          <template v-if="report.warningTotal > 0">
            <p class="text-sm mt-3">{{ s.kbs.logWarnings(report.warningTotal) }}</p>
            <ul class="mt-1 space-y-1">
              <li v-for="(w, i) in report.warnings" :key="i" class="text-sm">{{ w.message }}</li>
            </ul>
          </template>

          <p
            v-if="
              report.skippedTotal > report.skipped.length ||
              report.warningTotal > report.warnings.length
            "
            class="muted text-xs mt-2"
          >
            {{ s.kbs.logCapped }}
          </p>
        </template>
      </template>
    </ModalShell>
  </div>
</template>

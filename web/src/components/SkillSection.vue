<script setup lang="ts">
/*
 * 安装 SKILL。上下两组：检索（教 AI 怎么读）与整理（教 AI 怎么写）。
 *
 * 每组下面直接列目标目录的**现状**（未安装 / 已是最新 / 有更新），按钮就在那一行上。
 * 以前按钮在卡片顶上、结果写在下边一段文字里，用户点完之后既不知道成没成，
 * 也不知道之前装没装过。状态是从磁盘读的（`GET /api/skill/status`），不是记在内存里。
 */
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { SkillState } from '../types'
import { humanTime } from '../format'
import { useToast } from '../toast'
import CopyInline from './CopyInline.vue'

const toast = useToast()
const items = ref<SkillState[]>([])
const busy = ref('')
const error = ref('')

const groups = [
  {
    kind: 'read' as const,
    title: s.skill.readTitle,
    desc: s.skill.readDesc,
    prompt: s.skill.promptRead,
    promptHint: s.skill.promptReadHint,
  },
  {
    kind: 'authoring' as const,
    title: s.skill.writeTitle,
    desc: s.skill.writeDesc,
    prompt: s.skill.promptAuthoring,
    promptHint: s.skill.promptAuthoringHint,
  },
]

const targets = [
  { id: 'dsh' as const, label: s.skill.targetAgents },
  { id: 'claude' as const, label: s.skill.targetClaude },
]

async function load() {
  try {
    items.value = (await api.skillStatus()).items
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  }
}
onMounted(load)

function stateOf(kind: string, target: string): SkillState | undefined {
  return items.value.find((i) => i.kind === kind && i.target === target)
}

function stateText(st?: SkillState): string {
  if (!st?.installed) return s.skill.stateMissing
  if (!st.upToDate) return s.skill.stateStale
  return s.skill.stateFresh(st.modifiedAt ? humanTime(st.modifiedAt) : '')
}

function stateColor(st?: SkillState): string {
  if (!st?.installed) return 'var(--text-muted)'
  if (!st.upToDate) return 'var(--danger)'
  return 'var(--accent)'
}

function buttonText(st?: SkillState): string {
  if (!st?.installed) return s.skill.install
  if (!st.upToDate) return s.skill.update
  return s.skill.reinstall
}

async function install(kind: 'read' | 'authoring', target: 'dsh' | 'claude') {
  busy.value = `${kind}/${target}`
  error.value = ''
  try {
    const resp = await api.installSkill(kind, [target])
    const path = resp.installed[0]?.path ?? ''
    toast.ok(s.skill.installedToast(path))
    await load()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}

// 有任何一个目标落后，就在组标题旁边点一下，省得用户逐个看。
const stale = computed(() => items.value.some((i) => !i.upToDate))
</script>

<template>
  <div class="space-y-3">
    <section v-for="g in groups" :key="g.kind" class="card p-4">
      <p class="font-medium">{{ g.title }}</p>
      <p class="muted text-xs mt-1">{{ g.desc }}</p>

      <div class="flex items-start gap-2 mt-3">
        <span class="text-xs muted shrink-0">{{ s.skill.howToAsk }}</span>
        <span class="mono text-xs flex-1 min-w-0 break-all">{{ g.prompt }}</span>
        <CopyInline :text="g.prompt" />
      </div>

      <div class="mt-3 space-y-2">
        <div v-for="t in targets" :key="t.id" class="flex items-center gap-2 flex-wrap">
          <span class="text-xs shrink-0 w-20">{{ t.label }}</span>
          <span class="mono text-xs muted flex-1 min-w-0 break-all">
            {{ stateOf(g.kind, t.id)?.path ?? '' }}
          </span>
          <span class="text-xs shrink-0" :style="{ color: stateColor(stateOf(g.kind, t.id)) }">
            {{ stateText(stateOf(g.kind, t.id)) }}
          </span>
          <button
            class="btn btn-sm shrink-0"
            :class="{ 'btn-primary': !stateOf(g.kind, t.id)?.upToDate }"
            type="button"
            :disabled="busy !== ''"
            @click="install(g.kind, t.id)"
          >
            {{ busy === `${g.kind}/${t.id}` ? s.common.loading : buttonText(stateOf(g.kind, t.id)) }}
          </button>
        </div>
      </div>
    </section>

    <p v-if="stale" class="note text-sm">{{ s.skill.staleHint }}</p>
    <p v-else class="muted text-xs">{{ s.skill.whenStale }}</p>
    <p class="muted text-xs">{{ s.skill.reloadHint }}</p>
    <p v-if="error" class="note text-sm">{{ error }}</p>
  </div>
</template>

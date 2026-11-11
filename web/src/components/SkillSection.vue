<script setup lang="ts">
/*
 * 安装 SKILL。上下两组：检索（教 AI 怎么读）与整理（教 AI 怎么写）。
 *
 * 每组下面直接列目标目录的**现状**（未安装 / 已是最新 / 有更新），按钮就在那一行上。
 * 状态是从磁盘读的（`GET /api/skill/status`），不是记在内存里。
 *
 * 一行的样子（docs/06 §5.2）：
 *   [√] 通用目录  3 分钟前更新          —— [重新安装|↓]
 *   [!] 通用目录  SKILL 未安装          —— [安装|↓]
 *
 * 路径不铺在行上了：用户不需要天天看 `~/.claude/skills/...`。
 * 需要的时候从右边那个下拉里「复制路径」或「打开文件位置」。
 */
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '../api'
import { copyPath, revealPath } from '../actions'
import { s } from '../strings'
import type { SkillState, Status } from '../types'
import { humanTime } from '../format'
import { useToast } from '../toast'
import AppIcon from './AppIcon.vue'
import CopyInline from './CopyInline.vue'
import SplitButton from './SplitButton.vue'

const props = defineProps<{ status: Status }>()

const toast = useToast()
const items = ref<SkillState[]>([])
const busy = ref('')
const error = ref('')
// 访问密码只在开着的时候用得上。取一次就够，装完之后它不会变。
const token = ref('')

const base = computed(() => `http://${location.hostname}:${props.status.port}`)

/** “不想安装”时贴给 AI 的那段话：带地址，开着密码就一起带上。 */
function pasteFor(kind: 'read' | 'authoring' | 'maintain'): string {
  if (kind === 'authoring') return s.skill.pasteAuthoring(base.value, token.value)
  // 维护那一组没有这一行（模板里按 g.shell 判断），这个分支只是为了类型完整。
  if (kind === 'maintain') return ''
  return s.skill.pasteRead(base.value, token.value)
}

const groups = [
  {
    kind: 'read' as const,
    title: s.skill.readTitle,
    desc: s.skill.readDesc,
    prompt: s.skill.promptRead,
    shell: '',
  },
  {
    kind: 'authoring' as const,
    title: s.skill.writeTitle,
    desc: s.skill.writeDesc,
    prompt: s.skill.promptAuthoring,
    shell: '',
  },
  {
    kind: 'maintain' as const,
    title: s.skill.maintainTitle,
    desc: s.skill.maintainDesc,
    prompt: s.skill.promptMaintain,
    // 这一组靠命令行干活，所以没有“贴给 AI 也行”那条退路：没有 shell 就是做不到。
    // 这条要求写在这一组卡片的靠上位置，不在正文里藏。
    shell: s.skill.maintainShell,
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

async function loadToken() {
  if (!props.status.requireToken) return
  try {
    token.value = (await api.authToken('show')).token
  } catch {
    // 拿不到就不写密码那一句：让用户自己发现也没关系，不要因此把整块弄成错误。
    token.value = ''
  }
}

onMounted(() => {
  void load()
  void loadToken()
})

function stateOf(kind: string, target: string): SkillState | undefined {
  return items.value.find((i) => i.kind === kind && i.target === target)
}

function stateText(st?: SkillState): string {
  if (!st?.installed) return s.skill.stateMissing
  if (!st.upToDate) return s.skill.stateStale
  // “3 分钟前更新”比“已是最新 · 3 分钟前”短，而且一眼看得出这次的状态。
  return s.skill.stateFresh(st.modifiedAt ? humanTime(st.modifiedAt).replace(/\s/g, '') : '')
}

/** 未安装与有更新都要动作，这两种状态配感叹号；已是最新配对号。 */
function needsAction(st?: SkillState): boolean {
  return !st?.installed || !st.upToDate
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

async function install(kind: 'read' | 'authoring' | 'maintain', target: 'dsh' | 'claude') {
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

// 有任何一个目标落后，就在三张卡片上面说一句，省得用户逐个看。
const stale = computed(() => items.value.some((i) => !i.upToDate))
</script>

<template>
  <div class="space-y-3">
    <!-- 落后提示放在卡片上方：用户看到的第一眼就知道“这一块要动手了”。
         以前它挂在最底下，滚过三张卡片才看得见。 -->
    <p v-if="stale" class="note text-sm">{{ s.skill.needInstall }}</p>

    <section v-for="g in groups" :key="g.kind" class="card p-4">
      <!-- 1. 名称 + 2. 简介 -->
      <p class="font-medium">{{ g.title }}</p>
      <p class="muted text-xs mt-1">{{ g.desc }}</p>

      <!-- 3. 对 agent / harness 的要求（只有维护那一组有） -->
      <p v-if="g.shell" class="note text-xs mt-2">{{ g.shell }}</p>

      <!-- 4. 安装行：左边是哪，右边是现状与按钮 -->
      <div class="mt-3 space-y-2">
        <div v-for="t in targets" :key="t.id" class="flex items-center gap-2 min-w-0">
          <AppIcon
            :name="needsAction(stateOf(g.kind, t.id)) ? 'alert-circle' : 'check'"
            :style="{ color: stateColor(stateOf(g.kind, t.id)) }"
          />
          <span class="text-sm shrink-0">{{ t.label }}</span>
          <span class="flex-1" />
          <span
            class="text-xs shrink-0 truncate"
            :style="{ color: stateColor(stateOf(g.kind, t.id)) }"
          >
            {{ stateText(stateOf(g.kind, t.id)) }}
          </span>
          <SplitButton
            :id="`skill-${g.kind}-${t.id}`"
            :label="
              busy === `${g.kind}/${t.id}` ? s.common.loading : buttonText(stateOf(g.kind, t.id))
            "
            :primary="needsAction(stateOf(g.kind, t.id))"
            :disabled="busy !== ''"
            @action="install(g.kind, t.id)"
          >
            <template #default="{ close }">
              <button
                class="menu-item"
                type="button"
                @click="close(); copyPath(stateOf(g.kind, t.id)?.path ?? '')"
              >
                {{ s.common.copyPath }}
              </button>
              <!-- 没装的时候那个文件还不存在，打开一个不存在的路径只会报错。 -->
              <button
                v-if="stateOf(g.kind, t.id)?.installed"
                class="menu-item"
                type="button"
                @click="close(); revealPath(stateOf(g.kind, t.id)?.path ?? '')"
              >
                {{ s.common.reveal }}
              </button>
            </template>
          </SplitButton>
        </div>
      </div>

      <!-- 5. 示例：一句能直接粘走的话，不加前缀 -->
      <div class="flex items-start gap-2 mt-4">
        <span class="mono text-xs flex-1 min-w-0 break-all">{{ g.prompt }}</span>
        <CopyInline :text="g.prompt" />
      </div>

      <!-- 6. 不装 SKILL 的用法：这一段本身就是“说明书 + 地址”，贴给任何 AI 都能用。
           维护那一组没有这一行：没有 shell 就是做不到。 -->
      <div v-if="!g.shell" class="flex items-start gap-2 mt-2">
        <span class="text-xs muted shrink-0">{{ s.skill.pasteHint }}</span>
        <span class="mono text-xs flex-1 min-w-0 break-all">{{ pasteFor(g.kind) }}</span>
        <CopyInline :text="pasteFor(g.kind)" />
      </div>
    </section>

    <p class="muted text-xs">{{ s.skill.whenStale }}</p>
    <p class="muted text-xs">{{ s.skill.reloadHint }}</p>
    <p v-if="error" class="note text-sm">{{ error }}</p>
  </div>
</template>

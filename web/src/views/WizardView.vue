<script setup lang="ts">
/*
 * 首次启动向导，四屏：
 *   0 欢迎 + 东西放在哪（只告知，不给选，见 ADR-018）
 *   1 端口与接口公开范围
 *   2 创建知识库（选目录、起名字、添加都在弹窗里完成）
 *   3 总结（配置项 + 网络 + 知识库 + 装 SKILL）
 *
 * 四屏里有三屏是“读一眼、点下一步”，只有第二屏真的改东西。这是故意的：
 * 目标用户第一次打开时最需要知道的是“它把我东西放哪了”和“别人能不能看到”，
 * 而不是被要求做一串技术决定。
 */
import { computed, ref, watch } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { Status } from '../types'
import { useStatus } from '../useStatus'
import DirPicker from '../components/DirPicker.vue'
import CopyBlock from '../components/CopyBlock.vue'
import SkillSection from '../components/SkillSection.vue'
import AppIcon from '../components/AppIcon.vue'

const props = defineProps<{ status: Status }>()
const emit = defineEmits<{ done: [] }>()
const { status: shared, refresh } = useStatus()

const step = ref(0)
const busy = ref(false)
const error = ref('')

// ---- 第 0 屏 ----
const configDir = computed(() => props.status.configDir)
const copied = ref('')
async function copy(text: string, key: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = key
    window.setTimeout(() => {
      if (copied.value === key) copied.value = ''
    }, 1600)
  } catch {
    error.value = '没复制成。手动选中复制吧。'
  }
}

// ---- 第 1 屏 ----
const port = ref(props.status.port)
const lan = ref(props.status.lanExposed)
const shownPort = computed(() => (port.value > 0 && port.value < 65536 ? port.value : 7331))
const localUrls = computed(
  () => `http://127.0.0.1:${shownPort.value} 或 http://localhost:${shownPort.value}`,
)
const localUrl = computed(() => `http://127.0.0.1:${shownPort.value}`)
// 拿不到局域网地址时不编一个：界面上写“这台机器的局域网地址”，让用户自己看。
const lanUrl = computed(() => {
  const ip = props.status.lanAddrs?.[0]
  return ip ? `http://${ip}:${shownPort.value}` : `http://这台机器的局域网地址:${shownPort.value}`
})

// ---- 第 2 屏 ----
const pickerOpen = ref(false)
const addBusy = ref(false)
const addError = ref('')
const existing = computed(() => props.status.kbs ?? [])

const canNext = computed(() => {
  if (step.value === 1) return port.value > 0 && port.value < 65536
  return true
})

// 在弹窗里点“添加”之后走这里。失败就留在弹窗里，改个名字就能重试。
async function addKB(path: string, name: string) {
  addBusy.value = true
  addError.value = ''
  try {
    await api.createKB(name, path)
    pickerOpen.value = false
    await refresh()
  } catch (e) {
    addError.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    addBusy.value = false
  }
}

function openPicker() {
  addError.value = ''
  pickerOpen.value = true
}

async function finish() {
  busy.value = true
  error.value = ''
  try {
    // 顺序有讲究：
    //  1. 知识库在这之前已经建好了（第 2 屏的弹窗里点“添加”就建了）；
    //  2. 这里再把端口、范围和「向导已完成」一起提交。
    //     如果端口真的换了，服务会当场上线新地址、旧地址立刻失效，
    //     所以这一次提交必须是**最后一次**对旧地址的调用，之后直接跳过去。
    const resp = await api.settings({
      port: port.value,
      host: lan.value ? '0.0.0.0' : '127.0.0.1',
      requireToken: lan.value ? true : undefined,
      setupCompleted: true,
    })

    if (resp.moved && resp.effective.port !== Number(location.port)) {
      location.replace(`http://${location.hostname}:${resp.effective.port}/`)
      return
    }
    port.value = resp.effective.port
    await refresh()
    // 服务端已经收下了，本地也立刻记住“向导做完了”：不看紧着那次刷新的结果，
    // 否则它拿到旧数据就会把刚填完的用户扔回第一屏。
    if (shared.value) shared.value.setupCompleted = true
    step.value = 3
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = false
  }
}

// ---- 第 3 屏（总结）----

const token = ref('')
const skillText = ref('')
const authoringText = ref('')

// 走到总结页才去取：两份说明全文 + （开了密码时）当前访问密码。
watch(step, async (v) => {
  if (v !== 3) return
  try {
    const [read, write] = await Promise.all([api.docText('skill'), api.docText('authoring')])
    skillText.value = read
    authoringText.value = write
  } catch {
    error.value = '两份说明没读出来。刷新一下页面再试。'
  }
  if (lan.value) {
    try {
      token.value = (await api.authToken('show')).token
    } catch {
      token.value = ''
    }
  }
})

const base = computed(() => `http://${location.hostname}:${shownPort.value}`)
const needsAuth = computed(() => lan.value || props.status.requireToken)

const cmdText = computed(
  () =>
    `curl -s "${base.value}/skill.md"` +
    (needsAuth.value ? ' \\\n  -H "Authorization: Bearer <访问密码>"' : ''),
)

// 说明书地址。有些工具支持“填一个网址当资料源”，那就填这一行。
//
// 不要把它包装成 MCP 配置：这个地址返回的是 markdown，不是 MCP 服务端点，
// 任何 MCP 客户端连上来都会握手失败。MCP 将来只做“转发到本地 HTTP”的薄壳（ADR-006）。
const skillUrl = computed(() => `${base.value}/skill.md`)
</script>

<template>
  <div class="flex-1">
    <div class="mx-auto max-w-2xl px-4 sm:px-6 py-10">
      <p class="text-xs muted tracking-widest uppercase">KBLite</p>
      <h1 class="text-xl font-semibold mt-1">
        {{ step === 0 ? s.wizard.welcome(status.version) : s.wizard.title }}
      </h1>

      <div v-if="step < 3" class="flex items-center gap-2 mt-5">
        <span
          v-for="i in 3"
          :key="i"
          class="h-1 rounded-full flex-1"
          :style="{ background: i - 1 <= step ? 'var(--accent)' : 'var(--border)' }"
        />
      </div>

      <!-- 第 0 屏：欢迎 + 东西放在哪 -->
      <section v-if="step === 0" class="mt-6">
        <p class="muted text-sm">{{ s.wizard.welcomeHint }}</p>

        <p class="text-sm mt-5">{{ s.wizard.configWhere }}</p>
        <div class="card p-3 mt-2 flex items-start gap-2">
          <pre class="preview flex-1 min-w-0">{{ configDir }}</pre>
          <button class="btn btn-sm shrink-0" type="button" @click="copy(configDir, 'dir')">
            <AppIcon :name="copied === 'dir' ? 'check' : 'copy'" />
            <span class="ml-1">{{ copied === 'dir' ? s.common.copied : s.common.copy }}</span>
          </button>
        </div>

        <p class="muted text-xs mt-2">{{ s.wizard.configOwner(status.user) }}</p>
      </section>

      <!-- 第 1 屏：端口与接口公开范围 -->
      <section v-else-if="step === 1" class="mt-6">
        <h2 class="font-medium">{{ s.wizard.step2 }}</h2>

        <div class="mt-4 flex items-center gap-3 flex-wrap">
          <label class="text-sm w-16" for="wizard-port">{{ s.wizard.port }}</label>
          <input
            id="wizard-port"
            v-model.number="port"
            class="input w-28 mono"
            type="number"
            min="1"
            max="65535"
          />
        </div>
        <p class="muted text-xs mt-1">{{ s.wizard.portHint }}</p>

        <p class="text-sm mt-5 mb-2">{{ s.wizard.whoCanOpen }}</p>
        <div class="space-y-2">
          <label class="radio-row" :data-checked="!lan">
            <input v-model="lan" type="radio" :value="false" class="mt-1" />
            <span class="min-w-0">
              <span class="block">{{ s.wizard.loopback }}</span>
              <span class="block muted text-xs mt-1">{{ s.wizard.localHint(localUrls) }}</span>
            </span>
          </label>

          <label class="radio-row" :data-checked="lan">
            <input v-model="lan" type="radio" :value="true" class="mt-1" />
            <span class="min-w-0">
              <span class="block">{{ s.wizard.lan }}</span>
              <span v-if="lan" class="block muted text-xs mt-1">{{ s.wizard.lanHint(lanUrl) }}</span>
            </span>
          </label>
        </div>

        <div v-if="lan" class="note mt-3 text-sm space-y-1">
          <p>{{ s.wizard.lanVpn }}</p>
          <p>{{ s.wizard.lanPassword }}</p>
        </div>
      </section>

      <!-- 第 2 屏：创建知识库 -->
      <section v-else-if="step === 2" class="mt-6">
        <h2 class="font-medium">{{ s.wizard.step3 }}</h2>
        <p class="muted text-sm mt-1">{{ s.wizard.step3Hint }}</p>
        <p class="muted text-sm mt-1">{{ s.wizard.step3HintMore }}</p>

        <template v-if="existing.length">
          <p class="text-sm mt-5">{{ s.wizard.existing }}</p>
          <ul class="mt-2 space-y-2">
            <li v-for="kb in existing" :key="kb.id" class="card px-3 py-2">
              <p class="text-sm">{{ kb.name }}</p>
              <p class="mono text-xs muted break-all mt-0.5">{{ kb.rootPath }}</p>
            </li>
          </ul>
        </template>
        <p v-else class="note mt-4 text-sm">{{ s.wizard.needKB }}</p>

        <button class="btn btn-primary mt-4" type="button" @click="openPicker">
          {{ s.wizard.addDir }}
        </button>
      </section>

      <!-- 第 3 屏：总结 -->
      <section v-else class="mt-6">
        <h2 class="font-medium">{{ s.wizard.summary }}</h2>
        <p class="muted text-sm mt-1">{{ s.wizard.summaryHint }}</p>

        <div class="card p-4 mt-4 space-y-4 text-sm">
          <div>
            <p class="font-medium">{{ s.wizard.sumConfig }}</p>
            <p class="mono text-xs muted mt-1 break-all">{{ status.configDir }}</p>
          </div>

          <div>
            <p class="font-medium">{{ s.wizard.sumNetwork }}</p>
            <p v-if="!lan" class="muted text-xs mt-1">{{ s.wizard.sumLocal(localUrl) }}</p>
            <template v-else>
              <p class="muted text-xs mt-1">{{ s.wizard.sumLan(lanUrl) }}</p>
              <p class="text-xs mt-2">{{ s.wizard.sumPassword }}</p>
              <pre v-if="token" class="preview mt-1">{{ token }}</pre>
              <p v-else class="muted text-xs mt-1">
                密码在设置页里能看到，也可以在那里换一个。
              </p>
            </template>
          </div>

          <div>
            <p class="font-medium">{{ s.wizard.sumKB }}</p>
            <ul v-if="existing.length" class="mt-1 space-y-2">
              <li v-for="kb in existing" :key="kb.id">
                <p class="text-xs">{{ kb.name }}</p>
                <p class="mono text-xs muted break-all">{{ kb.rootPath }}</p>
              </li>
            </ul>
            <p v-else class="muted text-xs mt-1">{{ s.wizard.sumKBEmpty }}</p>
          </div>
        </div>

        <!-- SKILL 不自动装：写进用户 home 的目录是显式动作，而且装了也要重启 AI 工具 -->
        <p class="font-medium mt-6">{{ s.wizard.sumSkill }}</p>
        <p class="muted text-xs mt-1">{{ s.wizard.sumSkillHint }}</p>
        <div class="mt-3">
          <SkillSection :status="status" />
        </div>

        <details class="mt-5">
          <summary class="text-sm cursor-pointer">{{ s.wizard.aiStuff }}</summary>
          <p class="note mt-3 text-sm">{{ s.agentNote }}</p>
          <div class="mt-3 grid grid-cols-1 gap-3">
            <CopyBlock
              :label="s.wizard.copySkill"
              :hint="s.wizard.copySkillHint"
              :text="skillText"
            />
            <CopyBlock
              :label="s.wizard.copySkillUrl"
              :hint="s.wizard.copySkillUrlHint"
              :text="skillUrl"
            />
            <CopyBlock
              :label="s.wizard.copyAuthoring"
              :hint="s.wizard.copyAuthoringHint"
              :text="authoringText"
            />
            <CopyBlock :label="s.wizard.copyCurl" :hint="s.wizard.copyCurlHint" :text="cmdText" />
          </div>
        </details>
      </section>

      <p v-if="error" class="note mt-4 text-sm">{{ error }}</p>

      <DirPicker
        :open="pickerOpen"
        ask-name
        :submitting="addBusy"
        :submit-error="addError"
        @pick="addKB"
        @cancel="pickerOpen = false"
      />

      <div class="flex items-center gap-2 mt-6">
        <button v-if="step > 0 && step < 3" class="btn btn-ghost" type="button" @click="step -= 1">
          {{ s.common.back }}
        </button>
        <span class="flex-1" />
        <button
          v-if="step < 2"
          class="btn btn-primary"
          type="button"
          :disabled="!canNext"
          @click="step += 1"
        >
          {{ s.common.next }}
        </button>
        <button
          v-else-if="step === 2"
          class="btn btn-primary"
          type="button"
          :disabled="busy || !canNext"
          @click="finish"
        >
          {{ busy ? s.common.loading : s.common.next }}
        </button>
        <button v-else class="btn btn-primary" type="button" @click="emit('done')">
          {{ s.common.start }}
        </button>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { Status } from '../types'
import { useStatus } from '../useStatus'
import DirPicker from '../components/DirPicker.vue'
import CopyBlock from '../components/CopyBlock.vue'

const props = defineProps<{ status: Status }>()
const emit = defineEmits<{ done: [] }>()
const { refresh } = useStatus()

const step = ref(0)
const busy = ref(false)
const error = ref('')

// 第 1 步：记录文件位置
const defaultDB = props.status.dbPath
const dbCustom = ref(false)
const dbPath = ref(props.status.dbPath)

// 第 2 步：端口与访问范围
const port = ref(props.status.port)
const lan = ref(props.status.lanExposed)

// 第 3 步：知识库。点“添加”开弹窗，在弹窗里选文件夹、起名字、确认 —— 确认完就真的建好了。
const pickerOpen = ref(false)
const addBusy = ref(false)
const addError = ref('')

// 已经加好的知识库：重新运行向导的人第一件事就是确认它们还在。
const existing = computed(() => props.status.kbs ?? [])

const canNext = computed(() => {
  if (step.value === 0) return dbCustom.value ? dbPath.value.trim().length > 0 : true
  if (step.value === 1) return port.value > 0 && port.value < 65536
  // 第三步不强制有知识库：万一个目录都加不上（权限不对之类），用户也得能走完向导，
  // 否则就卡死在这一屏，连设置页都进不去。
  return true
})

// 在弹窗里点“添加这个知识库”之后走这里。失败就留在弹窗里（文案就显示在按钮上方），
// 用户改个名字就能重试，不用重新选一遍文件夹。
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
    //  1. 知识库在这之前已经建好了（第 3 步的弹窗里点“添加”就建了）；
    //  2. 这里再把端口、范围、记录文件位置和「向导已完成」一起提交。
    //     如果端口真的换了，服务会当场上线新地址、旧地址立刻失效，
    //     所以这一次提交必须是**最后一次**对旧地址的调用，之后直接跳过去。
    const resp = await api.settings({
      port: port.value,
      host: lan.value ? '0.0.0.0' : '127.0.0.1',
      dbPath: dbCustom.value ? dbPath.value.trim() : undefined,
      requireToken: lan.value ? true : undefined,
      setupCompleted: true,
    })

    if (resp.moved && resp.effective.port !== Number(location.port)) {
      const target = `http://${location.hostname}:${resp.effective.port}/`
      location.replace(target)
      return
    }
    port.value = resp.effective.port
    step.value = 3
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = false
  }
}

// ---- 完成页的四段“给 AI 用的东西”：先露内容，再给复制（docs/06 §5.1 ⁄ §8.5）----

const skillText = ref('')
const authoringText = ref('')

// 走到完成页才去取两份文档，不然第一步就多两个请求。
watch(step, async (v) => {
  if (v !== 3) return
  try {
    const [read, write] = await Promise.all([api.docText('skill'), api.docText('authoring')])
    skillText.value = read
    authoringText.value = write
  } catch {
    error.value = '两份说明没读出来。刷新一下页面再试。'
  }
})

const base = computed(() => `http://${location.hostname}:${port.value}`)
const needsAuth = computed(() => props.status.requireToken || lan.value)

const cmdText = computed(
  () =>
    `curl -s "${base.value}/skill.md"` +
    (needsAuth.value ? ' \\\n  -H "Authorization: Bearer <访问密码>"' : ''),
)

// MCP 的工具描述就是这个地址：多数客户端读完它就自己会调。
const mcpText = computed(() =>
  JSON.stringify(
    { mcpServers: { 'kb-lite': { type: 'http', url: `${base.value}/skill.md` } } },
    null,
    2,
  ),
)
</script>

<template>
  <div class="flex-1">
    <div class="mx-auto max-w-2xl px-4 sm:px-6 py-10">
      <p class="text-xs muted tracking-widest uppercase">KBLite</p>
      <h1 class="text-xl font-semibold mt-1">{{ s.wizard.title }}</h1>

      <div v-if="step < 3" class="flex items-center gap-2 mt-5">
        <span
          v-for="i in 3"
          :key="i"
          class="h-1 rounded-full flex-1"
          :style="{ background: i - 1 <= step ? 'var(--accent)' : 'var(--border)' }"
        />
      </div>

      <section v-if="step === 0" class="mt-6">
        <h2 class="font-medium">{{ s.wizard.step1 }}</h2>
        <p class="muted text-sm mt-1">{{ s.wizard.step1Hint }}</p>

        <div class="mt-4 space-y-2">
          <label class="radio-row" :data-checked="!dbCustom">
            <input v-model="dbCustom" type="radio" :value="false" class="mt-1" />
            <span class="min-w-0">
              <span class="block">{{ s.wizard.defaultLocation }}</span>
              <span class="block mono muted truncate">{{ defaultDB }}</span>
            </span>
          </label>

          <label class="radio-row" :data-checked="dbCustom">
            <input v-model="dbCustom" type="radio" :value="true" class="mt-1" />
            <span class="min-w-0 flex-1">
              <span class="block">{{ s.wizard.customLocation }}</span>
              <input
                v-if="dbCustom"
                v-model="dbPath"
                class="input w-full mt-2 mono"
                spellcheck="false"
                placeholder="D:\kb-lite\index.db"
              />
            </span>
          </label>
        </div>
      </section>

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
          <span class="muted text-sm">{{ s.wizard.portHint }}</span>
        </div>

        <p class="text-sm mt-5 mb-2">{{ s.wizard.whoCanOpen }}</p>
        <div class="space-y-2">
          <label class="radio-row" :data-checked="!lan">
            <input v-model="lan" type="radio" :value="false" class="mt-1" />
            <span>{{ s.wizard.loopback }}</span>
          </label>
          <label class="radio-row" :data-checked="lan">
            <input v-model="lan" type="radio" :value="true" class="mt-1" />
            <span>{{ s.wizard.lan }}</span>
          </label>
        </div>

        <p v-if="lan" class="note mt-3 text-sm">{{ s.wizard.lanWarn }}</p>
      </section>

      <section v-else-if="step === 2" class="mt-6">
        <h2 class="font-medium">{{ existing.length ? s.wizard.step3Again : s.wizard.step3 }}</h2>
        <p class="muted text-sm mt-1">
          {{ existing.length ? s.wizard.step3AgainHint : s.wizard.pickFolderHint }}
        </p>

        <ul v-if="existing.length" class="mt-4 space-y-2">
          <li v-for="kb in existing" :key="kb.id" class="card px-3 py-2">
            <p class="text-sm">{{ kb.name }}</p>
            <p class="mono text-xs muted break-all mt-0.5">{{ kb.rootPath }}</p>
          </li>
        </ul>

        <p v-if="!existing.length" class="note mt-4 text-sm">{{ s.wizard.needKB }}</p>

        <!-- 这里是唯一的“添加”入口：选文件夹、起名字、确认都在弹窗里完成。
             以前名字留在页面上，于是“选文件夹”和“真的建一个”看起来是两件事。 -->
        <button class="btn btn-primary mt-4" type="button" @click="openPicker">
          {{ s.wizard.addKB }}
        </button>
      </section>

      <section v-else class="mt-6">
        <h2 class="font-medium">{{ s.wizard.done }}</h2>
        <p class="muted text-sm mt-1">{{ s.wizard.doneHint }}</p>

        <p class="note mt-4 text-sm">{{ s.agentNote }}</p>

        <div class="mt-3 grid grid-cols-1 gap-3">
          <CopyBlock
            :label="s.wizard.copySkill"
            :hint="s.wizard.copySkillHint"
            :text="skillText"
          />
          <CopyBlock :label="s.wizard.copyMcp" :hint="s.wizard.copyMcpHint" :text="mcpText" />
          <CopyBlock
            :label="s.wizard.copyAuthoring"
            :hint="s.wizard.copyAuthoringHint"
            :text="authoringText"
          />
          <CopyBlock :label="s.wizard.copyCurl" :hint="s.wizard.copyCurlHint" :text="cmdText" />
        </div>
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
          {{ busy ? s.common.loading : s.common.start }}
        </button>
        <button v-else class="btn btn-primary" type="button" @click="emit('done')">
          {{ s.common.start }}
        </button>
      </div>
    </div>
  </div>
</template>

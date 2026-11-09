<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { Status } from '../types'
import DirPicker from '../components/DirPicker.vue'

const props = defineProps<{ status: Status }>()
const emit = defineEmits<{ done: [] }>()

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

// 第 3 步：第一个知识库
const pickerOpen = ref(false)
const kbPath = ref('')
const kbName = ref('')

const canNext = computed(() => {
  if (step.value === 0) return dbCustom.value ? dbPath.value.trim().length > 0 : true
  if (step.value === 1) return port.value > 0 && port.value < 65536
  return true
})

function onPick(path: string) {
  pickerOpen.value = false
  kbPath.value = path
  if (!kbName.value) kbName.value = path.split(/[\\/]/).filter(Boolean).pop() ?? '知识库'
}

async function finish() {
  busy.value = true
  error.value = ''
  try {
    // 先落配置（端口、范围、记录文件位置），再建第一个知识库，最后标记向导已完成。
    await api.settings({
      port: port.value,
      host: lan.value ? '0.0.0.0' : '127.0.0.1',
      dbPath: dbCustom.value ? dbPath.value.trim() : undefined,
      requireToken: lan.value ? true : undefined,
    })
    if (kbPath.value) await api.createKB(kbName.value.trim(), kbPath.value)
    await api.settings({ setupCompleted: true })
    step.value = 3
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = false
  }
}

// ---- 完成页的四个“复制”：只复制，不下载文件（docs/06 §5.1）----

const copied = ref('')
const copyError = ref('')

async function copy(kind: 'cmd' | 'skill' | 'mcp' | 'authoring') {
  copyError.value = ''
  const base = `http://${location.hostname}:${port.value}`
  try {
    let text: string
    if (kind === 'cmd') {
      const auth =
        props.status.requireToken || lan.value ? ' \\\n  -H "Authorization: Bearer <访问密码>"' : ''
      text = `curl -s "${base}/kb"${auth}`
    } else if (kind === 'mcp') {
      text = JSON.stringify(
        { mcpServers: { 'kb-lite': { type: 'http', url: `${base}/skill.md` } } },
        null,
        2,
      )
    } else {
      text = await api.docText(kind)
    }
    await navigator.clipboard.writeText(text)
    copied.value = kind
    window.setTimeout(() => {
      if (copied.value === kind) copied.value = ''
    }, 1600)
  } catch {
    copyError.value =
      '这段没复制成。你的浏览器可能不允许这个页面写剪贴板，换个浏览器或手动选中复制。'
  }
}
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
        <h2 class="font-medium">{{ s.wizard.step3 }}</h2>
        <p class="muted text-sm mt-1">{{ s.wizard.pickFolderHint }}</p>

        <div class="mt-4 flex items-center gap-2 flex-wrap">
          <button class="btn" type="button" @click="pickerOpen = true">
            {{ s.wizard.pickFolder }}
          </button>
          <span v-if="kbPath" class="mono text-sm break-all">{{ kbPath }}</span>
        </div>

        <div v-if="kbPath" class="mt-4 flex items-center gap-3 flex-wrap">
          <label class="text-sm w-16" for="wizard-name">{{ s.wizard.name }}</label>
          <input id="wizard-name" v-model="kbName" class="input w-48" />
        </div>

        <div v-if="pickerOpen" class="mt-4">
          <DirPicker :start="kbPath" @pick="onPick" @cancel="pickerOpen = false" />
        </div>
      </section>

      <section v-else class="mt-6">
        <h2 class="font-medium">{{ s.wizard.done }}</h2>
        <p class="muted text-sm mt-1">{{ s.wizard.doneHint }}</p>

        <div class="mt-4 grid grid-cols-1 sm:grid-cols-2 gap-2">
          <button class="btn" type="button" @click="copy('cmd')">
            {{ copied === 'cmd' ? s.common.copied : s.wizard.copyCurl }}
          </button>
          <button class="btn" type="button" @click="copy('skill')">
            {{ copied === 'skill' ? s.common.copied : s.wizard.copySkill }}
          </button>
          <button class="btn" type="button" @click="copy('mcp')">
            {{ copied === 'mcp' ? s.common.copied : s.wizard.copyMcp }}
          </button>
          <button class="btn" type="button" @click="copy('authoring')">
            {{ copied === 'authoring' ? s.common.copied : s.wizard.copyAuthoring }}
          </button>
        </div>

        <p class="note mt-4 text-sm">{{ s.agentNote }}</p>
        <p v-if="copyError" class="note mt-2 text-sm">{{ copyError }}</p>
      </section>

      <p v-if="error" class="note mt-4 text-sm">{{ error }}</p>

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
          :disabled="busy || !kbPath"
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

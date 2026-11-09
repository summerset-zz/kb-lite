<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { Status } from '../types'
import { useStatus } from '../useStatus'
import ModalShell from '../components/ModalShell.vue'
import DirPicker from '../components/DirPicker.vue'

const props = defineProps<{ status: Status }>()
const { refresh } = useStatus()

const port = ref(props.status.port)
const lan = ref(props.status.lanExposed)
const requireToken = ref(props.status.requireToken)
const dbPath = ref(props.status.dbPath)

const busy = ref('')
const error = ref('')
const notice = ref('')

// ---- 访问密码 ----
const token = ref('')
const copied = ref('')
const pickerOpen = ref(false)

const pasteText = computed(
  () => `我的知识库服务在 ${location.protocol}//${location.hostname}:${props.status.port}，访问密码是 ${token.value}`,
)

async function loadToken() {
  try {
    const resp = await api.authToken('show')
    token.value = resp.token
  } catch {
    token.value = ''
  }
}

onMounted(() => void loadToken())

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

async function save() {
  busy.value = 'save'
  error.value = ''
  notice.value = ''
  try {
    const resp = await api.settings({
      port: port.value,
      host: lan.value ? '0.0.0.0' : '127.0.0.1',
      dbPath: dbPath.value.trim() || undefined,
      requireToken: lan.value ? true : requireToken.value,
    })
    requireToken.value = lan.value ? true : requireToken.value
    notice.value = resp.restartRequired ? s.settings.restartNeeded : '已保存。'
    await refresh()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}

async function rotate() {
  busy.value = 'rotate'
  error.value = ''
  try {
    const resp = await api.authToken('rotate')
    token.value = resp.token
    notice.value = '新密码已经生效，旧的立刻不能用了。'
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}

async function rerunWizard() {
  busy.value = 'wizard'
  try {
    await api.settings({ setupCompleted: false })
    await refresh()
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}

const quitOpen = ref(false)
async function quit() {
  busy.value = 'quit'
  try {
    await api.shutdown()
    quitOpen.value = false
  } catch {
    // 服务已经开始退出，连接被掐断是正常现象，不当成错误。
    quitOpen.value = false
  } finally {
    busy.value = ''
  }
}

// ---- 配色 ----
const theme = ref(localStorage.getItem('kb-lite-theme') ?? 'auto')
function setTheme(v: string) {
  theme.value = v
  const root = document.documentElement
  if (v === 'auto') {
    delete root.dataset.theme
    localStorage.removeItem('kb-lite-theme')
  } else {
    root.dataset.theme = v
    localStorage.setItem('kb-lite-theme', v)
  }
}
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 sm:px-6 py-6">
    <h1 class="font-medium mb-3">{{ s.settings.title }}</h1>

    <section class="card p-4 space-y-4">
      <!-- 记录文件 -->
      <div>
        <p class="text-sm font-medium">{{ s.settings.dbPath }}</p>
        <p class="muted text-xs mt-0.5">{{ s.settings.dbPathHint }}</p>
        <div class="flex items-center gap-2 mt-2 flex-wrap">
          <input v-model="dbPath" class="input flex-1 min-w-0 mono" spellcheck="false" />
          <button class="btn btn-sm" type="button" @click="pickerOpen = true">换个位置</button>
        </div>
        <div v-if="pickerOpen" class="mt-2">
          <DirPicker :start="dbPath" @pick="(p) => { dbPath = p; pickerOpen = false }" @cancel="pickerOpen = false" />
        </div>
      </div>

      <!-- 端口 -->
      <div>
        <p class="text-sm font-medium">{{ s.settings.port }}</p>
        <div class="flex items-center gap-2 mt-2">
          <input v-model.number="port" class="input w-28 mono" type="number" min="1" max="65535" />
          <span class="muted text-xs">{{ s.wizard.portHint }}</span>
        </div>
      </div>

      <!-- 谁可以打开 -->
      <div>
        <p class="text-sm font-medium">{{ s.settings.whoCanOpen }}</p>
        <div class="mt-2 space-y-2">
          <label class="radio-row" :data-checked="!lan">
            <input v-model="lan" type="radio" :value="false" class="mt-1" />
            <span>{{ s.wizard.loopback }}</span>
          </label>
          <label class="radio-row" :data-checked="lan">
            <input v-model="lan" type="radio" :value="true" class="mt-1" />
            <span>{{ s.wizard.lan }}</span>
          </label>
        </div>
        <p v-if="lan" class="note mt-2 text-sm">{{ s.wizard.lanWarn }}</p>
      </div>

      <!-- 访问密码 -->
      <div>
        <div class="flex items-center gap-3">
          <p class="text-sm font-medium flex-1">{{ s.settings.password }}</p>
          <label class="flex items-center gap-2 text-sm">
            <input v-model="requireToken" type="checkbox" :disabled="lan" />
            {{ requireToken || lan ? s.settings.passwordOn : s.settings.passwordOff }}
          </label>
        </div>

        <template v-if="requireToken || lan">
          <p class="muted text-sm mt-2">{{ s.settings.passwordOnHint }}</p>
          <div class="card p-3 mt-2" :style="{ background: 'var(--surface-2)' }">
            <p class="mono text-xs break-all">{{ pasteText }}</p>
            <div class="flex gap-2 mt-2">
              <button class="btn btn-sm" type="button" @click="copy(pasteText, 'paste')">
                {{ copied === 'paste' ? s.common.copied : s.common.copy }}
              </button>
              <button class="btn btn-sm" type="button" :disabled="busy !== ''" @click="rotate">
                {{ s.settings.rotate }}
              </button>
            </div>
          </div>
          <p class="muted text-xs mt-1">一次性密码，别发给别人。点“换一个密码”，旧的就作废了。</p>
        </template>
        <p v-else class="muted text-sm mt-2">{{ s.settings.passwordOffHint }}</p>
      </div>

      <p v-if="notice" class="text-sm" :style="{ color: 'var(--accent)' }">{{ notice }}</p>
      <p v-if="error" class="note text-sm">{{ error }}</p>

      <div class="flex items-center gap-2 pt-1">
        <button class="btn btn-primary" type="button" :disabled="busy !== ''" @click="save">
          {{ busy === 'save' ? s.common.loading : s.common.save }}
        </button>
      </div>
    </section>

    <!-- 其他 -->
    <section class="card p-4 mt-4 space-y-4">
      <div>
        <p class="text-sm font-medium">{{ s.settings.theme }}</p>
        <div class="flex gap-2 mt-2">
          <button
            v-for="opt in ['auto', 'light', 'dark'] as const"
            :key="opt"
            class="btn btn-sm"
            :class="{ 'btn-primary': theme === opt }"
            type="button"
            @click="setTheme(opt)"
          >
            {{ opt === 'auto' ? s.settings.themeAuto : opt === 'light' ? s.settings.themeLight : s.settings.themeDark }}
          </button>
        </div>
      </div>

      <div>
        <p class="text-sm font-medium">{{ s.settings.rerunWizard }}</p>
        <button class="btn btn-sm mt-2" type="button" :disabled="busy !== ''" @click="rerunWizard">
          {{ s.settings.rerunWizard }}
        </button>
      </div>

      <div>
        <p class="text-sm font-medium">{{ s.settings.quit }}</p>
        <p class="muted text-xs mt-0.5">{{ s.settings.quitHint }}</p>
        <button class="btn btn-danger btn-sm mt-2" type="button" @click="quitOpen = true">
          {{ s.settings.quit }}
        </button>
      </div>
    </section>

    <ModalShell :open="quitOpen" :title="s.settings.quit" @close="quitOpen = false">
      <p class="text-sm">{{ s.settings.quitHint }}</p>
      <template #footer>
        <button class="btn btn-ghost" type="button" @click="quitOpen = false">算了</button>
        <button class="btn btn-danger" type="button" :disabled="busy !== ''" @click="quit">
          退出
        </button>
      </template>
    </ModalShell>

    <p class="muted text-xs mt-6 text-center">
      这个页面关掉不代表 kb-lite 退出了。它还在后台跑着，AI 照样连得上。
    </p>
  </div>
</template>

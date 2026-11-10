<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { StartupInfo, Status, UpdateInfo } from '../types'
import { useStatus } from '../useStatus'
import { useToast } from '../toast'
import ModalShell from '../components/ModalShell.vue'

const props = defineProps<{ status: Status }>()
const { refresh } = useStatus()
// 保存成功的反馈走提示条（浮在页面底部，自己消失）——
// 以前成功什么也不说，用户只能看到页面没变。
const toast = useToast()

const port = ref(props.status.port)
const lan = ref(props.status.lanExposed)
const requireToken = ref(props.status.requireToken)

const busy = ref('')
const error = ref('')
// 端口当场搬走时记下新地址：旧页面之后每个请求都会失败，得让用户一键跳过去。
const movedTo = ref('')

// ---- 访问密码 ----
const token = ref('')
const copied = ref('')

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

// ---- 开机自启 / 快捷方式 / 检查更新 ----
const startup = ref<StartupInfo | null>(null)
const shortcutPath = ref('')
const update = ref<UpdateInfo | null>(null)
const updateError = ref('')

async function loadStartup() {
  try {
    startup.value = await api.startup()
  } catch {
    startup.value = null
  }
}

async function toggleStartup() {
  if (!startup.value) return
  const next = !startup.value.enabled
  busy.value = 'startup'
  error.value = ''
  try {
    startup.value = await api.setStartup(next)
    toast.ok(next ? s.settings.startupEnabled : s.settings.startupDisabled)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}

async function createShortcut() {
  busy.value = 'shortcut'
  error.value = ''
  shortcutPath.value = ''
  try {
    const resp = await api.shortcut()
    shortcutPath.value = resp.path
    toast.ok(s.settings.shortcutCreated)
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}

async function checkUpdate(refresh = false) {
  busy.value = 'update'
  updateError.value = ''
  try {
    update.value = await api.update(refresh)
  } catch (e) {
    updateError.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}

onMounted(() => {
  void loadToken()
  void loadStartup()
})

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
  movedTo.value = ''
  try {
    const resp = await api.settings({
      port: port.value,
      host: lan.value ? '0.0.0.0' : '127.0.0.1',
      requireToken: lan.value ? true : requireToken.value,
    })
    requireToken.value = lan.value ? true : requireToken.value
    port.value = resp.effective.port

    // 端口已经当场重开：旧地址马上就不能用了，得把用户引到新地址去。
    if (resp.moved && resp.effective.port !== Number(location.port)) {
      movedTo.value = `http://${location.hostname}:${resp.effective.port}`
    }
    if (resp.restartRequired) {
      toast.warn(s.settings.restartNeeded)
    } else {
      toast.ok(s.common.saved)
    }
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
    toast.ok(s.settings.rotated)
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

async function togglePause() {
  busy.value = 'pause'
  error.value = ''
  try {
    const next = !props.status.paused
    await api.setPaused(next)
    if (next) {
      toast.warn(s.settings.paused)
    } else {
      toast.ok(s.settings.resumed)
    }
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
      <!-- 配置与缓存都住在这个目录里，**不给改**：让用户挑一个缓存文件的位置，
           收益为零，困惑很大。要整个搬走就把这个目录复制到新机器。 -->
      <div>
        <div class="flex items-center gap-2 flex-wrap">
          <p class="text-sm font-medium flex-1">{{ s.settings.location }}</p>
          <button class="btn btn-xs" type="button" @click="copy(status.configDir, 'dir')">
            {{ copied === 'dir' ? s.common.copied : s.settings.locationCopy }}
          </button>
        </div>
        <p class="muted text-xs mt-0.5">{{ s.settings.locationHint }}</p>
        <p class="mono text-xs mt-2 break-all">{{ status.configDir }}</p>
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

      <p v-if="movedTo" class="note text-sm">
        {{ s.settings.moved }} 新地址是 <span class="mono">{{ movedTo }}</span>。
        <a :href="movedTo" class="underline">{{ s.settings.moveNow }}</a>
      </p>
      <p v-if="error" class="note text-sm">{{ error }}</p>

      <div class="flex items-center gap-2 pt-1">
        <button class="btn btn-primary" type="button" :disabled="busy !== ''" @click="save">
          {{ busy === 'save' ? s.common.loading : s.common.save }}
        </button>
      </div>
    </section>

    <!-- 启动与更新 -->
    <section class="card p-4 mt-4 space-y-4">
      <div>
        <div class="flex items-center gap-2 flex-wrap">
          <p class="text-sm font-medium flex-1">{{ s.settings.startup }}</p>
          <label class="flex items-center gap-2 text-sm">
            <input
              type="checkbox"
              :checked="startup?.enabled ?? false"
              :disabled="!startup?.supported || busy !== ''"
              @change="toggleStartup"
            />
            {{ startup?.enabled ? '已开启' : '未开启' }}
          </label>
        </div>
        <p class="muted text-xs mt-0.5">
          {{ startup?.supported ? (startup?.enabled ? s.settings.startupOn : s.settings.startupOff) : s.settings.startupUnsupported }}
        </p>
        <p v-if="startup?.supported && startup.command" class="mono text-xs muted mt-1 break-all">
          {{ s.settings.startupCommand }}：{{ startup.command }}
        </p>
        <p v-if="startup?.mismatch" class="note text-xs mt-2">
          {{ s.settings.startupMismatch(startup.exePath ?? '') }}
        </p>
        <p v-if="startup?.fragile" class="note text-xs mt-2">{{ s.settings.startupFragile }}</p>
      </div>

      <div>
        <p class="text-sm font-medium">{{ s.settings.shortcut }}</p>
        <p class="muted text-xs mt-0.5">{{ s.settings.shortcutHint }}</p>
        <button class="btn btn-sm mt-2" type="button" :disabled="busy !== ''" @click="createShortcut">
          {{ s.settings.shortcut }}
        </button>
        <p v-if="shortcutPath" class="mono text-xs mt-1 break-all">
          {{ s.settings.shortcutDone }}：{{ shortcutPath }}
        </p>
      </div>

      <div>
        <div class="flex items-center gap-2 flex-wrap">
          <p class="text-sm font-medium flex-1">{{ s.settings.update }}</p>
          <button class="btn btn-sm" type="button" :disabled="busy !== ''" @click="checkUpdate(true)">
            {{ busy === 'update' ? s.settings.updateChecking : s.settings.update }}
          </button>
        </div>
        <p class="muted text-xs mt-0.5">{{ s.settings.updateCurrent(status.version) }}</p>
        <p v-if="updateError" class="note text-xs mt-1">{{ updateError }}</p>
        <p v-else-if="update && !update.ok" class="note text-xs mt-1">
          {{ s.settings.updateFailed }}<span class="muted">{{ update.error }}</span>
        </p>
        <p v-else-if="update && update.newer" class="text-xs mt-1" :style="{ color: 'var(--accent)' }">
          {{ s.settings.updateNew(update.latest ?? '') }} ·
          <a :href="update.url" target="_blank" rel="noopener" class="underline">{{ s.settings.updateDownload }}</a>
        </p>
        <p v-else-if="update" class="muted text-xs mt-1">
          {{ update.dev ? s.settings.updateDev : s.settings.updateLatest }}
        </p>
      </div>
    </section>

    <!-- 其他 -->
    <section class="card p-4 mt-4 space-y-4">
      <div>
        <p class="text-sm font-medium">{{ s.settings.service }}</p>
        <p class="muted text-xs mt-0.5">{{ s.settings.pauseHint }}</p>
        <button
          class="btn btn-sm mt-2"
          type="button"
          :disabled="busy !== ''"
          @click="togglePause"
        >
          {{ status.paused ? s.settings.resume : s.settings.pause }}
        </button>
      </div>

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

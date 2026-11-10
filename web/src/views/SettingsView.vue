<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '../api'
import { copyPath, revealPath } from '../actions'
import { s } from '../strings'
import type { ShortcutInfo, StartupInfo, Status, UpdateInfo } from '../types'
import { useStatus } from '../useStatus'
import { useToast } from '../toast'
import ModalShell from '../components/ModalShell.vue'
import SplitButton from '../components/SplitButton.vue'

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
const shortcut = ref<ShortcutInfo | null>(null)
const update = ref<UpdateInfo | null>(null)
const updateError = ref('')

async function loadStartup() {
  try {
    startup.value = await api.startup()
  } catch {
    startup.value = null
  }
}

async function loadShortcut() {
  try {
    shortcut.value = await api.shortcut()
  } catch {
    shortcut.value = null
  }
}

async function toggleStartup() {
  if (!startup.value) return
  await writeStartup(!startup.value.enabled)
}

// 自启项指向的是另一份程序时，把这个开关重写一遍就好：写进去的永远是当前 exe。
// 不去“改那一行”，直接覆盖；反正原来的那行本来就是旧的。
async function fixStartup() {
  await writeStartup(true)
}

async function writeStartup(next: boolean) {
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

// 快捷方式只有一条路：重建。覆盖之后自然就指向当前这份 exe。
async function createShortcut() {
  busy.value = 'shortcut'
  error.value = ''
  try {
    shortcut.value = await api.createShortcut()
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
  void loadShortcut()
})

// 访问密码默认只在“共享到局域网”时才出现：只绑本机的人不需要被一个密码框打扰。
// 但已经开着的时候必须能看见、能关掉，否则用户就锁不回去了。
const showPassword = computed(() => lan.value || requireToken.value)

// 快捷方式那段的一句话与按钮文字，按“有没有、指向谁”分三种。
const shortcutHint = computed(() => {
  const st = shortcut.value
  if (!st?.supported) return s.settings.shortcutUnsupported
  if (!st.exists) return s.settings.shortcutHint
  if (st.missing || st.mismatch) return '' // 用下面那条警示说明
  return s.settings.shortcutCurrent
})
const shortcutAction = computed(() =>
  shortcut.value?.exists ? s.settings.shortcutRecreate : s.settings.shortcut,
)

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
    // 地址或密码变了：装好的那份说明书就要重装，AI 也得知道一声。
    const addrChanged = resp.moved || resp.effective.requireToken !== props.status.requireToken
    await refresh()
    if (addrChanged) openStale()
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
    // 密码换了，装好的说明书里那把就是旧的。
    openStale()
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

// 启动 / 停止已经搬到顶栏（全站状态就该一直在），设置页不再重复一份。
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

// ---- SKILL 过期提示 ----
//
// 装好的 SKILL.md 里写着服务地址和访问密码。改了这两个东西，用户手上那份就不对了，
// 而 AI 不会自己发现。所以改完当场追一句，并给一句可以直接贴给 AI 的话。
const staleOpen = ref(false)
const staleBase = ref('')

function openStale() {
  staleBase.value = `http://${location.hostname}:${port.value}`
  staleOpen.value = true
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
          <SplitButton id="settings-dir" :label="s.kbs.actions" menu-only>
            <template #default="{ close }">
              <button
                class="menu-item"
                type="button"
                @click="close(); revealPath(status.configDir)"
              >
                {{ s.settings.openLocation }}
              </button>
              <button
                class="menu-item"
                type="button"
                @click="close(); copyPath(status.configDir)"
              >
                {{ s.common.copyPath }}
              </button>
            </template>
          </SplitButton>
        </div>
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

      <!-- 访问密码：只有开了才显示（共享到局域网时强制开）。 -->
      <div v-if="showPassword">
        <div class="flex items-center gap-3">
          <p class="text-sm font-medium flex-1">{{ s.settings.password }}</p>
          <label class="flex items-center gap-2 text-sm">
            <input v-model="requireToken" type="checkbox" :disabled="lan" />
            {{ requireToken || lan ? s.settings.passwordOn : s.settings.passwordOff }}
          </label>
        </div>

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
        <button
          v-if="startup?.mismatch"
          class="btn btn-sm mt-2"
          type="button"
          :disabled="busy !== ''"
          @click="fixStartup"
        >
          {{ s.settings.startupFix }}
        </button>
        <p v-if="startup?.fragile" class="note text-xs mt-2">{{ s.settings.startupFragile }}</p>
      </div>

      <div>
        <div class="flex items-center gap-2 flex-wrap">
          <p class="text-sm font-medium flex-1">{{ s.settings.shortcut }}</p>
          <button
            class="btn btn-sm"
            type="button"
            :disabled="busy !== '' || !shortcut?.supported"
            @click="createShortcut"
          >
            {{ busy === 'shortcut' ? s.common.loading : shortcutAction }}
          </button>
        </div>
        <p v-if="shortcutHint" class="muted text-xs mt-0.5">{{ shortcutHint }}</p>
        <p v-if="shortcut?.error" class="note text-xs mt-2">{{ shortcut.error }}</p>
        <p v-else-if="shortcut?.missing" class="note text-xs mt-2">
          {{ s.settings.shortcutDeleted(shortcut.target ?? '') }}
        </p>
        <p v-else-if="shortcut?.mismatch" class="note text-xs mt-2">
          {{ s.settings.shortcutMismatch(shortcut.target ?? '') }}
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
        <p class="text-sm font-medium">{{ s.logs.title }}</p>
        <p class="muted text-xs mt-0.5">{{ s.logs.hint }}</p>
        <a class="btn btn-sm mt-2" href="#logs">{{ s.logs.title }}</a>
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

    <!-- SKILL 过期：地址或密码变了，装好的那份说明书就不对了 -->
    <ModalShell :open="staleOpen" :title="s.settings.staleTitle" @close="staleOpen = false">
      <p class="text-sm">{{ s.settings.staleBody }}</p>
      <p class="muted text-xs mt-3">{{ s.settings.stalePromptHint }}</p>
      <div class="card p-2 mt-1 flex items-start gap-2">
        <pre class="preview flex-1 min-w-0">{{ s.settings.stalePrompt(staleBase) }}</pre>
        <button
          class="btn btn-xs shrink-0"
          type="button"
          @click="copy(s.settings.stalePrompt(staleBase), 'stale')"
        >
          {{ copied === 'stale' ? s.common.copied : s.common.copy }}
        </button>
      </div>
      <p class="muted text-xs mt-3">{{ s.settings.staleHow }}</p>
      <template #footer>
        <button class="btn btn-primary" type="button" @click="staleOpen = false">
          {{ s.common.ok }}
        </button>
      </template>
    </ModalShell>

    <ModalShell :open="quitOpen" :title="s.settings.quit" @close="quitOpen = false">
      <p class="text-sm">{{ s.settings.quitHint }}</p>
      <template #footer>
        <button class="btn btn-ghost" type="button" @click="quitOpen = false">算了</button>
        <button class="btn btn-danger" type="button" :disabled="busy !== ''" @click="quit">
          退出
        </button>
      </template>
    </ModalShell>
  </div>
</template>

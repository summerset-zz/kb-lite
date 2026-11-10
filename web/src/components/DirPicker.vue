<script setup lang="ts">
/*
 * 目录选择器 = 一个完整的弹窗（不是页内一块，也不是只有列表的半成品）。
 *
 * 确认按钮放在弹窗底部、紧挨“取消”，因为它就是确认：以前它摆在右上角，
 * 和底部的关闭按钮隔着半个屏幕，用户分不清哪个才是“就选这个”。
 *
 * 选完之后要起的名字也在这里问。名字属于“我正要创建的这个知识库”，
 * 留在页面上会让“选文件夹”和“确认创建”变成两件看不出关系的事。
 */
import { computed, ref, watch } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { FsList } from '../types'
import ModalShell from './ModalShell.vue'
import AppIcon from './AppIcon.vue'

const props = defineProps<{
  open: boolean
  /** 从哪个目录开始。省略或空 = 先给盘符。 */
  start?: string
  /** 要不要问名字（添加知识库要，换记录文件位置不要）。 */
  askName?: boolean
  /** 父组件在确认之后做事时的状态：创建中转圈、失败的原因。 */
  submitting?: boolean
  submitError?: string
}>()
const emit = defineEmits<{ pick: [path: string, name: string]; cancel: [] }>()

const cur = ref<FsList | null>(null)
const loading = ref(false)
const error = ref('')
const busy = ref('')

// 地址栏：能点、能改、能把路径直接贴进来（从资源管理器的地址栏复制）。
const typed = ref('')
const name = ref('')

// “新建文件夹”的小弹窗
const mkOpen = ref(false)
const mkName = ref('')
const mkBusy = ref(false)
const mkError = ref('')

// 添加前的确认：先探一眼（是不是磁盘根、里面有多少 markdown），再问一句。
// 不探就直接加，用户很可能把整个 D:\\ 加进去，然后等好几分钟。
const probeOpen = ref(false)
const probing = ref(false)
const probeMsg = ref('')

const path = computed(() => cur.value?.path ?? '')

// 根目录（`D:\`）没有 parent，但还能退回上一屏的盘符列表。
const upTarget = computed(() => cur.value?.parent ?? '')
const canUp = computed(() => {
  if (!path.value) return false // 已经是盘符列表，没有更上一层
  return Boolean(cur.value?.parent) || Boolean(cur.value?.drives?.length)
})

// 输入框里改过、而且跟当前目录不一样：才露出“前往”。
const dirty = computed(() => typed.value.trim() !== path.value)

const canConfirm = computed(
  () => Boolean(path.value) && (!props.askName || name.value.trim().length > 0),
)

async function go(p: string) {
  loading.value = true
  busy.value = p
  try {
    cur.value = await api.fsList(p)
    error.value = ''
  } catch (e) {
    // 失败时不动 cur：地址栏里留着用户打的字，让他就着那一处改。
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    loading.value = false
    busy.value = ''
  }
}

function submitTyped() {
  const p = typed.value.trim()
  if (p === path.value) return
  void go(p)
}

// 每次打开都重新载一次，不用上一次留下的目录。
watch(
  () => props.open,
  (open) => {
    if (!open) return
    cur.value = null
    error.value = ''
    mkOpen.value = false
    mkName.value = ''
    typed.value = props.start ?? ''
    void go(props.start ?? '')
  },
  { immediate: true },
)

// 位置变了：地址栏跟着变；名字默认取最后一段（盘符根就退回一个通用名）。
watch(
  () => cur.value?.path,
  (p) => {
    typed.value = p ?? ''
    if (!props.askName) return
    const seg = (p ?? '').split(/[\\/]/).filter(Boolean).pop() ?? ''
    name.value = seg && !/^[A-Za-z]:$/.test(seg) ? seg : s.picker.defaultName
  },
)

function confirm() {
  if (!canConfirm.value) return
  // 只是选个文件夹（不建知识库）时不必追问。
  if (!props.askName) {
    emit('pick', path.value, '')
    return
  }
  void probeAndAsk()
}

async function probeAndAsk() {
  probing.value = true
  error.value = ''
  try {
    const p = await api.fsProbe(path.value)
    probeMsg.value = p.isRoot
      ? s.picker.confirmRoot
      : p.capped
        ? s.picker.confirmCapped(p.markdownCount)
        : p.markdownCount === 0
          ? s.picker.confirmEmpty
          : s.picker.confirmCount(p.markdownCount)
    probeOpen.value = true
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    probing.value = false
  }
}

function confirmed() {
  probeOpen.value = false
  emit('pick', path.value, name.value.trim())
}

async function createDir() {
  const n = mkName.value.trim()
  if (!n || !path.value) return
  mkBusy.value = true
  mkError.value = ''
  try {
    const resp = await api.fsMkdir(path.value, n)
    mkOpen.value = false
    mkName.value = ''
    // 建完直接进去：用户下一步的动作本来就发生在刚建好的那个文件夹里。
    await go(resp.path)
  } catch (e) {
    mkError.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    mkBusy.value = false
  }
}
</script>

<template>
  <ModalShell
    :open="open"
    :title="askName ? s.picker.titlePick : s.picker.titleBrowse"
    wide
    @close="emit('cancel')"
  >
    <div class="flex items-center gap-2 flex-wrap">
      <button class="btn btn-sm" type="button" :disabled="!canUp || loading" @click="go(upTarget)">
        {{ s.common.back }}
      </button>
      <input
        v-model="typed"
        class="input flex-1 min-w-0 mono"
        spellcheck="false"
        autocomplete="off"
        :placeholder="s.picker.root"
        :title="s.picker.pathInputTitle"
        @keydown.enter.prevent="submitTyped"
      />
      <button v-if="dirty" class="btn btn-sm" type="button" :disabled="loading" @click="submitTyped">
        {{ s.picker.go }}
      </button>
      <button
        v-if="path"
        class="btn btn-sm"
        type="button"
        :disabled="loading"
        @click="mkOpen = true"
      >
        {{ s.picker.newFolder }}
      </button>
    </div>

    <p v-if="error" class="note mt-3 text-sm">{{ error }}</p>
    <p v-if="askName" class="text-sm mt-3">{{ s.picker.pickHint }}</p>
    <p class="muted text-xs mt-2">{{ s.picker.pathHint }}</p>

    <div class="mt-3 max-h-72 overflow-auto -mx-1 px-1">
      <p v-if="loading" class="muted text-sm py-6 text-center">{{ s.common.loading }}</p>

      <template v-else-if="cur">
        <!-- 还没有路径时只有盘符：那时候说“这个位置下面没有子文件夹”是废话，
             确认按钮也点不了。先告诉他要干什么。 -->
        <p v-if="!path" class="muted text-sm py-6 text-center">{{ s.picker.emptyRoot }}</p>

        <p v-else-if="cur.dirs.length === 0 && !cur.drives?.length" class="muted text-sm py-6 text-center">
          {{ s.picker.emptyHere }}
        </p>

        <button
          v-for="d in cur.dirs"
          :key="d.path"
          type="button"
          class="w-full text-left px-2 py-2 rounded-md flex items-center gap-2 hover:bg-(--surface-2) disabled:opacity-50"
          :disabled="busy === d.path"
          @click="go(d.path)"
        >
          <AppIcon name="folder" class="muted" />
          <span class="truncate">{{ d.name }}</span>
        </button>

        <!-- 盘符像目录一样渲染，只是图标不同。不单独放一行按钮：对用户来说
             它们就是“上一层里的文件夹”。 -->
        <button
          v-for="dr in cur.drives"
          :key="dr"
          type="button"
          class="w-full text-left px-2 py-2 rounded-md flex items-center gap-2 hover:bg-(--surface-2) disabled:opacity-50"
          :disabled="busy === dr"
          @click="go(dr)"
        >
          <AppIcon name="drive" class="muted" />
          <span class="mono">{{ dr }}</span>
        </button>
      </template>
    </div>

    <!-- 名字在这里问：它属于“我正要创建的这个知识库”，不属于页面。 -->
    <div v-if="askName" class="mt-4">
      <label class="text-sm" for="picker-name">{{ s.picker.nameLabel }}</label>
      <input id="picker-name" v-model="name" class="input w-full mt-2" autocomplete="off" />
      <p class="muted text-xs mt-1">{{ s.picker.nameHint }}</p>
    </div>

    <!-- 在用户目录里就提醒一句：别的电脑用户读不到，索引也更新不了。
         不说的话，用户换个人登录看到“读不到目录”会以为是程序坏了。 -->
    <p v-if="askName && cur?.underHome" class="note mt-3 text-sm">{{ s.picker.sharedHint }}</p>

    <p v-if="submitError" class="note mt-3 text-sm">{{ submitError }}</p>

    <template #footer>
      <button class="btn btn-ghost" type="button" @click="emit('cancel')">
        {{ s.common.cancel }}
      </button>
      <button
        class="btn btn-primary"
        type="button"
        :disabled="!canConfirm || loading || submitting || probing"
        @click="confirm"
      >
        {{
          submitting
            ? s.common.loading
            : probing
              ? s.picker.probing
              : askName
                ? s.picker.confirmCreate
                : s.picker.confirmPick
        }}
      </button>
    </template>
  </ModalShell>

  <!-- 添加前的最后一次追问：磁盘根 / 已有 N 个文件 / 没有文件。 -->
  <ModalShell :open="probeOpen" :title="s.picker.confirmCreate" @close="probeOpen = false">
    <p class="text-sm">{{ probeMsg }}</p>
    <p class="mono text-xs muted mt-2 break-all">{{ path }}</p>
    <template #footer>
      <button class="btn btn-ghost" type="button" @click="probeOpen = false">
        {{ s.common.cancel }}
      </button>
      <button class="btn btn-primary" type="button" @click="confirmed">
        {{ s.picker.confirmCreate }}
      </button>
    </template>
  </ModalShell>

  <ModalShell :open="mkOpen" :title="s.picker.newFolder" @close="mkOpen = false">
    <label class="text-sm" for="mk-name">{{ s.picker.newFolderName }}</label>
    <input
      id="mk-name"
      v-model="mkName"
      class="input w-full mt-2"
      autocomplete="off"
      :placeholder="s.picker.newFolderPlaceholder"
      @keydown.enter.prevent="createDir"
    />
    <p class="muted text-xs mt-2">{{ s.picker.newFolderHint }}</p>
    <p class="muted text-xs mt-1 mono break-all">{{ path }}</p>
    <p v-if="mkError" class="note mt-2 text-sm">{{ mkError }}</p>
    <template #footer>
      <button class="btn btn-ghost" type="button" @click="mkOpen = false">
        {{ s.common.cancel }}
      </button>
      <button
        class="btn btn-primary"
        type="button"
        :disabled="mkBusy || !mkName.trim()"
        @click="createDir"
      >
        {{ mkBusy ? s.common.loading : s.common.create }}
      </button>
    </template>
  </ModalShell>
</template>

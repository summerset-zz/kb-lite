<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { FsList } from '../types'

const props = defineProps<{ start?: string }>()
const emit = defineEmits<{ pick: [path: string]; cancel: [] }>()

const cur = ref<FsList | null>(null)
const loading = ref(true)
const error = ref('')
const busy = ref('')
// 地址栏：能点、能改、能把路径直接贴进去（从资源管理器的地址栏复制）。
const typed = ref(props.start ?? '')

async function go(path: string) {
  loading.value = true
  busy.value = path
  try {
    cur.value = await api.fsList(path)
    error.value = ''
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    loading.value = false
    busy.value = ''
  }
}

onMounted(() => void go(props.start ?? ''))

const path = computed(() => cur.value?.path ?? '')

// 根目录（`D:\`）没有 parent，但还能退回上一屏的盘符列表。
// 以前只认 parent，于是在盘符根目录上“返回”永远是禁用的，用户卡在那里。
const upTarget = computed(() => cur.value?.parent ?? '')
const canUp = computed(() => {
  if (!path.value) return false // 已经是盘符列表，没有更上一层
  return Boolean(cur.value?.parent) || Boolean(cur.value?.drives?.length)
})

// 输入框里改过、而且跟当前目录不一样：才露出“前往”。
const dirty = computed(() => typed.value.trim() !== path.value)

// 跳完了（或者从列表点进去）就把输入框同步成真正的位置。失败时不动，
// 让用户就着自己打错的路径改。
watch(
  () => cur.value?.path,
  (p) => {
    typed.value = p ?? ''
  },
)

function submitTyped() {
  const p = typed.value.trim()
  if (p === path.value) return
  void go(p)
}
</script>

<template>
  <div class="card p-4">
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
        class="btn btn-primary btn-sm"
        type="button"
        :disabled="!path || loading"
        @click="emit('pick', path)"
      >
        {{ s.picker.pickThis }}
      </button>
    </div>

    <p v-if="error" class="note mt-3 text-sm">{{ error }}</p>
    <p class="muted text-xs mt-2">{{ s.picker.pathHint }}</p>

    <div class="mt-3 max-h-72 overflow-auto -mx-1 px-1">
      <p v-if="loading" class="muted text-sm py-6 text-center">{{ s.common.loading }}</p>

      <template v-else-if="cur">
        <!-- 还没有路径时只有盘符：那时候说“这个位置下面没有子文件夹”是废话，
             “用这个文件夹”也点不了。先告诉他要干什么。 -->
        <p v-if="!path" class="muted text-sm py-6 text-center">{{ s.picker.emptyRoot }}</p>

        <p v-else-if="cur.dirs.length === 0" class="muted text-sm py-6 text-center">
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
          <span class="truncate">{{ d.name }}</span>
          <span class="flex-1" />
        </button>

        <div v-if="cur.drives?.length" class="mt-3 flex flex-wrap gap-2">
          <button
            v-for="dr in cur.drives"
            :key="dr"
            class="btn btn-sm mono"
            type="button"
            @click="go(dr)"
          >
            {{ dr }}
          </button>
        </div>
      </template>
    </div>

    <div class="flex justify-end mt-3">
      <button class="btn btn-ghost btn-sm" type="button" @click="emit('cancel')">
        {{ s.common.cancel }}
      </button>
    </div>
  </div>
</template>

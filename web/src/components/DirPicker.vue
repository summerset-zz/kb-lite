<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { FsList } from '../types'

const props = defineProps<{ start?: string }>()
const emit = defineEmits<{ pick: [path: string]; cancel: [] }>()

const cur = ref<FsList | null>(null)
const loading = ref(true)
const error = ref('')
const busy = ref('')

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
const canUp = computed(() => Boolean(cur.value?.parent))
</script>

<template>
  <div class="card p-4">
    <div class="flex items-center gap-2 flex-wrap">
      <button class="btn btn-sm" type="button" :disabled="!canUp || loading" @click="go(cur?.parent ?? '')">
        {{ s.common.back }}
      </button>
      <span class="mono flex-1 truncate" :title="path">{{ path || '选取磁盘' }}</span>
      <button
        class="btn btn-primary btn-sm"
        type="button"
        :disabled="!path || loading"
        @click="emit('pick', path)"
      >
        用这个文件夹
      </button>
    </div>

    <p v-if="error" class="note mt-3 text-sm">{{ error }}</p>

    <div class="mt-3 max-h-72 overflow-auto -mx-1 px-1">
      <p v-if="loading" class="muted text-sm py-6 text-center">{{ s.common.loading }}</p>

      <template v-else-if="cur">
        <p v-if="cur.dirs.length === 0" class="muted text-sm py-6 text-center">
          这个位置下面没有子文件夹。选上面那个“用这个文件夹”也行。
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
          <span v-if="d.hasMarkdown" class="muted text-xs shrink-0">
            这里有 {{ d.markdownCount >= 2000 ? '2000+' : d.markdownCount }} 个 markdown 文件
          </span>
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

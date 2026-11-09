<script setup lang="ts">
import { computed, reactive, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { PreviewResponse, Status } from '../types'

const props = defineProps<{ status: Status }>()

const kbName = ref(props.status.kbs[0]?.name ?? '')
const query = ref('')
const entryPath = ref('')

type Kind = 'skill' | 'manifest' | 'search' | 'section'

// 每块各自的状态：谁能取、取到了什么、错在哪。
const state = reactive<Record<Kind, { loading: boolean; error: string; resp: PreviewResponse | null }>>({
  skill: { loading: false, error: '', resp: null },
  manifest: { loading: false, error: '', resp: null },
  search: { loading: false, error: '', resp: null },
  section: { loading: false, error: '', resp: null },
})

const hasKB = computed(() => props.status.kbs.length > 0)

async function take(kind: Kind) {
  const st = state[kind]
  st.loading = true
  st.error = ''
  try {
    st.resp = await api.preview({
      kind,
      kb: kind === 'skill' ? undefined : kbName.value,
      q: kind === 'search' ? query.value.trim() : undefined,
      path: kind === 'section' ? entryPath.value.trim() : undefined,
      n: kind === 'section' ? 1 : undefined,
    })
  } catch (e) {
    st.resp = null
    st.error = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    st.loading = false
  }
}

/** 需要知识库的那几块，没选库时按钮不可用。 */
const needsKB = (kind: Kind) => kind !== 'skill' && !hasKB

/** 检索与阅读要额外参数，没填就别让人点。 */
const missingArg = (kind: Kind) => {
  if (kind === 'search') return query.value.trim() === ''
  if (kind === 'section') return entryPath.value.trim() === ''
  return false
}
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 sm:px-6 py-6">
    <h1 class="font-medium">{{ s.agent.title }}</h1>
    <p class="muted text-sm mt-1">{{ s.agent.hint }}</p>

    <div class="card p-4 mt-4 flex flex-wrap items-end gap-3">
      <label class="text-sm">
        <span class="muted block mb-1">{{ s.search.kb }}</span>
        <select v-model="kbName" class="input" :disabled="!hasKB">
          <option v-for="kb in status.kbs" :key="kb.id" :value="kb.name">{{ kb.name }}</option>
        </select>
      </label>
      <label class="text-sm flex-1 min-w-0">
        <span class="muted block mb-1">{{ s.agent.query }}</span>
        <input v-model="query" class="input w-full" :placeholder="s.search.placeholder" />
      </label>
      <label class="text-sm flex-1 min-w-0">
        <span class="muted block mb-1">{{ s.agent.path }}</span>
        <input
          v-model="entryPath"
          class="input w-full mono"
          :placeholder="s.agent.pathPlaceholder"
          spellcheck="false"
        />
      </label>
    </div>

    <div class="grid grid-cols-1 gap-3 mt-4">
      <section v-for="kind in (['skill', 'manifest', 'search', 'section'] as const)" :key="kind" class="card p-4">
        <div class="flex items-start gap-3 flex-wrap">
          <div class="min-w-0 flex-1">
            <p class="text-sm font-medium">{{ s.agent.blocks[kind] }}</p>
            <p class="muted text-xs mt-0.5">{{ s.agent.blocks[`${kind}Hint` as const] }}</p>
          </div>
          <button
            class="btn btn-sm shrink-0"
            type="button"
            :disabled="state[kind].loading || needsKB(kind) || missingArg(kind)"
            @click="take(kind)"
          >
            {{ state[kind].resp ? s.agent.retake : s.agent.take }}
          </button>
        </div>

        <p v-if="needsKB(kind)" class="muted text-xs mt-2">{{ s.agent.needKB }}</p>
        <p v-if="state[kind].error" class="note text-xs mt-2">{{ state[kind].error }}</p>

        <template v-if="state[kind].resp">
          <p class="muted text-xs mt-2 mono break-all">
            {{ state[kind].resp!.status }} · {{ state[kind].resp!.contentType }} ·
            {{ state[kind].resp!.url }}
          </p>
          <pre class="preview mt-2">{{ state[kind].resp!.body }}</pre>
          <p v-if="state[kind].resp!.truncated" class="muted text-xs mt-1">
            {{ s.agent.truncated }}
          </p>
        </template>
      </section>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { Hit, SearchResponse, Status } from '../types'

const props = defineProps<{ status: Status }>()

const kbName = ref(props.status.kbs[0]?.name ?? '')
const query = ref('')
const limit = ref(5)
const loading = ref(false)
const error = ref('')
const resp = ref<SearchResponse | null>(null)

// 哪一条展开了“凭什么排在这”。同时只展开一条：这类表格很宽，展开多了没法看。
const openPath = ref('')

const hasKB = computed(() => props.status.kbs.length > 0)

/** 把 debug 行按 path 索引，方便每条结果取自己的明细。 */
const debugByPath = computed(() => {
  const m = new Map<string, NonNullable<SearchResponse['debug']>[number]>()
  for (const row of resp.value?.debug ?? []) m.set(row.path, row)
  return m
})

// 列的固定顺序（docs/03：命中位置的名称不许自己发明）
const columns: { key: keyof Hit; label: string }[] = [
  { key: 'title', label: s.search.colTitle },
  { key: 'leaf', label: s.search.colLeaf },
  { key: 'path', label: s.search.colPath },
  { key: 'toc', label: s.search.colToc },
  { key: 'body', label: s.search.colBody },
  { key: 'full', label: s.search.colFull },
]

async function run() {
  const q = query.value.trim()
  if (!q || !kbName.value) return
  loading.value = true
  error.value = ''
  openPath.value = ''
  try {
    resp.value = await api.search(kbName.value, q, limit.value)
  } catch (e) {
    resp.value = null
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    loading.value = false
  }
}

function detailOf(path: string) {
  const row = debugByPath.value.get(path)
  return row?.details?.[0]
}

function toggle(path: string) {
  openPath.value = openPath.value === path ? '' : path
}
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 sm:px-6 py-6">
    <h1 class="font-medium">{{ s.search.title }}</h1>
    <p class="muted text-sm mt-1">{{ s.search.hint }}</p>

    <div v-if="!hasKB" class="card p-6 mt-4 text-center">
      <p>{{ s.search.noKB }}</p>
    </div>

    <template v-else>
      <div class="card p-4 mt-4">
        <div class="flex flex-wrap items-end gap-3">
          <label class="text-sm">
            <span class="muted block mb-1">{{ s.search.kb }}</span>
            <select v-model="kbName" class="input">
              <option v-for="kb in status.kbs" :key="kb.id" :value="kb.name">{{ kb.name }}</option>
            </select>
          </label>
          <label class="text-sm flex-1 min-w-0">
            <span class="muted block mb-1">{{ s.search.query }}</span>
            <input
              v-model="query"
              class="input w-full"
              :placeholder="s.search.placeholder"
              @keyup.enter="run"
            />
          </label>
          <button class="btn btn-primary" type="button" :disabled="loading || !query.trim()" @click="run">
            {{ loading ? s.common.loading : s.search.run }}
          </button>
        </div>
        <p class="muted text-xs mt-2">{{ s.search.multiWord }}</p>
      </div>

      <p v-if="error" class="note mt-3 text-sm">{{ error }}</p>

      <div v-if="resp && resp.results.length === 0" class="card p-5 mt-4">
        <p>{{ s.search.empty }}</p>
        <p class="muted text-sm mt-1">{{ s.search.emptyNext }}</p>
      </div>

      <div v-if="resp && resp.results.length > 0" class="grid grid-cols-1 gap-2 mt-4">
        <div v-for="r in resp.results" :key="r.path" class="card p-4">
          <div class="flex items-start gap-3 flex-wrap">
            <div class="min-w-0 flex-1">
              <div class="font-medium truncate">{{ r.title }}</div>
              <p class="mono text-xs muted mt-1 break-all">
                {{ r.path }} · rev {{ r.rev }} · {{ s.search.sections(r.toc.length) }}
              </p>
            </div>
            <span class="text-sm shrink-0">{{ r.score }} 分</span>
            <button class="btn btn-sm shrink-0" type="button" @click="toggle(r.path)">
              {{ s.search.why }} {{ openPath === r.path ? '▴' : '▾' }}
            </button>
          </div>

          <div v-if="openPath === r.path && detailOf(r.path)" class="mt-3 overflow-auto">
            <table class="explain">
              <thead>
                <tr>
                  <th class="text-left">{{ s.search.keyword }}</th>
                  <th v-for="c in columns" :key="c.key">{{ c.label }}</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="h in detailOf(r.path)!.hits" :key="h.keyword">
                  <td class="text-left mono">
                    {{ h.keyword }}<span v-if="h.approx" class="muted">(≈)</span>
                  </td>
                  <td v-for="c in columns" :key="c.key">
                    <span v-if="h[c.key]" class="hit">✓</span>
                  </td>
                </tr>
              </tbody>
            </table>

            <p class="muted text-xs mt-2">
              {{ s.search.counts(detailOf(r.path)!.strong, detailOf(r.path)!.weak, detailOf(r.path)!.fullOnly) }}
            </p>
            <p v-if="detailOf(r.path)!.approx > 0" class="muted text-xs mt-1">
              {{ s.search.approx(detailOf(r.path)!.approx) }}
            </p>
          </div>
        </div>
      </div>
    </template>
  </div>
</template>

<style scoped>
.explain {
  width: 100%;
  border-collapse: collapse;
  font-size: 12px;
}
.explain th,
.explain td {
  padding: 4px 8px;
  border-bottom: 1px solid var(--border);
  text-align: center;
  white-space: nowrap;
}
.explain th {
  color: var(--text-muted);
  font-weight: 500;
}
.hit {
  color: var(--accent);
  font-weight: 700;
}
</style>

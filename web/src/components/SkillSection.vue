<script setup lang="ts">
import { ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import CopyInline from './CopyInline.vue'

const alsoClaude = ref(false)
const busy = ref('')
const error = ref('')
const done = ref<{ kind: string; paths: string[] } | null>(null)

async function install(kind: 'read' | 'authoring') {
  busy.value = kind
  error.value = ''
  try {
    const targets = alsoClaude.value ? ['dsh', 'claude'] : ['dsh']
    const resp = await api.installSkill(kind, targets)
    done.value = { kind, paths: resp.installed.map((t) => t.path) }
  } catch (e) {
    error.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    busy.value = ''
  }
}
</script>

<template>
  <section class="card p-4">
    <div class="grid grid-cols-1 sm:grid-cols-2 gap-3">
      <div>
        <button
          class="btn btn-primary btn-lg w-full"
          type="button"
          :disabled="busy !== ''"
          @click="install('read')"
        >
          {{ s.skill.installRead }}
        </button>
        <p class="muted text-xs mt-1">{{ s.skill.installReadHint }}</p>
      </div>

      <div>
        <button
          class="btn btn-lg w-full"
          type="button"
          :disabled="busy !== ''"
          @click="install('authoring')"
        >
          {{ s.skill.installAuthoring }}
        </button>
        <p class="muted text-xs mt-1">{{ s.skill.installAuthoringHint }}</p>
      </div>
    </div>

    <label class="flex items-center gap-2 mt-3 text-xs muted">
      <input v-model="alsoClaude" type="checkbox" />
      也装一份给 Claude Code 用
    </label>

    <p class="muted text-xs mt-2">{{ s.skill.afterInstall }}</p>

    <div v-if="done" class="mt-2 text-xs">
      <p>{{ s.skill.installed }}：</p>
      <p v-for="p in done.paths" :key="p" class="mono break-all">{{ p }}</p>
      <p v-if="done.paths.length === 0" class="muted">一个都没写成，检查一下设置里有没有写权限。</p>
    </div>

    <!-- 不想重启的话，这两句话直接贴给 AI 也能用（docs/06 §5.2） -->
    <div class="mt-3 grid grid-cols-1 gap-3">
      <div>
        <div class="flex items-center gap-2">
          <span class="text-xs muted flex-1 min-w-0">{{ s.skill.promptReadHint }}</span>
          <CopyInline :text="s.skill.promptRead" />
        </div>
        <p class="mono text-xs mt-1">{{ s.skill.promptRead }}</p>
      </div>
      <div>
        <div class="flex items-center gap-2">
          <span class="text-xs muted flex-1 min-w-0">{{ s.skill.promptAuthoringHint }}</span>
          <CopyInline :text="s.skill.promptAuthoring" />
        </div>
        <p class="mono text-xs mt-1">{{ s.skill.promptAuthoring }}</p>
      </div>
    </div>

    <p v-if="error" class="note mt-2 text-sm">{{ error }}</p>
  </section>
</template>

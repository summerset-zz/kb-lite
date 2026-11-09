<script setup lang="ts">
import { ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'

const alsoClaude = ref(false)
const busy = ref('')
const error = ref('')
const done = ref<{ kind: string; paths: string[] } | null>(null)
const copied = ref('')

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

async function copyPrompt(text: string, key: string) {
  try {
    await navigator.clipboard.writeText(text)
    copied.value = key
    window.setTimeout(() => {
      if (copied.value === key) copied.value = ''
    }, 1600)
  } catch {
    error.value = '这句话没复制成。手动选中复制吧。'
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
        <button
          class="btn btn-ghost btn-xs"
          type="button"
          @click="copyPrompt(s.skill.promptRead, 'read')"
        >
          {{ copied === 'read' ? s.common.copied : s.skill.copyPrompt }}
        </button>
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
        <button
          class="btn btn-ghost btn-xs"
          type="button"
          @click="copyPrompt(s.skill.promptAuthoring, 'authoring')"
        >
          {{ copied === 'authoring' ? s.common.copied : s.skill.copyPrompt }}
        </button>
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

    <p v-if="error" class="note mt-2 text-sm">{{ error }}</p>
  </section>
</template>

<script setup lang="ts">
import { s } from '../strings'
import type { Status } from '../types'

defineProps<{ status: Status }>()
</script>

<template>
  <footer class="border-t mt-10" :style="{ borderColor: 'var(--border)' }">
    <div
      class="mx-auto max-w-3xl px-4 sm:px-6 py-5 flex flex-wrap items-center gap-x-4 gap-y-2 text-sm muted"
    >
      <span class="mono">KBLite v{{ status.version }}</span>
      <a href="https://github.com/summerset-zz" target="_blank" rel="noopener">Summerset-zz</a>
      <a href="https://github.com/summerset-zz/kb-lite/issues" target="_blank" rel="noopener">
        {{ s.footer.feedback }}
      </a>
      <span>{{ s.footer.checkUpdate }} · {{ s.about.soon }}</span>
      <span class="flex-1" />
      <a href="https://buymeacoffee.com/summerset" target="_blank" rel="noopener">
        {{ s.footer.support }}
      </a>
    </div>
  </footer>
</template>

<style scoped>
a {
  color: inherit;
  text-decoration: none;
  border-bottom: 1px solid var(--border);
}
a:hover {
  color: var(--text);
  border-color: var(--text-muted);
}
</style>

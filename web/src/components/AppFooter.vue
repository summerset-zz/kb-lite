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
      <a href="#help">{{ s.footer.help }}</a>
      <span>{{ s.footer.checkUpdate }} · {{ s.about.soon }}</span>
      <span class="flex-1" />
      <a href="https://buymeacoffee.com/summerset" target="_blank" rel="noopener">
        {{ s.footer.support }}
      </a>
    </div>
    <!-- 这句话对所有页面都成立：用户看到“配置文件”“退出”之类的字眼时，最容易误以为关掉页面就停了服务。 -->
    <div class="mx-auto max-w-3xl px-4 sm:px-6 pb-5 text-xs muted">
      {{ s.footer.closeNote }}
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

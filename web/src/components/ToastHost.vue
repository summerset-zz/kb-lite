<script setup lang="ts">
// 唯一的提示容器。挂在 App.vue 的根节点里，所以向导页、出错页也能用。
import { dismiss, toasts, type ToastKind } from '../toast'
import { s } from '../strings'

function accent(kind: ToastKind): string {
  if (kind === 'fail') return 'var(--danger)'
  if (kind === 'warn') return 'var(--warn-border)'
  return 'var(--accent)'
}
</script>

<template>
  <div
    class="fixed inset-x-0 bottom-4 z-50 flex flex-col items-center gap-2 px-4 pointer-events-none"
    role="status"
    aria-live="polite"
  >
    <div
      v-for="t in toasts"
      :key="t.id"
      class="card flex items-center gap-3 px-4 py-2 text-sm pointer-events-auto max-w-md"
      :style="{ borderColor: accent(t.kind) }"
    >
      <span class="flex-1">{{ t.text }}</span>
      <button class="btn btn-ghost btn-xs" type="button" @click="dismiss(t.id)">
        {{ s.common.close }}
      </button>
    </div>
  </div>
</template>

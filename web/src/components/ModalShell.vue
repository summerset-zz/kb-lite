<script setup lang="ts">
import { ref, watch } from 'vue'
import { s } from '../strings'

/*
 * 原生 `<dialog>` 拿到焦点陷阱与 Esc 关闭，不需要弹层库（docs/06 §3 只买交互原语，
 * 而浏览器已经把这件事做对了）。等真需要锚点定位再考虑引 Reka UI。
 */
const props = defineProps<{ open: boolean; title: string; wide?: boolean }>()
const emit = defineEmits<{ close: [] }>()

const el = ref<HTMLDialogElement>()

watch(
  () => props.open,
  (open) => {
    const d = el.value
    if (!d) return
    if (open && !d.open) d.showModal()
    if (!open && d.open) d.close()
  },
)
</script>

<template>
  <dialog ref="el" class="shell" @close="emit('close')" @cancel.prevent="emit('close')">
    <div class="p-5" :class="wide ? 'w-[min(92vw,44rem)]' : 'w-[min(92vw,26rem)]'">
      <h2 class="font-medium">{{ title }}</h2>
      <div class="mt-3">
        <slot />
      </div>
      <div class="mt-5 flex items-center justify-end gap-2">
        <slot name="footer">
          <button class="btn" type="button" @click="emit('close')">{{ s.common.close }}</button>
        </slot>
      </div>
    </div>
  </dialog>
</template>

<style scoped>
.shell {
  padding: 0;
  border: 1px solid var(--border);
  border-radius: 12px;
  background: var(--surface);
  color: var(--text);
  max-width: none;
  max-height: 88vh;
  overflow: auto;
  /* 必须自己写回来：Tailwind 的 preflight 给所有元素 `margin: 0`，
     把 dialog 靠 margin:auto 居中的那条也一起抹掉了，于是弹框贴在左上角。 */
  margin: auto;
}
.shell::backdrop {
  background: rgb(0 0 0 / 42%);
}
</style>

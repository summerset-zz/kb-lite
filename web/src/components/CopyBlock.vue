<script setup lang="ts">
import { computed, ref } from 'vue'
import { s } from '../strings'

/*
 * 「给 AI 用的那一段」统一用这个块：先把内容摊开给人看一眼，再给复制按钮。
 *
 * 只有按钮没有内容时，用户不知道点下去会得到什么，也不敢点（docs/06 §8.5）。
 * 长文本只露前几行：全文要复制，不要塞满屏幕。
 */
const props = withDefaults(
  defineProps<{
    label: string
    text: string
    hint?: string
    /** 预览最多显示几行；0 表示全文（短文本用）。 */
    lines?: number
  }>(),
  { lines: 10, hint: '' },
)

const copied = ref(false)
const failed = ref('')

const preview = computed(() => {
  const text = props.text
  if (!text) return ''
  if (props.lines === 0) return text
  const all = text.split('\n')
  if (all.length <= props.lines) return text
  return all.slice(0, props.lines).join('\n') + '\n…'
})

const truncated = computed(() => {
  const all = props.text.split('\n')
  return props.lines !== 0 && all.length > props.lines
})

async function copy() {
  failed.value = ''
  try {
    await navigator.clipboard.writeText(props.text)
    copied.value = true
    window.setTimeout(() => (copied.value = false), 1600)
  } catch {
    failed.value = '没复制成。手动选中框里的文字复制吧。'
  }
}
</script>

<template>
  <div class="copy-block">
    <div class="flex items-center gap-2">
      <span class="text-sm font-medium flex-1 min-w-0 truncate">{{ label }}</span>
      <button class="btn btn-sm shrink-0" type="button" :disabled="!text" @click="copy">
        {{ copied ? s.common.copied : s.common.copy }}
      </button>
    </div>
    <p v-if="hint" class="muted text-xs mt-0.5">{{ hint }}</p>
    <pre v-if="text" class="preview mt-2">{{ preview }}</pre>
    <p v-else class="muted text-xs mt-1">{{ s.common.loading }}</p>
    <p v-if="truncated" class="muted text-xs mt-1">
      这里只露出前 {{ lines }} 行。点“复制”拿到的是全文。
    </p>
    <p v-if="failed" class="note text-xs mt-1">{{ failed }}</p>
  </div>
</template>

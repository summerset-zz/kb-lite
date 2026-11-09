<script setup lang="ts">
import { ref } from 'vue'
import { s } from '../strings'

/** 一行文字旁边的小复制按钮（内容就在屏幕上，不需要 CopyBlock 的预览框）。 */
const props = defineProps<{ text: string }>()

const copied = ref(false)

async function copy() {
  try {
    await navigator.clipboard.writeText(props.text)
    copied.value = true
    window.setTimeout(() => (copied.value = false), 1600)
  } catch {
    copied.value = false
  }
}
</script>

<template>
  <button class="btn btn-xs shrink-0" type="button" @click="copy">
    {{ copied ? s.common.copied : s.common.copy }}
  </button>
</template>

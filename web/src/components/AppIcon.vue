<script setup lang="ts">
/*
 * 几个内联图标。几何形状取自 lucide（ISC），但这里是直接内联的路径：
 * 为四个图标引一个图标依赖 + 一套按需打包配置，收益不成比例。
 * 如果要加图标，从这个文件里加，别在组件里各处画 SVG。
 */
defineProps<{ name: 'copy' | 'check' | 'folder' | 'drive' }>()
</script>

<template>
  <svg
    viewBox="0 0 24 24"
    fill="none"
    stroke="currentColor"
    stroke-width="1.8"
    stroke-linecap="round"
    stroke-linejoin="round"
    class="w-4 h-4 shrink-0"
    aria-hidden="true"
  >
    <template v-if="name === 'copy'">
      <rect width="14" height="14" x="8" y="8" rx="2" ry="2" />
      <path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2" />
    </template>
    <path v-else-if="name === 'check'" d="M20 6 9 17l-5-5" />
    <path
      v-else-if="name === 'folder'"
      d="M20 20a2 2 0 0 0 2-2V8a2 2 0 0 0-2-2h-7.9a2 2 0 0 1-1.69-.9L9.6 3.9A2 2 0 0 0 7.93 3H4a2 2 0 0 0-2 2v13a2 2 0 0 0 2 2Z"
    />
    <template v-else>
      <line x1="22" x2="2" y1="12" y2="12" />
      <path
        d="M5.45 5.11 2 12v6a2 2 0 0 0 2 2h16a2 2 0 0 0 2-2v-6l-3.45-6.89A2 2 0 0 0 16.76 4H7.24a2 2 0 0 0-1.79 1.11z"
      />
      <line x1="6" x2="6.01" y1="16" y2="16" />
      <line x1="10" x2="10.01" y1="16" y2="16" />
    </template>
  </svg>
</template>

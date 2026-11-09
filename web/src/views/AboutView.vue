<script setup lang="ts">
import { s } from '../strings'
import type { Status } from '../types'

defineProps<{ status: Status }>()
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 sm:px-6 py-8">
    <h1 class="font-medium">{{ s.about.title }}</h1>

    <section class="card p-5 mt-3 flex flex-col sm:flex-row gap-4 items-start">
      <img
        src="/avatar-summerset.jpg"
        alt="Summerset"
        class="h-20 w-20 rounded-full object-cover shrink-0"
        :style="{ border: '1px solid var(--border)' }"
      />
      <div class="min-w-0">
        <p class="font-medium">Summerset</p>
        <p class="text-sm muted mt-1">{{ s.about.bio }}</p>
        <ul class="text-sm mt-3 space-y-1">
          <li>
            {{ s.about.email }}：
            <a href="mailto:kyoinbeijing@163.com" class="mono">kyoinbeijing@163.com</a>
          </li>
          <li>
            {{ s.about.github }}：
            <a href="https://github.com/summerset-zz" target="_blank" rel="noopener" class="mono">
              github.com/summerset-zz
            </a>
          </li>
        </ul>
      </div>
    </section>

    <section class="card p-5 mt-3">
      <p class="font-medium">{{ s.about.support }}</p>
      <p class="muted text-sm mt-1">{{ s.about.supportHint }}</p>
      <!-- 二维码在深色主题下必须白底，否则识别不出来（docs/06 §5.3）。 -->
      <div class="bg-white p-2 rounded-lg inline-block mt-3">
        <img src="/buymecoffee-wx.png" alt="微信收款码" class="w-40 h-40 object-contain" />
      </div>
    </section>

    <p class="muted text-xs mt-6">KBLite v{{ status.version }}</p>
  </div>
</template>

<style scoped>
a {
  color: var(--accent);
}
</style>

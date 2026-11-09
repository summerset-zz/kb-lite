<script setup lang="ts">
import { computed, ref } from 'vue'
import { api, ApiError } from '../api'
import { s } from '../strings'
import type { Status } from '../types'
import { useStatus } from '../useStatus'
import KBCard from '../components/KBCard.vue'
import SkillSection from '../components/SkillSection.vue'
import DirPicker from '../components/DirPicker.vue'

const props = defineProps<{ status: Status }>()
const { refresh } = useStatus()

const addOpen = ref(false)
const adding = ref(false)
const addError = ref('')

const base = computed(() => `http://${location.hostname}:${props.status.port}`)
const reach = computed(() => (props.status.lanExposed ? s.wizard.lan : s.wizard.loopback))

function openAdd() {
  addOpen.value = true
  addError.value = ''
}

// 弹窗里已经选好文件夹、起好名字了，这里只管提交。失败留在弹窗里，改名字就能重试。
async function submitAdd(path: string, name: string) {
  adding.value = true
  addError.value = ''
  try {
    await api.createKB(name, path)
    addOpen.value = false
    await refresh()
  } catch (e) {
    addError.value = e instanceof ApiError ? e.message : s.errors.generic
  } finally {
    adding.value = false
  }
}

const copied = ref('')
async function copyBase() {
  try {
    await navigator.clipboard.writeText(base.value)
    copied.value = 'base'
    window.setTimeout(() => (copied.value = ''), 1600)
  } catch {
    copied.value = ''
  }
}
</script>

<template>
  <div class="mx-auto max-w-3xl px-4 sm:px-6 py-6">
    <!-- 运行状态 -->
    <section class="card p-4 text-sm">
      <div class="flex items-center gap-2 flex-wrap">
        <span class="muted">地址</span>
        <button class="btn btn-ghost btn-xs mono" type="button" @click="copyBase">
          {{ copied === 'base' ? s.common.copied : base }}
        </button>
      </div>
      <p class="muted mt-1">谁可以打开：{{ reach }}</p>
      <p v-if="status.requireToken" class="muted mt-1">
        访问密码已开启，AI 连接时要带上它（在设置里能看到）。
      </p>
      <p v-if="status.dbMode === 'memory'" class="mt-2 note text-xs">
        记录文件没能打开，这次是记在内存里的。不影响使用，但下次启动要重新整理一遍。
      </p>
    </section>

    <!-- 知识库列表 -->
    <div class="flex items-center justify-between mt-6 mb-2">
      <h1 class="font-medium">{{ s.nav.kbs }}</h1>
      <button class="btn btn-sm" type="button" @click="openAdd">{{ s.kbs.add }}</button>
    </div>

    <div v-if="status.kbs.length === 0" class="card p-6 text-center">
      <p>{{ s.kbs.empty }}</p>
      <p class="muted text-sm mt-1">{{ s.kbs.emptyHint }}</p>
      <button class="btn btn-primary mt-4" type="button" @click="openAdd">{{ s.kbs.add }}</button>
    </div>

    <div v-else class="grid grid-cols-1 gap-2">
      <KBCard v-for="kb in status.kbs" :key="kb.id" :kb="kb" @changed="refresh()" />
    </div>

    <!-- SKILL -->
    <h2 class="font-medium mt-8 mb-2">让 AI 用起来</h2>
    <SkillSection />

    <DirPicker
      :open="addOpen"
      ask-name
      :submitting="adding"
      :submit-error="addError"
      @pick="submitAdd"
      @cancel="addOpen = false"
    />
  </div>
</template>

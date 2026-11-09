<script setup lang="ts">
import { ref } from 'vue'
import { api } from './api'
import { useStatus } from './useStatus'
import { s } from './strings'
import AppHeader from './components/AppHeader.vue'
import AppFooter from './components/AppFooter.vue'
import WizardView from './views/WizardView.vue'
import KBListView from './views/KBListView.vue'
import SettingsView from './views/SettingsView.vue'
import AboutView from './views/AboutView.vue'

type Tab = 'kbs' | 'settings' | 'about'

const { status, loading, error, refresh, needWizard } = useStatus()
const tab = ref<Tab>('kbs')

function onWizardDone() {
  tab.value = 'kbs'
  void refresh()
}

const resuming = ref(false)
async function resume() {
  resuming.value = true
  try {
    await api.setPaused(false)
    await refresh()
  } finally {
    resuming.value = false
  }
}
</script>

<template>
  <div class="min-h-full flex flex-col">
    <template v-if="loading">
      <div class="flex-1 grid place-items-center">
        <p class="muted">{{ s.common.loading }}</p>
      </div>
    </template>

    <template v-else-if="error">
      <div class="flex-1 grid place-items-center px-6">
        <div class="card p-6 max-w-md w-full">
          <p class="font-medium">{{ s.errors.daemonDown }}</p>
          <p class="muted text-sm mt-2">
            这个页面本来是 kb-lite 自己启动的服务送出来的。它退出了，页面就失效了。重新打开一次 kb-lite 就好。
          </p>
          <button class="btn mt-4" type="button" @click="refresh()">{{ s.common.retry }}</button>
        </div>
      </div>
    </template>

    <template v-else-if="status && needWizard">
      <WizardView :status="status" @done="onWizardDone" />
    </template>

    <template v-else-if="status">
      <AppHeader v-model:tab="tab" :status="status" />
      <!-- 暂停是全站状态，必须一直看得见，否则用户只会看到“AI 查不到”。 -->
      <div v-if="status.paused" class="border-b" :style="{ borderColor: 'var(--border)', background: 'var(--warn-bg)' }">
        <div
          class="mx-auto max-w-3xl px-4 sm:px-6 py-2 flex items-center gap-3 text-sm"
          :style="{ color: 'var(--warn-text)' }"
        >
          <span class="flex-1">{{ s.settings.paused }}</span>
          <button class="btn btn-sm shrink-0" type="button" :disabled="resuming" @click="resume">
            {{ resuming ? s.common.loading : s.settings.resume }}
          </button>
        </div>
      </div>
      <main class="flex-1">
        <KBListView v-if="tab === 'kbs'" :status="status" />
        <SettingsView v-else-if="tab === 'settings'" :status="status" />
        <AboutView v-else :status="status" />
      </main>
      <AppFooter :status="status" />
    </template>
  </div>
</template>

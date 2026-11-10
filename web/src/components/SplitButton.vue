<script setup lang="ts">
/*
 * 分体按钮：左边是动作，右边只开菜单。文档里写成 `[按钮文本|↓]`（docs/06 §3）。
 *
 * 为什么要有它：一行里只放一个「操作」按钮时，用户不知道点下去是执行还是展开；
 * 放两个按钮（「更新」+「更多」）又占地方、还容易点错。分成两半以后，
 * “最常用的那一步”和“其余动作”在视觉上是一体的，手指落点也不同。
 *
 * 用 `useMenu()` 而不是 `<details>`：同时只开一个、点外面要关，这两件事浏览器都不管。
 */
import { useMenu } from '../menu'
import AppIcon from './AppIcon.vue'

const props = defineProps<{
  /** 全局唯一，用于“同时只开一个菜单”。 */
  id: string
  label: string
  /** 主色：说明这一步最该被点。 */
  primary?: boolean
  disabled?: boolean
  /** 左边那半没有单独的动作时（比如「操作」），点它也是开菜单。 */
  menuOnly?: boolean
}>()

const emit = defineEmits<{ action: [] }>()
const menu = useMenu()

function onLeft() {
  if (props.menuOnly) {
    menu.toggle(props.id)
    return
  }
  menu.close()
  emit('action')
}
</script>

<template>
  <div class="split relative shrink-0">
    <button
      class="btn btn-sm"
      :class="{ 'btn-primary': primary }"
      type="button"
      :disabled="disabled"
      @click="onLeft"
    >
      {{ label }}
    </button>
    <button
      class="btn btn-sm"
      :class="{ 'btn-primary': primary }"
      type="button"
      :disabled="disabled"
      aria-haspopup="menu"
      :aria-expanded="menu.isOpen(id)"
      aria-label="更多动作"
      @click.stop="menu.toggle(id)"
    >
      <AppIcon name="chevron-down" />
    </button>
    <div
      v-if="menu.isOpen(id)"
      class="card absolute right-0 mt-1 z-10 p-1 w-44"
      role="menu"
      @click.stop
    >
      <slot :close="menu.close" />
    </div>
  </div>
</template>

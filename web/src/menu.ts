// 页面上同时只允许开一个下拉菜单，点别处就关。
//
// 用 `<details>` 做下拉时，浏览器不会替你处理这两件事：点外面不关，
// 而且每张卡片的菜单各开各的（能同时开好几个）。这里给一个全局的“当前开的是谁”。
//
// 用法：
//   const menu = useMenu()
//   <button @click.stop="menu.toggle('kb-'+kb.id)">
//   <div v-if="menu.isOpen('kb-'+kb.id)" @click.stop>…
//
// 触发按钮与菜单本身都要 `@click.stop`：冒泡到 document 的那一下才算“点在外面”。
import { ref } from 'vue'

const openId = ref<string | null>(null)

let wired = false
function wire() {
  if (wired) return
  wired = true
  document.addEventListener('click', () => {
    openId.value = null
  })
}

export function useMenu() {
  wire()
  return {
    isOpen: (id: string) => openId.value === id,
    toggle: (id: string) => {
      openId.value = openId.value === id ? null : id
    },
    close: () => {
      openId.value = null
    },
  }
}

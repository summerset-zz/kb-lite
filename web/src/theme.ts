// 配色。只存两态（浅色 / 深色），默认跟随系统。
//
// 以前设置页里有“跟随系统 / 浅色 / 深色”三个按钮。顶栏放一个开关更省地方，
// 而三态循环的按钮点起来没人猜得到下一步是什么，所以这里是两态：
// 没点过就跟随系统；点过一次就固定在点出来的那个上。
import { ref } from 'vue'

const KEY = 'kb-lite-theme'
const stored = localStorage.getItem(KEY)
const current = ref<'light' | 'dark'>(
  stored === 'light' || stored === 'dark' ? stored : systemPrefers(),
)

function systemPrefers(): 'light' | 'dark' {
  return window.matchMedia?.('(prefers-color-scheme: dark)').matches ? 'dark' : 'light'
}

function apply(v: 'light' | 'dark') {
  document.documentElement.dataset.theme = v
}

// 第一次加载就把系统偏好落到 <html data-theme> 上，避免闪一下浅色。
apply(current.value)

export function useTheme() {
  function toggle() {
    current.value = current.value === 'dark' ? 'light' : 'dark'
    apply(current.value)
    localStorage.setItem(KEY, current.value)
  }
  return { theme: current, toggle }
}

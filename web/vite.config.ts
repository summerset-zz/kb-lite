import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'

/*
 * 开发时用 `pnpm dev`：vite 把 /api、/kb、/skill.md、/authoring.md 代理到本地 daemon，
 * 这样界面里发起的都是同源请求，`kb-admin` cookie 与 Origin 校验都能照常工作。
 *
 * 端口一律显式写死，不用 vite 的默认 5173（本机上常被别的项目占着）。
 * daemon 端口在 config.json 里，改过的话把下面的 daemon 也跟着改。
 */
const daemon = 'http://127.0.0.1:7331'

export default defineConfig({
  plugins: [vue(), tailwindcss()],
  server: {
    // 不要用 vite 的默认 5173：本机上它常被别的项目占着。
    // strictPort 让“端口被占”当场报错，而不是默默换成 5200 让人对着旧页面看半天。
    port: 5199,
    strictPort: true,
    proxy: {
      '/api': { target: daemon, changeOrigin: false },
      '/kb': { target: daemon, changeOrigin: false },
      '/skill.md': { target: daemon, changeOrigin: false },
      '/authoring.md': { target: daemon, changeOrigin: false },
    },
  },
  build: {
    outDir: 'dist',
    emptyOutDir: true,
  },
})

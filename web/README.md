# web

`kb-lite` 的界面。产物会被拷进 `internal/daemon/static/` 再 `//go:embed` 进 Go 二进制，
**用户侧永远不需要 Node**。这个目录只在开发时装。

## 依赖与命令

用 `pnpm`（不要混用 npm / yarn，锁文件只留一份）。

```bash
pnpm install
pnpm dev         # 起 vite（http://127.0.0.1:5199），代理 /api、/kb、/skill.md、/authoring.md 到 http://127.0.0.1:7331
pnpm typecheck   # 只检查类型，只读
pnpm build       # vue-tsc + vite build + 同步到 internal/daemon/static/
```

`pnpm dev` 之前先起 daemon（`go run ./cmd/kb-lite serve`）。vite 的代理写在
`vite.config.ts` 里，daemon 换过端口就要跟着改。

> 端口是显式写的 **5199**，不用 vite 默认的 5173（本机上常被别的项目占着），
> 并且开了 `strictPort`：被占时直接报错，不会默默顺延到别的端口。

> 端口、地址、记录文件位置一律从 `/api/status` 读，界面上不硬编码（docs/06）。

## 结构

```
src/
  api.ts          所有网络调用都在这（组件里不许出现裸 fetch）
  strings.ts      所有界面文案（术语照 docs/06 §8.3）
  types.ts        与 daemon 的响应形状对齐
  format.ts       体积 / 时间的人话格式化
  useStatus.ts    全站共享的状态与唯一的轮询
  App.vue         外壳：加载 / 连不上 / 向导 / 三个页面
  views/          WizardView · KBListView · SettingsView · AboutView
  components/     AppHeader · AppFooter · KBCard · SkillSection · DirPicker · ModalShell
```

## 约束（照抄 docs/06 §3，别自己发挥）

- 禁止 SSR / Nuxt / CSS-in-JS / Element Plus / Naive UI / Ant Design。
- **禁止用 `innerHTML` 渲染任何来自知识库或用户输入的内容。**
- 弹层先不引 Reka UI：原生 `<dialog>` 已经给了焦点陷阱与 Esc 关闭。
  等确实需要锚点定位（下拉贴边、浮层跟随）再引，别为了两个弹窗拖进一整套组件库。
- 图标暂不使用 `@iconify-json/lucide`。按钮一律用文字，等界面稳定后再补图标。
- 配色不用 JS 取系统值：`color-scheme` 与 `@media (prefers-color-scheme)` 在首帧就生效；
  用户手动覆盖时才由 `<head>` 里那几行同步脚本写 `data-theme`。两种情况都不闪。

## 还没有的东西

- “打开所在文件夹”“开机自启”“日志”三处设置项：后端接口还没有，先不摆按钮。
- 检查更新：v1 先占位。

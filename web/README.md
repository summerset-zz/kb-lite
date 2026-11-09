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

pnpm serve       # 本地看真实效果：停旧进程 → 构建界面 → 编译 → 起服务（前台）
pnpm serve --no-web    # 只改了 Go，跳过界面构建，快很多
pnpm serve --no-tray   # 不起托盘
pnpm serve --keep      # 不先停旧进程（它会因为端口被占自动 +1）
```

`pnpm dev` 之前先起 daemon（`go run ./cmd/kb-lite serve`）。vite 的代理写在
`vite.config.ts` 里，daemon 换过端口就要跟着改。

### `pnpm serve` 为什么是这个顺序

1. **先停旧进程**。Windows 上正被运行的 exe 不能覆盖，不停掉 `go build` 会报“拒绝访问”；
   而且不停掉的话新进程会因为端口被占自动 +1，浏览器却还停在旧地址。
   没有服务在跑时 `quit` 会以“连不上服务”退出，这是正常的，脚本不计为失败。
2. **界面构建排在编译之前**。`web/dist` 是在编译时被 `//go:embed` 打进二进制的，
   反过来就又是旧界面。
3. 最后前台起服务，stdio 直通，所以 Ctrl+C、横幅、日志跟直接跑 exe 一模一样。

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

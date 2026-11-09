// 一条命令跑完本地调试的一整串：停旧进程 → 构建界面 → 编译 → 起服务。
//
// 为什么顺序是这个：
//   1. **先 quit**。Windows 上正被运行的 exe 不能覆盖，不先停掉 go build 会报
//      “拒绝访问”。而且不先停，新进程会因为端口被占自动 +1，浏览器却还停在旧地址。
//   2. 界面构建要排在 go build 之前：`web/dist` 会在编译时被 `//go:embed` 打进二进制。
//   3. 最后前台起服务（Ctrl+C 退出）。
//
// 用法（在 web/ 下）：
//   pnpm serve              全套
//   pnpm serve --no-web     只改了 Go 时用，跳过 vite 构建，快很多
//   pnpm serve --no-tray    不起 Windows 托盘
//   pnpm serve --keep       不先 quit（旧进程还在的话端口会自动 +1）

import { spawn } from 'node:child_process'
import { existsSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const webDir = resolve(here, '..')
const root = resolve(webDir, '..')
const isWin = process.platform === 'win32'
const exe = join(root, 'dist', isWin ? 'kb-lite.exe' : 'kb-lite')

const argv = process.argv.slice(2)
const has = (name) => argv.includes(`--${name}`)

/** 跑一个命令并等它结束；失败就带着退出码收摊。 */
function run(cmd, args, opts = {}) {
  return new Promise((resolvePromise) => {
    // .cmd（pnpm.cmd）在 Windows 上必须走 shell 才能执行；真正的 .exe 不要走，
    // 免得路径里的空格被 shell 再解释一遍。
    const child = spawn(cmd, args, {
      stdio: 'inherit',
      cwd: opts.cwd ?? root,
      shell: Boolean(opts.shell),
    })
    child.on('error', (err) => {
      console.error(`起不来：${cmd} —— ${err.message}`)
      resolvePromise(1)
    })
    child.on('close', (code) => resolvePromise(code ?? 1))
  })
}

/** 跑一个命令，失败也不管（quit 在没有服务在跑时本来就非零）。 */
async function tryRun(cmd, args) {
  try {
    await run(cmd, args)
  } catch {
    /* 忽略 */
  }
}

// 1. 停掉旧进程。没在跑时 `quit` 会以“连不上服务”退出，这是正常的，不当失败。
if (!has('keep')) {
  if (existsSync(exe)) {
    process.stdout.write('· 停掉可能在跑的旧进程\n')
    await tryRun(exe, ['quit'])
  }
}

// 2. 构建界面（可选）
if (!has('no-web')) {
  process.stdout.write('· 构建界面\n')
  const code = await run(isWin ? 'pnpm.cmd' : 'pnpm', ['build'], { cwd: webDir, shell: isWin })
  if (code !== 0) {
    console.error('界面构建失败，先停在编译之前。')
    process.exit(code)
  }
}

// 3. 编译
process.stdout.write('· 编译 kb-lite\n')
const buildCode = await run('go', ['build', '-o', join('dist', isWin ? 'kb-lite.exe' : 'kb-lite'), './cmd/kb-lite'])
if (buildCode !== 0) {
  console.error('编译失败。')
  process.exit(buildCode)
}

// 4. 前台起服务。stdio 直通，所以 Ctrl+C、横幅、日志都跟直接跑一模一样。
process.stdout.write('· 启动\n\n')
const serveArgs = ['serve', '--open']
if (has('no-tray')) serveArgs.push('--no-tray')

const serve = spawn(exe, serveArgs, { stdio: 'inherit', cwd: root })
serve.on('close', (code) => process.exit(code ?? 0))

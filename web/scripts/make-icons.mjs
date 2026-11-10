// 从两张源图生成用得到图标的地方。
//
// 源文件（**这两个才是源，别的都是产物**）：
//   internal/tray/assets/normal.png   运行中
//   internal/tray/assets/warn.png     已暂停
//
// 产物：
//   web/public/favicon.png            浏览器标签页
//   website/favicon.png               宣传页（favicon 与顶欄品牌图标）
//   cmd/kb-lite/rsrc_windows_*.syso   exe 的图标与版本信息（go build 自动吃）
//
// 用法（在 web/ 下）：pnpm icons
//
// 为什么托盘图标不在这里切尺寸：托盘要按系统 DPI 取 16 / 20 / 24 / 32，
// 各机器不一样。保留 512 的原图，运行时按当前 DPI 缩（internal/tray/tray_windows.go）。
//
// 这一步只用 Node 标准库；唯一的外部工具是 go-winres，它只在生成 syso 时用，
// **不进 go.mod、不进二进制、CI 也不需要**（产物是提交进仓库的）。

import { copyFile, readFile, stat } from 'node:fs/promises'
import { spawn } from 'node:child_process'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { existsSync } from 'node:fs'

const here = dirname(fileURLToPath(import.meta.url))
const webDir = resolve(here, '..')
const root = resolve(webDir, '..')
const assets = join(root, 'internal', 'tray', 'assets')

const sources = [
  { name: 'normal.png', what: '运行中' },
  { name: 'warn.png', what: '已暂停' },
]

/** 从 PNG 头里读宽高，顺手确认它真的是 PNG。 */
function readPNGSize(buf, name) {
  const sig = [0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]
  for (let i = 0; i < sig.length; i++) {
    if (buf[i] !== sig[i]) throw new Error(`${name} 不是 PNG`)
  }
  const w = buf.readUInt32BE(16)
  const h = buf.readUInt32BE(20)
  return { w, h }
}

function run(cmd, args, opts = {}) {
  return new Promise((done) => {
    const isWinCmd = process.platform === 'win32' && cmd.endsWith('.cmd')
    const child = spawn(cmd, args, {
      stdio: 'inherit',
      cwd: opts.cwd ?? root,
      shell: isWinCmd,
    })
    child.on('error', () => done(127))
    child.on('close', (code) => done(code ?? 1))
  })
}

let failed = false

// 1. 检查源图。
for (const src of sources) {
  const path = join(assets, src.name)
  if (!existsSync(path)) {
    console.error(`缺少源图：${path}`)
    failed = true
    continue
  }
  const buf = await readFile(path)
  const { w, h } = readPNGSize(buf, src.name)
  const kb = (buf.length / 1024).toFixed(1)
  if (w !== h) {
    console.error(`${src.name} 是 ${w}x${h}，必须是正方形（不是正方形会被拉变形）`)
    failed = true
  } else if (w < 256) {
    console.error(`${src.name} 只有 ${w}px，至少要 256（缩到 16 之前要有足够细节）`)
    failed = true
  }
  console.log(`· 源图 ${src.name}（${src.what}）${w}x${h}，${kb} KB`)
}
if (failed) {
  console.error('\n源图有问题，先修好再跑。')
  process.exit(1)
}

// 2. 浏览器标签页与宣传页。直接给 512 的 PNG，浏览器自己缩；省掉一个有损的中间步骤。
await copyFile(join(assets, 'normal.png'), join(webDir, 'public', 'favicon.png'))
console.log('· 写好 web/public/favicon.png')
await copyFile(join(assets, 'normal.png'), join(root, 'website', 'favicon.png'))
console.log('· 写好 website/favicon.png')

// 3. exe 的图标与版本信息。
//
// go-winres 会生成 rsrc_windows_amd64.syso 与 rsrc_windows_arm64.syso，
// 文件名里的 `_windows_amd64` 后缀是 go build 认出“只在编这个平台时带上”的依据。
const rsrcDir = join(root, 'cmd', 'kb-lite')
const version = (await readFile(join(root, 'internal', 'daemon', 'version.go'), 'utf8')).match(
  /Version\s*=\s*"([^"]+)"/,
)?.[1]

if (!version) {
  console.error('读不到 internal/daemon/version.go 里的版本号，跳过 syso。')
  process.exit(1)
}

console.log('· 生成 exe 资源（go-winres，第一次会先下载）')
const code = await run('go', [
  'run', 'github.com/tc-hib/go-winres@latest', 'simply',
  '--icon', join(assets, 'normal.png'),
  '--arch', 'amd64,arm64',
  '--product-name', 'KBLite',
  '--file-description', 'kb-lite：把 markdown 目录变成只读 HTTP 知识库',
  '--product-version', version,
  '--file-version', version,
  '--original-filename', 'kb-lite.exe',
  // 注意这个参数是**文件名的前缀**，不是目录。
  '--out', join(rsrcDir, 'rsrc'),
])

if (code !== 0) {
  console.error(
    '\nsyso 没生成成。常见原因：没网（go run 要先下载 go-winres）、或者 Go 不在 PATH。\n' +
      '不影响其他产物：托盘图标与 favicon 已经写好，exe 会沿用上一次的图标。',
  )
  process.exit(code)
}

const made = ['rsrc_windows_amd64.syso', 'rsrc_windows_arm64.syso'].filter((f) =>
  existsSync(join(rsrcDir, f)),
)
for (const f of made) {
  const { size } = await stat(join(rsrcDir, f))
  console.log(`· 写好 cmd/kb-lite/${f}（${(size / 1024).toFixed(1)} KB）`)
}
if (made.length === 0) {
  console.error('go-winres 说成功了，但没找到 syso，检查一下 --out 指向哪儿。')
  process.exit(1)
}

// 4. 提醒一句最容易忘的：产物要跟着改。
console.log('\n图标已更新。syso 与 favicon.png 都要一起提交，否则别人拉下来还是旧图标。')

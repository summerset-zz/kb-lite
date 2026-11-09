// 把 web/dist 的产物拷到 internal/daemon/static/，并把 .gitkeep 原样留着。
//
// 为什么要单独一步：`//go:embed all:static` 要求那个目录非空，而仓库里只提交
// .gitkeep（产物不进版本库）。如果构建时把那里整个清空，下一个 clone 下来的人
// `go build` 就编不过。
//
// 只用 Node 标准库，不引依赖 —— 它只在开发者机器上跑一次。

import { cp, mkdir, readdir, readFile, rm, writeFile } from 'node:fs/promises'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const here = dirname(fileURLToPath(import.meta.url))
const src = resolve(here, '..', 'dist')
const target = resolve(here, '..', '..', 'internal', 'daemon', 'static')
const keep = join(target, '.gitkeep')

// 先把它读出来，稍后原样写回：内容必须与仓库里那份一致，否则构建完 git status 会脏。
let keepText = ''
try {
  keepText = await readFile(keep, 'utf8')
} catch {
  keepText = '这个目录放构建好的 UI（`web/dist` 的产物）。\n'
}

await rm(target, { recursive: true, force: true })
await mkdir(target, { recursive: true })
await cp(src, target, { recursive: true })
await writeFile(keep, keepText, 'utf8')

const entries = await readdir(target)
console.log(`已同步 ${entries.length} 项到 internal/daemon/static/（含 .gitkeep）`)

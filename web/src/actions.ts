/*
 * 两个到处都用的小动作：复制路径、打开文件位置。
 *
 * 收在一处是因为知识库那一行与 SKILL 那一行都要它，而失败时说的话必须一样。
 * 复制失败不能静默：用户会以为已经把路径贴到别处了。
 */
import { api, ApiError } from './api'
import { s } from './strings'
import { useToast } from './toast'

export async function copyPath(path: string): Promise<void> {
  const toast = useToast()
  if (!path) return
  try {
    await navigator.clipboard.writeText(path)
    toast.ok(s.common.pathCopied)
  } catch {
    toast.fail('没复制成。手动选中复制吧。')
  }
}

export async function revealPath(path: string): Promise<void> {
  const toast = useToast()
  if (!path) return
  try {
    await api.fsReveal(path)
  } catch (e) {
    toast.fail(e instanceof ApiError ? e.message : s.errors.generic)
  }
}

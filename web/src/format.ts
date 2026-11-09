/** 体积与时间的显示。界面上一律给“人话”，不露原始数字。 */

export function humanBytes(n: number): string {
  if (!n) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB']
  let i = 0
  let v = n
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024
    i += 1
  }
  return `${v >= 10 || i === 0 ? Math.round(v) : v.toFixed(1)} ${units[i]}`
}

export function humanTime(ms: number): string {
  const diff = Date.now() - ms
  if (diff < 45_000) return '刚刚'
  const min = Math.round(diff / 60_000)
  if (min < 60) return `${min} 分钟前`
  const hour = Math.round(min / 60)
  if (hour < 24) return `${hour} 小时前`
  const day = Math.round(hour / 24)
  if (day < 30) return `${day} 天前`
  return new Date(ms).toLocaleDateString('zh-CN')
}

package cli

import (
	"fmt"
	"strings"
)

// displayWidth 按**显示宽度**算列宽：CJK 算 2，其余算 1。
//
// docs/05 明确要求这个 —— 不这么算，`--explain` 和 `list` 的表格在中文标题上会歪。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		if isWideRune(r) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func isWideRune(r rune) bool {
	switch {
	case r >= 0x1100 && r <= 0x115F, // 谚文字母
		r >= 0x2E80 && r <= 0xA4CF, // CJK 部首 … 彝文
		r >= 0xAC00 && r <= 0xD7A3, // 谚文音节
		r >= 0xF900 && r <= 0xFAFF, // CJK 兼容表意
		r >= 0xFE30 && r <= 0xFE6F, // CJK 兼容形式
		r >= 0xFF00 && r <= 0xFF60, // 全角
		r >= 0xFFE0 && r <= 0xFFE6,
		r >= 0x1F300 && r <= 0x1F64F, // emoji
		r >= 0x1F900 && r <= 0x1F9FF:
		return true
	}
	return false
}

// padRight 按显示宽度补空格。
func padRight(s string, width int) string {
	if d := displayWidth(s); d < width {
		return s + strings.Repeat(" ", width-d)
	}
	return s
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

//go:build windows

package tray

import _ "embed"

// 托盘图标的两份源图，512×512 的 PNG，透明底。
//
// 为什么不预先切成 16×16 的几个尺寸：
// 托盘图标要按系统 DPI 取 16 或 20/24/32，机器之间不一样；这里保留原图，
// 运行时按当前 DPI 缩，高 DPI 屏才不会糊。
//
// 这两个文件是**源文件**，替换它们之后重跑 `pnpm icons`（在 web/ 下），
// 它会同步 web/public/favicon.png 与 exe 的图标资源。
//
//go:embed assets/normal.png
var iconNormalPNG []byte

//go:embed assets/warn.png
var iconWarnPNG []byte

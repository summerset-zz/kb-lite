// Package tray 是 Windows 托盘图标（docs/06 第四节）。
//
// 约束（ADR-014）：**纯 Go，不引入 cgo，不上 Tauri/Electron**。
// 全部走 syscall 调 user32 / shell32 / kernel32，所以五个平台里只有 Windows 有实现，
// 其他平台给空实现（`tray_other.go`），交叉编译照样过。
//
// 图标只有两态：服务在跑、服务暂停。不做“处理中”的第三态 —— Windows 上频繁重绘
// 托盘图标观感很差，进度属于窗口内的信息。
package tray

// State 是画菜单那一刻的实际情况。菜单每次右键都重新构建，所以不需要缓存。
type State struct {
	// Address 是能点开的地址，例如 http://127.0.0.1:7331。
	Address string
	// Paused 为真时菜单项写“启动服务”，否则写“暂停服务”。
	Paused bool
	// Indexing 为真时多一项“正在索引 120 / 530”，点击打开界面。
	Indexing bool
	Done     int
	Total    int
}

// Actions 是托盘能触发的动作。实现都在 cli 包，托盘只管把点击转成调用。
type Actions struct {
	TogglePause func()
	OpenBrowser func(url string)
	OpenConsole func()
	// CopyAddress 把地址写进剪贴板；返回写成功的文本（空表示失败）。
	CopyAddress func() string
	Quit        func()
}

// Menu 的命令 id。Windows 那边按这些 id 分发，改动时两边一起改。
const (
	idCopyAddress = 1
	idTogglePause = 2
	idProgress    = 3
	idSettings    = 4
	idConsole     = 5
	idQuit        = 6
)

//go:build windows

package tray

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// ---- Win32 常量 ----

const (
	wmApp           = 0x8000 // WM_APP
	wmTray          = wmApp + 1
	wmCommand       = 0x0111
	wmRButtonUp     = 0x0205
	wmLButtonDblClk = 0x0203
	wmDestroy       = 0x0002

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10

	tpmRightButton = 0x0002
	tpmReturnCmd   = 0x0100

	mfString    = 0x0000
	mfSeparator = 0x0800

	cfUnicodeText = 13
	gmemMoveable  = 0x0042

	lrDefaultColor = 0x0000
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW    = user32.NewProc("RegisterClassExW")
	procCreateWindowExW     = user32.NewProc("CreateWindowExW")
	procDefWindowProcW      = user32.NewProc("DefWindowProcW")
	procDestroyWindow       = user32.NewProc("DestroyWindow")
	procGetMessageW         = user32.NewProc("GetMessageW")
	procTranslateMessage    = user32.NewProc("TranslateMessage")
	procDispatchMessageW    = user32.NewProc("DispatchMessageW")
	procPostQuitMessage     = user32.NewProc("PostQuitMessage")
	procCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	procAppendMenuW         = user32.NewProc("AppendMenuW")
	procDestroyMenu         = user32.NewProc("DestroyMenu")
	procTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procGetCursorPos        = user32.NewProc("GetCursorPos")
	procCreateIconFromResEx = user32.NewProc("CreateIconFromResourceEx")
	procDestroyIcon         = user32.NewProc("DestroyIcon")
	procOpenClipboard       = user32.NewProc("OpenClipboard")
	procEmptyClipboard      = user32.NewProc("EmptyClipboard")
	procSetClipboardData    = user32.NewProc("SetClipboardData")
	procCloseClipboard      = user32.NewProc("CloseClipboard")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")
	procGlobalAlloc      = kernel32.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32.NewProc("GlobalUnlock")
	procGlobalFree       = kernel32.NewProc("GlobalFree")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procRtlMoveMemory    = kernel32.NewProc("RtlMoveMemory")
)

// ---- 结构体（amd64/arm64 对齐一致）----

type point struct{ x, y int32 }

type wndMsg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type notifyIconData struct {
	cbSize            uint32
	hWnd              uintptr
	uID               uint32
	uFlags            uint32
	uCallbackMessage  uint32
	hIcon             uintptr
	szTip             [128]uint16
	dwState           uint32
	dwStateMask       uint32
	szInfo            [256]uint16
	uTimeoutOrVersion uint32
	szInfoTitle       [64]uint16
	dwInfoFlags       uint32
	guidItem          [16]byte
	hBalloonIcon      uintptr
}

// Available 报告本平台有没有托盘。serve 据此决定是否挂。
func Available() bool { return true }

// mutexUpdate 串行化对托盘的写操作：菜单点击、定时刷新、退出可能撞在一起。
var (
	cfgMu sync.RWMutex
	state func() State
	act   Actions

	nid       notifyIconData
	hwnd      uintptr
	iconRun   uintptr
	iconPause uintptr
	lastTip   string
)

// Run 建托盘并进入消息循环。**阻塞**，返回即表示已经退出（用户点了“退出”或窗口被销毁）。
//
// 消息循环必须跑在锁定的 OS 线程上：Windows 的窗口属于创建它的线程，
// 换线程收消息会直接收不到。
func Run(stateFn func() State, actions Actions) error {
	state = stateFn
	act = actions

	errCh := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		errCh <- messageLoop()
	}()
	return <-errCh
}

func messageLoop() error {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	className, err := syscall.UTF16PtrFromString("KBLiteTrayWindow")
	if err != nil {
		return err
	}

	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     hInst,
		lpszClassName: className,
	}
	if r, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return fmt.Errorf("RegisterClassEx 失败：%v", e)
	}

	// 隐藏窗口：托盘图标必须挂在一个窗口上才收得到鼠标消息。
	h, _, e := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		0,
		0, 0, 0, 0, 0,
		0, 0, hInst, 0,
	)
	hwnd = h
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowEx 失败：%v", e)
	}

	iconRun = makeIcon(colorRun)
	iconPause = makeIcon(colorPause)
	if iconRun == 0 || iconPause == 0 {
		return errors.New("托盘图标创建失败")
	}

	if err := addIcon(); err != nil {
		return err
	}
	defer procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))

	// 提示文字要跟着状态变（索引进度、暂停）。图标本身不频繁重绘，只改 tooltip。
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				refreshTip()
			}
		}
	}()

	var m wndMsg
	for {
		r, _, e := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(r) {
		case -1: // 出错
			return fmt.Errorf("GetMessage 失败：%v", e)
		case 0: // WM_QUIT
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// wndProc 是窗口过程。只处理托盘回调与菜单命令，其余交给默认处理。
func wndProc(h uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmTray:
		switch uint32(lParam) {
		case wmLButtonDblClk:
			open("")
		case wmRButtonUp:
			showMenu(h)
		}
		return 0
	case wmCommand:
		handleCommand(uint32(wParam & 0xffff))
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(h, uintptr(msg), wParam, lParam)
	return r
}

// showMenu 构建并弹出右键菜单。菜单每次现建，内容永远等于此刻的状态。
//
// 必须 SetForegroundWindow + PostMessage(WM_NULL) 那一套吗？这里用更简单可靠的做法：
// 先把窗口提到前台再弹出，否则点空白处菜单不收，用户会觉得卡住了。
func showMenu(h uintptr) {
	st := currentState()

	menu, _, _ := procCreatePopupMenu.Call()
	if menu == 0 {
		return
	}
	defer procDestroyMenu.Call(menu)

	// 地址这一条可点：点了复制并弹气泡（docs/06 第四节：不做灰掉的 disable 项）。
	addr := st.Address
	if addr == "" {
		addr = "kb-lite 正在运行"
	}
	appendItem(menu, idCopyAddress, "已在 "+addr+" 运行", false)

	appendItem(menu, 0, "", true)
	toggleLabel := "暂停服务"
	if st.Paused {
		toggleLabel = "启动服务"
	}
	appendItem(menu, idTogglePause, toggleLabel, false)

	if st.Indexing {
		appendItem(menu, idProgress, fmt.Sprintf("正在索引 %d / %d", st.Done, st.Total), false)
	}

	appendItem(menu, idSettings, "设置", false)
	appendItem(menu, idConsole, "在命令行中打开", false)
	appendItem(menu, 0, "", true)
	appendItem(menu, idQuit, "退出", false)

	var pt point
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// 菜单弹出时窗口不在前台会导致点别处不收起，这是 TrackPopupMenu 的老毛病。
	procSetForegroundWindow.Call(h)
	cmd, _, _ := procTrackPopupMenu.Call(
		menu,
		tpmRightButton|tpmReturnCmd,
		uintptr(pt.x), uintptr(pt.y),
		0, h, 0,
	)
	if cmd != 0 {
		handleCommand(uint32(cmd))
	}
}

func appendItem(menu uintptr, id int, text string, separator bool) {
	if separator {
		procAppendMenuW.Call(menu, mfSeparator, 0, 0)
		return
	}
	p, err := syscall.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procAppendMenuW.Call(menu, mfString, uintptr(id), uintptr(unsafe.Pointer(p)))
}

func handleCommand(id uint32) {
	switch id {
	case idCopyAddress:
		if act.CopyAddress != nil {
			if got := act.CopyAddress(); got != "" {
				balloon("已复制", got)
			}
		}
	case idTogglePause:
		if act.TogglePause != nil {
			act.TogglePause()
		}
		refreshTip()
	case idProgress:
		open("")
	case idSettings:
		open("settings")
	case idConsole:
		if act.OpenConsole != nil {
			act.OpenConsole()
		}
	case idQuit:
		if act.Quit != nil {
			act.Quit()
		}
		procDestroyWindow.Call(hwnd)
	}
}

// open 打开界面。tab 为 "settings" 时带上锚点，前端按它选页。
func open(tab string) {
	if act.OpenBrowser == nil {
		return
	}
	url := currentState().Address
	if url == "" {
		return
	}
	if tab != "" {
		url += "/#" + tab
	}
	act.OpenBrowser(url)
}

func currentState() State {
	cfgMu.RLock()
	fn := state
	cfgMu.RUnlock()
	if fn == nil {
		return State{}
	}
	return fn()
}

// ---- 托盘图标本体 ----

func addIcon() error {
	cfgMu.Lock()
	st := State{}
	if state != nil {
		st = state()
	}
	icon := iconRun
	if st.Paused {
		icon = iconPause
	}
	tip := tipText(st)
	lastTip = tip
	cfgMu.Unlock()

	nid = notifyIconData{
		hWnd:             hwnd,
		uID:              1,
		uFlags:           nifMessage | nifIcon | nifTip,
		uCallbackMessage: wmTray,
		hIcon:            icon,
	}
	nid.cbSize = uint32(unsafe.Sizeof(nid))
	copy(nid.szTip[:], syscall.StringToUTF16(tip))

	if r, _, e := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		return fmt.Errorf("Shell_NotifyIcon 失败：%v", e)
	}
	return nil
}

// refreshTip 只在文字或图标真的变了时才发 NIM_MODIFY。
func refreshTip() {
	st := currentState()
	tip := tipText(st)
	icon := iconRun
	if st.Paused {
		icon = iconPause
	}

	cfgMu.Lock()
	changed := tip != lastTip || nid.hIcon != icon
	lastTip = tip
	cfgMu.Unlock()
	if !changed {
		return
	}

	cfgMu.Lock()
	nid.uFlags = nifTip | nifIcon
	nid.hIcon = icon
	nid.szTip = [128]uint16{}
	copy(nid.szTip[:], syscall.StringToUTF16(tip))
	cfgMu.Unlock()
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// balloon 弹一个一次性气泡（“已复制”这类），不打扰用户。
func balloon(title, text string) {
	cfgMu.Lock()
	nid.uFlags = nifInfo
	nid.szInfo = [256]uint16{}
	nid.szInfoTitle = [64]uint16{}
	copy(nid.szInfo[:], syscall.StringToUTF16(text))
	copy(nid.szInfoTitle[:], syscall.StringToUTF16(title))
	cfgMu.Unlock()
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

func tipText(st State) string {
	if st.Paused {
		return "KBLite · 服务已暂停"
	}
	if st.Indexing {
		return fmt.Sprintf("KBLite · 正在索引 %d / %d", st.Done, st.Total)
	}
	return "KBLite · " + st.Address
}

// ---- 图标位图 ----

type bgr struct{ b, g, r byte }

var (
	// 服务主色 #3f6f5f，暂停用中性灰。注意内存里是 BGRA，字段名就是按字节序写的。
	colorRun   = bgr{b: 0x5f, g: 0x6f, r: 0x3f}
	colorPause = bgr{b: 0x8f, g: 0x8f, r: 0x8f}
)

// makeIcon 在内存里画一个 16x16 的圆形图标，不依赖任何图片文件。
//
// ICONIMAGE 的结构是 BITMAPINFOHEADER + 反序的 32 位像素（BGRA）+ 1 位 AND 掩码。
// 用 CreateIconFromResourceEx 直接吃这块内存，省掉 .ico 资源与 GDI 那一套。
func makeIcon(c bgr) uintptr {
	const size = 16
	const headerSize = 40
	const maskRowBytes = 4 // 16 位深/行，按 4 字节对齐
	const maskSize = maskRowBytes * size
	const pixels = size * size * 4

	buf := make([]byte, headerSize+pixels+maskSize)

	put32 := func(off int, v uint32) {
		buf[off] = byte(v)
		buf[off+1] = byte(v >> 8)
		buf[off+2] = byte(v >> 16)
		buf[off+3] = byte(v >> 24)
	}
	put32(0, headerSize)       // biSize
	put32(4, size)             // biWidth
	put32(8, size*2)           // biHeight（颜色位图 + 掩码各占一半）
	put32(12, 1)               // biPlanes
	put32(14, 32)              // biBitCount
	put32(16, 0)               // biCompression = BI_RGB
	put32(20, pixels+maskSize) // biSizeImage

	// 像素：自下而上。圆外的 alpha 留 0，掩码全 0 表示“不透明的地方由 alpha 决定”。
	center := float64(size-1) / 2
	radius := center - 0.6
	for y := 0; y < size; y++ {
		row := size - 1 - y // 反序
		for x := 0; x < size; x++ {
			dx := float64(x) - center
			dy := float64(y) - center
			off := headerSize + (row*size+x)*4
			if dx*dx+dy*dy <= radius*radius {
				buf[off] = c.b
				buf[off+1] = c.g
				buf[off+2] = c.r
				buf[off+3] = 0xff
			}
		}
	}

	h, _, _ := procCreateIconFromResEx.Call(
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
		1, // fIcon
		0x00030000,
		size, size,
		lrDefaultColor,
	)
	return h
}

// CopyToClipboard 把文本写进剪贴板（托盘“复制地址”用）。
func CopyToClipboard(text string) bool {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return false
	}
	if r, _, _ := procOpenClipboard.Call(0); r == 0 {
		return false
	}
	defer procCloseClipboard.Call()
	procEmptyClipboard.Call()

	n := uintptr(len(utf16) * 2)
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, n)
	if h == 0 {
		return false
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		procGlobalFree.Call(h)
		return false
	}
	// 把 UTF-16 拷进那块全局内存。这里用 RtlMoveMemory 而不是把 uintptr 转成
	// unsafe.Pointer 再包一层切片：后者会被 go vet 判成 misuse（确实也是不安全的写法）。
	procRtlMoveMemory.Call(p, uintptr(unsafe.Pointer(&utf16[0])), n)
	procGlobalUnlock.Call(h)
	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h)
		return false
	}
	return true
}

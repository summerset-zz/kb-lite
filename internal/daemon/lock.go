package daemon

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// LockFileName 是单实例锁的文件名（docs/06 第二节）。
const LockFileName = "lock"

// LockInfo 是锁里记的东西。写 pid 是为了判断“上一个实例还活着吗”，
// 写 port 是为了重复启动时能直接把界面打开到**正在跑的那个**。
type LockInfo struct {
	PID       int    `json:"pid"`
	Port      int    `json:"port"`
	StartedAt int64  `json:"startedAt"`
	Exe       string `json:"exe"`
}

// LockPath 返回锁文件路径。
func LockPath(dir string) string { return filepath.Join(dir, LockFileName) }

// ReadLock 读锁。文件不存在或内容坏了都当成“没有锁”。
//
// 坏掉的锁不该拦住用户启动：删掉它比让程序起不来强。
func ReadLock(dir string) (*LockInfo, bool) {
	b, err := os.ReadFile(LockPath(dir))
	if err != nil {
		return nil, false
	}
	var info LockInfo
	if err := json.Unmarshal(b, &info); err != nil || info.PID <= 0 {
		return nil, false
	}
	return &info, true
}

// WriteLock 写锁（原子写：先写临时文件再改名）。
func WriteLock(dir string, info LockInfo) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.Marshal(info)
	if err != nil {
		return err
	}
	tmp := LockPath(dir) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, LockPath(dir))
}

// RemoveLock 删锁。只删自己的：pid 对不上就说明锁已经被别人接手了。
func RemoveLock(dir string) {
	info, ok := ReadLock(dir)
	if ok && info.PID != os.Getpid() {
		return
	}
	_ = os.Remove(LockPath(dir))
}

// AnotherInstance 判断“是不是已经有一个 kb-lite 在跑”，是的话把它的信息带回来。
//
// 判定用两个信号取或：pid 还活着，或者那个端口上真的有人答话。
// 只看 pid 会漏掉“pid 被系统复用”，只看端口会漏掉“刚启动还没监听”。
//
// 为什么需要这个：Windows 上正被运行的 exe 不能覆盖，而且两个实例会各自占一个端口
// （我们的端口自动 +1 会让第二个**静默成功**），托盘里出现两个图标，配置被反复改写。
// 用户双击两次、或者开机自启与手动打开撞在一起，都会走到这里。
func AnotherInstance(dir string) (*LockInfo, bool) {
	info, ok := ReadLock(dir)
	if !ok {
		return nil, false
	}
	if info.PID == os.Getpid() {
		return info, false // 自己的锁（比如热重开后重写），不算冲突
	}
	if processAlive(info.PID) {
		return info, true
	}
	if info.Port > 0 && portAnswers(info.Port) {
		return info, true
	}
	return info, false
}

// portAnswers 看这个端口上有没有 HTTP 服务在应答（不带凭据，401 也算“有人”）。
func portAnswers(port int) bool {
	// 不走系统代理：本机回环请求被代理带走会得到假阴性/假阳性。
	client := &http.Client{
		Timeout:   500 * time.Millisecond,
		Transport: &http.Transport{Proxy: nil},
	}
	resp, err := client.Get("http://127.0.0.1:" + strconv.Itoa(port) + "/api/status")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return true
}

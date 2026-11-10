package daemon

import (
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// currentUserName 是这个进程所属的系统用户名，只用来在界面上说一句
// “这个目录只有 <名字> 能访问”。
//
// 走环境变量而不是 os/user：`os/user` 在部分平台上要么依赖 cgo，要么要读
// /etc/passwd，而这里只需要一个给人看的称呼，环境变量足够。
func currentUserName() string {
	for _, k := range []string{"USERNAME", "USER", "LOGNAME"} {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			return v
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Base(home)
	}
	return ""
}

// lanAddrs 返回本机的局域网 IPv4 地址（去掉回环与链路本地）。
//
// 用来告诉用户“局域网里其他设备该用哪个地址访问”。拿不到就返回空，
// 界面退回一句不带具体地址的说法，而不是编一个。
func lanAddrs() []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	seen := map[string]bool{}
	out := make([]string, 0, 4)
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip := ipnet.IP.To4()
			if ip == nil || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
				continue
			}
			s := ip.String()
			if seen[s] {
				continue
			}
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

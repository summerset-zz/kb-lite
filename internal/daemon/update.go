package daemon

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ReleasesAPI 是检查更新时唯一会访问的外部地址。
//
// 为什么要写死：kb-lite 的卖点之一是“不联网”。这一条请求只在用户**主动点按钮**时发，
// 不在启动、不在后台定时、不顺手捎带。所以它必须是一个常量，且只有一处调用点。
const ReleasesAPI = "https://api.github.com/repos/summerset-zz/kb-lite/releases/latest"

// UpdateInfo 是 `GET /api/update` 的响应，也是 `kb-lite update` 的输出来源。
type UpdateInfo struct {
	OK      bool   `json:"ok"`
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	// Newer 为真表示远端比当前新。Dev 构建只在数字版本确实更旧时才算“有更新”。
	Newer bool `json:"newer"`
	// Dev 表示当前运行的是 `-dev` 构建（开发中的中间态）。
	Dev         bool   `json:"dev"`
	URL         string `json:"url,omitempty"`
	PublishedAt string `json:"publishedAt,omitempty"`
	CheckedAt   int64  `json:"checkedAt"`
	// Error 非空时 OK 为假：查不到不算故障，界面照常显示一句“这次没查到”。
	Error string `json:"error,omitempty"`
}

// updateTTL 是结果的复用时间。用户连点几下不该把 GitHub 的匿名配额打满
// （未登录 60 次/小时/IP）。
const updateTTL = 10 * time.Minute

var (
	updateMu    sync.Mutex
	updateCache *UpdateInfo
	updateAt    time.Time
)

// FetchUpdate 取远端最新版本。force 为真时忽略缓存（界面上的“重新检查”）。
func FetchUpdate(current string, force bool) UpdateInfo {
	info := UpdateInfo{Current: current, Dev: strings.Contains(current, "-")}

	updateMu.Lock()
	if !force && updateCache != nil && time.Since(updateAt) < updateTTL {
		cached := *updateCache
		updateMu.Unlock()
		cached.Current, cached.Dev = current, info.Dev
		cached.Newer = CompareVersions(cached.Latest, current) > 0
		return cached
	}
	updateMu.Unlock()

	info.CheckedAt = time.Now().UnixMilli()

	client := &http.Client{Timeout: 8 * time.Second}
	req, err := http.NewRequest(http.MethodGet, ReleasesAPI, nil)
	if err != nil {
		info.Error = err.Error()
		return info
	}
	// GitHub 的 API 要求带 User-Agent，不带直接 403。
	req.Header.Set("User-Agent", "kb-lite/"+current)
	req.Header.Set("Accept", "application/vnd.github+json")

	resp, err := client.Do(req)
	if err != nil {
		info.Error = "连不上 GitHub：" + err.Error()
		return info
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		info.Error = "GitHub 返回 " + resp.Status
		return info
	}

	var rel struct {
		TagName     string `json:"tag_name"`
		HTMLURL     string `json:"html_url"`
		PublishedAt string `json:"published_at"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		info.Error = "对方的响应读不出来：" + err.Error()
		return info
	}

	info.OK = true
	info.Latest = rel.TagName
	info.URL = rel.HTMLURL
	info.PublishedAt = rel.PublishedAt
	info.Newer = CompareVersions(rel.TagName, current) > 0

	updateMu.Lock()
	cache := info
	updateCache = &cache
	updateAt = time.Now()
	updateMu.Unlock()

	return info
}

// CompareVersions 比较两个版本号（可带 `v` 前缀与 `-dev` 之类的后缀）。
//
// 返回 1 表示 a 比 b 新，-1 表示旧，0 表示一样。
// 只按数字段比：`0.4.0-dev` 比 `0.3.9` 新，比 `0.4.0` 旧。
func CompareVersions(a, b string) int {
	an, apre := splitVersion(a)
	bn, bpre := splitVersion(b)
	for i := 0; i < 3; i++ {
		if an[i] != bn[i] {
			if an[i] > bn[i] {
				return 1
			}
			return -1
		}
	}
	// 数字一样：带后缀的算旧（0.4.0-dev < 0.4.0）。
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1
	case bpre == "":
		return -1
	default:
		return 0
	}
}

// splitVersion 把 "v0.3.0-dev" 拆成 [0,3,0] 与 "dev"。
func splitVersion(v string) ([3]int, string) {
	var out [3]int
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")

	pre := ""
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre = v[i+1:]
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		if n, err := strconv.Atoi(parts[i]); err == nil {
			out[i] = n
		}
	}
	return out, pre
}

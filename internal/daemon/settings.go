package daemon

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// 设置接口的错误（docs/04「POST /api/settings」）。
var (
	ErrInvalidPort = errors.New("端口必须在 1 到 65535 之间")
	ErrPortInUse   = errors.New("这个端口被别的软件占用了")
)

// SettingsPatch 是 `POST /api/settings` 的请求体，字段都可选，没传的不动。
//
// 这里**没有**记录文件位置：它永远和 config.json 放在一起（ADR-018）。
type SettingsPatch struct {
	Port         *int    `json:"port"`
	Host         *string `json:"host"`
	RequireToken *bool   `json:"requireToken"`
	// SetupCompleted 由向导最后一步置真；设置页的“重新运行向导”置假。
	SetupCompleted *bool `json:"setupCompleted"`
}

// ApplySettings 落盘设置，并告诉调用方还有哪些改动要重启才生效。
//
// 访问密码可以**立刻生效**（Handler 里是原子开关）；端口与绑定地址由 Handler 当场
// 重开监听（`HandlerOptions.Reload`）。这一份里没有“换记录文件位置”这种事了：
// 缓存永远住在配置目录里。
func (m *Manager) ApplySettings(p SettingsPatch) (restart bool, err error) {
	if p.Port != nil && (*p.Port < 1 || *p.Port > 65535) {
		return false, ErrInvalidPort
	}

	portChanged := false
	err = m.UpdateConfig(func(c *Config) error {
		// 先在一份副本上把所有校验跑完，**全部通过才落到 c 上**。
		// 否则端口已经写进内存、目录探测再失败，配置就与磁盘不一致了（docs/06 §5.4 第 5 步）。
		next := *c

		probeHost := strings.TrimSpace(next.Host)
		if probeHost == "" {
			probeHost = DefaultHost
		}
		if p.Port != nil && *p.Port != next.Port {
			if !portFree(probeHost, *p.Port) {
				return fmt.Errorf("%w：%d", ErrPortInUse, *p.Port)
			}
			next.Port = *p.Port
			portChanged = true
		}
		if p.Host != nil && *p.Host != next.Host {
			h := strings.TrimSpace(*p.Host)
			if h == "" {
				h = DefaultHost
			}
			next.Host = h
			portChanged = true // 监听地址变了也要重开
		}
		if p.RequireToken != nil {
			next.RequireToken = *p.RequireToken
		}
		if p.SetupCompleted != nil {
			next.SetupCompleted = *p.SetupCompleted
		}
		// 手写的配置可能没有 host：先补默认，后面的探测与判定才有意义。
		if strings.TrimSpace(next.Host) == "" {
			next.Host = DefaultHost
		}
		// 硬不变式（ADR-015）：绑了非回环地址就必须要密码。这里直接改回真，不报错，
		// 因为用户的本意显然不是“把自己的知识库公开给局域网”。
		if !IsLoopbackHost(next.Host) {
			next.RequireToken = true
		}

		*c = next
		return nil
	})
	if err != nil {
		return false, err
	}
	return portChanged, nil
}

// portFree 试绑一次，能绑就说明没被占。
//
// 这中间有个很短的竞态窗口（放开到真正监听之间可能被别人抢），可接受：
// 真正监听失败时 serve 会自动 +1 并写回配置，用户看到的是新地址。
func portFree(host string, port int) bool {
	ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// ---- 安装 SKILL ----

// SkillTarget 是一份 SKILL 要装到哪。
type SkillTarget struct {
	// Kind 是 read（教 agent 怎么用）、authoring（教 AI 怎么写）或 maintain（增删改登记）。
	Kind string `json:"kind"`
	// Target 是 dsh 或 claude。
	Target string `json:"target"`
	// Path 是最终写出的文件。
	Path string `json:"path"`
	// Bytes 是写入的字节数。
	Bytes int `json:"bytes"`
}

// SkillInstallResult 是 `POST /api/skill/install` 的响应。
type SkillInstallResult struct {
	Installed []SkillTarget `json:"installed"`
}

// skillRoots 返回两个目标目录的根（`~/.agents/skills` 与 `~/.claude/skills`）。
//
// 只认这两个位置：写别的地方要么用户找不到，要么我们不该碰。
func skillRoots() (dsh, claude string, err error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", "", err
	}
	return filepath.Join(home, agentsHomeDirName, "skills"),
		filepath.Join(home, ".claude", "skills"), nil
}

const agentsHomeDirName = ".agents"

// skillDir 返回某个 (kind, target) 要写到哪个目录。
//
// 两个目标目录互不兼容：Claude Code 只认 `~/.claude/skills`，不读 `~/.agents/skills`。
// 两者用的是**同一套文件格式**（SKILL.md + frontmatter），所以要各装一份，不是共享一份。
func skillDir(kind, target string) (root, dirName string, err error) {
	if kind != "read" && kind != "authoring" && kind != "maintain" {
		return "", "", errors.New("不认识的 skill 种类：" + kind)
	}
	dshRoot, claudeRoot, err := skillRoots()
	if err != nil {
		return "", "", err
	}
	switch target {
	case "dsh":
		root = dshRoot
	case "claude":
		root = claudeRoot
	default:
		return "", "", errors.New("不认识的安装目标：" + target)
	}
	dirName = "kb-lite"
	switch kind {
	case "authoring":
		dirName = "kb-lite-authoring"
	case "maintain":
		dirName = "kb-lite-maintain"
	}
	return root, dirName, nil
}

// SkillFilePath 是某个 (kind, target) 对应的 SKILL.md 路径（不碰磁盘）。
func SkillFilePath(kind, target string) (string, error) {
	root, dirName, err := skillDir(kind, target)
	if err != nil {
		return "", err
	}
	return filepath.Join(root, dirName, "SKILL.md"), nil
}

// SkillState 是一个安装目标的现状。
//
// `UpToDate` 比的是**文件内容与现在应该装的那份是否逐字相同**：
// 地址、密码、说明书本身只要变了一样，装过的那份就过时了。
type SkillState struct {
	Kind      string `json:"kind"`
	Target    string `json:"target"`
	Path      string `json:"path"`
	Installed bool   `json:"installed"`
	UpToDate  bool   `json:"upToDate"`
	// ModifiedAt 是文件的修改时间（毫秒），没装就是 0。
	ModifiedAt int64 `json:"modifiedAt"`
}

// SkillStateFor 看一个目标现在是什么情况。expected 是此刻应该装的内容。
func SkillStateFor(kind, target, expected string) (SkillState, error) {
	p, err := SkillFilePath(kind, target)
	if err != nil {
		return SkillState{}, err
	}
	st := SkillState{Kind: kind, Target: target, Path: p}
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return st, nil // 没装不是错
		}
		return st, err
	}
	st.Installed = true
	st.UpToDate = string(b) == expected
	if info, err := os.Stat(p); err == nil {
		st.ModifiedAt = info.ModTime().UnixMilli()
	}
	return st, nil
}

// InstallSkill 把一份渲染好的文本写到 agent 的 skill 目录。
//
// 写入前把旧文件备份成 `.bak`，失败就还原 —— 用户手上那份 SKILL 可能是他改过的，
// 不能因为一次写入失败就没了。
func InstallSkill(kind, target, content string) (SkillTarget, error) {
	root, dirName, err := skillDir(kind, target)
	if err != nil {
		return SkillTarget{}, err
	}
	return writeSkillFile(root, dirName, content)
}

func writeSkillFile(root, dirName, content string) (SkillTarget, error) {
	dir := filepath.Join(root, dirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return SkillTarget{}, err
	}
	p := filepath.Join(dir, "SKILL.md")

	var backup string
	if old, err := os.ReadFile(p); err == nil {
		backup = p + ".bak"
		if err := os.WriteFile(backup, old, 0o644); err != nil {
			return SkillTarget{}, err
		}
	}

	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		if backup != "" {
			// 还原，别把用户原来那份弄没。
			if old, rerr := os.ReadFile(backup); rerr == nil {
				_ = os.WriteFile(p, old, 0o644)
			}
		}
		return SkillTarget{}, err
	}
	_ = os.Chtimes(p, time.Now(), time.Now())
	return SkillTarget{Path: p, Bytes: len(content)}, nil
}

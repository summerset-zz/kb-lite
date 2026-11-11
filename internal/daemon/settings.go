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

// Harness 是一个会读 skill 的 AI 工具，以及它从哪个目录读。
//
// 每个 harness 的目录约定都不一样，**没有任何一个目录是通用的**：
// WorkBuddy 只读 `$WORKBUDDY_CONFIG_DIR/skills`（默认 `~/.workbuddy/skills`），
// 明确不扫 `~/.agents/skills`；Claude Code 只读 `~/.claude/skills`。
// 所以这里是一个注册表，不是一个“通用目录”。
type Harness struct {
	// ID 是内部标识，CLI 的 --target 与接口的 targets 都用它。
	ID string
	// SkillsRoot 返回这个 harness 的 skills 根目录。
	SkillsRoot func() (string, error)
	// Detected 判断本机有没有发现这个 harness。只看目录，尽力而为：
	// 环境变量被改到别处、便携版、刚装还没跑过，都会漏判，所以它只影响默认显示，
	// 不影响允不允许装。
	Detected func() (bool, error)
}

const agentsHomeDirName = ".agents"

// workbuddyConfigEnv 是 WorkBuddy 自己用的环境变量。它的用户级 skill 目录就从这里来，
// 不认这个变量就会在改过配置目录的机器上永远探测不到。
const workbuddyConfigEnv = "WORKBUDDY_CONFIG_DIR"

// Harnesses 是认识的 AI 工具，顺序就是界面上的顺序。
var Harnesses = []Harness{
	{ID: "dsh", SkillsRoot: func() (string, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, agentsHomeDirName, "skills"), nil
	}, Detected: func() (bool, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return false, err
		}
		return dirExists(filepath.Join(home, agentsHomeDirName))
	}},
	{ID: "claude", SkillsRoot: func() (string, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".claude", "skills"), nil
	}, Detected: func() (bool, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return false, err
		}
		return dirExists(filepath.Join(home, ".claude"))
	}},
	{ID: "workbuddy", SkillsRoot: func() (string, error) {
		dir, err := workbuddyConfigDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(dir, "skills"), nil
	}, Detected: func() (bool, error) {
		dir, err := workbuddyConfigDir()
		if err != nil {
			return false, err
		}
		return dirExists(dir)
	}},
}

// workbuddyConfigDir 先看它自己的环境变量，再退回默认目录。
func workbuddyConfigDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(workbuddyConfigEnv)); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".workbuddy"), nil
}

func dirExists(p string) (bool, error) {
	info, err := os.Stat(p)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	return info.IsDir(), nil
}

// HarnessIDs 返回全部 harness 的 ID（接口与 CLI 用它遍历）。
func HarnessIDs() []string {
	out := make([]string, 0, len(Harnesses))
	for _, h := range Harnesses {
		out = append(out, h.ID)
	}
	return out
}

// harnessByID 按 ID 找一个 harness。
func harnessByID(id string) (Harness, bool) {
	for _, h := range Harnesses {
		if h.ID == id {
			return h, true
		}
	}
	return Harness{}, false
}

// skillDir 返回某个 (kind, target) 要写到哪个目录。
//
// **安装不检查 harness 装没装**：探测必然有假阴性，用户明确知道自己用什么工具时，
// 不能因为界面没认出来就把他挡在门外。
func skillDir(kind, target string) (root, dirName string, err error) {
	if kind != "read" && kind != "authoring" && kind != "maintain" {
		return "", "", errors.New("不认识的 skill 种类：" + kind)
	}
	h, ok := harnessByID(target)
	if !ok {
		return "", "", errors.New("不认识的安装目标：" + target)
	}
	root, err = h.SkillsRoot()
	if err != nil {
		return "", "", err
	}
	// 目录名与 SKILL.md 里的 `name` 必须一致：harness 靠目录名与 frontmatter 认技能，
	// 两边不一样的时候，有的工具会报重名、有的会同时列出两个。
	// 三个都带 kbl- 前缀，一眼看得出是同一套东西。
	switch kind {
	case "authoring":
		dirName = "kbl-authoring"
	case "maintain":
		dirName = "kbl-maintain"
	default:
		dirName = "kbl-query"
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
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Path   string `json:"path"`
	// Detected 表示本机发现了这个 harness（只看目录，尽力而为）。
	// 为 false **不代表不能装**：探测会漏（环境变量指到别处、便携版、刚装还没跑过）。
	Detected  bool `json:"detected"`
	Installed bool `json:"installed"`
	UpToDate  bool `json:"upToDate"`
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
	// 探测失败当成“没发现”：它只影响界面怎么显示，不影响能不能装。
	if h, ok := harnessByID(target); ok {
		if found, err := h.Detected(); err == nil {
			st.Detected = found
		}
	}
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

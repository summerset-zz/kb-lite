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
	ErrDirNotWrite = errors.New("这个目录不能写入")
)

// SettingsPatch 是 `POST /api/settings` 的请求体，字段都可选，没传的不动。
type SettingsPatch struct {
	Port         *int    `json:"port"`
	Host         *string `json:"host"`
	DBPath       *string `json:"dbPath"`
	RequireToken *bool   `json:"requireToken"`
	// SetupCompleted 由向导最后一步置真；设置页的“重新运行向导”置假。
	SetupCompleted *bool `json:"setupCompleted"`
}

// ApplySettings 落盘设置，并告诉调用方哪些改动要重启才生效。
//
// 访问密码可以**立刻生效**（Handler 里是原子开关）；端口、绑定地址、记录文件位置
// 都要重新监听或换库，所以只落盘 + 回一个 restartRequired，让 UI 提示用户。
// 这一版的取舍写进了 docs/04。
func (m *Manager) ApplySettings(p SettingsPatch) (restart bool, err error) {
	if p.Port != nil && (*p.Port < 1 || *p.Port > 65535) {
		return false, ErrInvalidPort
	}

	portChanged := false
	err = m.UpdateConfig(func(c *Config) error {
		probeHost := strings.TrimSpace(c.Host)
		if probeHost == "" {
			probeHost = DefaultHost
		}
		if p.Port != nil && *p.Port != c.Port {
			if !portFree(probeHost, *p.Port) {
				return fmt.Errorf("%w：%d", ErrPortInUse, *p.Port)
			}
			c.Port = *p.Port
			portChanged = true
		}
		if p.Host != nil && *p.Host != c.Host {
			h := strings.TrimSpace(*p.Host)
			if h == "" {
				h = DefaultHost
			}
			c.Host = h
			portChanged = true // 监听地址变了也要重开
		}
		if p.DBPath != nil && *p.DBPath != c.DBPath {
			if err := probeWritable(*p.DBPath); err != nil {
				return err
			}
			c.DBPath = *p.DBPath
		}
		if p.RequireToken != nil {
			c.RequireToken = *p.RequireToken
		}
		if p.SetupCompleted != nil {
			c.SetupCompleted = *p.SetupCompleted
		}
		// 手写的配置可能没有 host：先补默认，后面的探测与判定才有意义。
		if strings.TrimSpace(c.Host) == "" {
			c.Host = DefaultHost
		}
		// 硬不变式（ADR-015）：绑了非回环地址就必须要密码。这里直接改回真，不报错，
		// 因为用户的本意显然不是“把自己的知识库公开给局域网”。
		if !IsLoopbackHost(c.Host) {
			c.RequireToken = true
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return portChanged || (p.DBPath != nil), nil
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

// probeWritable 在目标目录建一个临时文件写完就删。
//
// **不能**真的建 `index.db`：Windows 上正被 daemon 拿着（docs/06 的保存校验第 2 步）。
func probeWritable(dbPath string) error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ErrDirNotWrite
	}
	f, err := os.CreateTemp(dir, ".kb-lite-probe-*")
	if err != nil {
		return ErrDirNotWrite
	}
	name := f.Name()
	_, werr := f.WriteString("ok")
	cerr := f.Close()
	_ = os.Remove(name)
	if werr != nil || cerr != nil {
		return ErrDirNotWrite
	}
	return nil
}

// ---- 安装 SKILL ----

// SkillTarget 是一份 SKILL 要装到哪。
type SkillTarget struct {
	// Kind 是 read（教 agent 怎么用）或 authoring（教 AI 怎么写）。
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

// InstallSkill 把一份渲染好的文本写到 agent 的 skill 目录。
//
// 写入前把旧文件备份成 `.bak`，失败就还原 —— 用户手上那份 SKILL 可能是他改过的，
// 不能因为一次写入失败就没了。
func InstallSkill(kind, target, content string) (SkillTarget, error) {
	dshRoot, claudeRoot, err := skillRoots()
	if err != nil {
		return SkillTarget{}, err
	}
	var root, dir string
	switch target {
	case "dsh":
		root, dir = dshRoot, "kb-lite"
	case "claude":
		root, dir = claudeRoot, "kb-lite"
	default:
		return SkillTarget{}, errors.New("不认识的安装目标：" + target)
	}
	if kind == "authoring" {
		dir = "kb-lite-authoring"
	}
	return writeSkillFile(root, dir, content)
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

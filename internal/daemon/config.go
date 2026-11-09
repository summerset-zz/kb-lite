package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	// ConfigFileName 是配置文件名（docs/01「配置文件」）。
	ConfigFileName = "config.json"
	// ReadTokenFileName 是读取面的访问密码（界面叫“访问密码”），32 字节随机 hex，0600。
	ReadTokenFileName = "read-token"
	// APITokenFileName 是管理面的凭据。UI 靠 HttpOnly cookie 拿，界面不显示（ADR-015）。
	APITokenFileName = "api-token"
	// legacyTokenFileName 是 v0.2 及以前的单凭据文件名。读到它就迁成 read-token。
	legacyTokenFileName = "token"
	// SchemaVersion 是 config.json 的版本。v2 加了 host / requireToken / setupCompleted。
	SchemaVersion = 2
	// DefaultPort 是默认端口；被占时自动 +1（docs/04）。
	DefaultPort = 7331
	// DefaultHost 是默认绑定地址。只绑回环地址时读取面才可以不要密码。
	DefaultHost = "127.0.0.1"
	// DBFileName 是默认的缓存文件名。
	DBFileName = "index.db"

	// ConfigDirEnv 是配置目录的逃生口：测试用它，将来 `--config` 也可以用它。
	// 正常运行时**不设**，走各平台的标准位置。
	ConfigDirEnv = "KB_LITE_CONFIG_DIR"
)

// KB 是一个被索引的目录。
//
// 对外标识用 Name（对人和 agent 都友好：`curl .../kb/我的笔记/...`），
// 内部用 ID。改名只改 Name，ID 不动，索引无需重建。
type KB struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	RootPath string   `json:"rootPath"`
	Enabled  bool     `json:"enabled"`
	Ignore   []string `json:"ignore,omitempty"`
	Include  []string `json:"include,omitempty"`
}

// UnmarshalJSON 让 `enabled` 缺省为 true。
//
// 用默认的 bool 零值会让「手写 config.json 时忘了 enabled」变成「这个库被关掉了」，
// 那是个很难查的故障（接口全是 503）。
func (k *KB) UnmarshalJSON(b []byte) error {
	var raw struct {
		ID       string   `json:"id"`
		Name     string   `json:"name"`
		RootPath string   `json:"rootPath"`
		Enabled  *bool    `json:"enabled"`
		Ignore   []string `json:"ignore"`
		Include  []string `json:"include"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	k.ID, k.Name, k.RootPath = raw.ID, raw.Name, raw.RootPath
	k.Ignore, k.Include = raw.Ignore, raw.Include
	k.Enabled = raw.Enabled == nil || *raw.Enabled
	return nil
}

// Config 是 config.json 的结构。
//
// KB 列表与配置在 config.json，**不在数据库里** —— 数据库是可重建的缓存（ADR-003）。
type Config struct {
	SchemaVersion int `json:"schemaVersion"`
	// SetupCompleted 为 false 时 UI 进向导（docs/06 5.1）。
	SetupCompleted bool `json:"setupCompleted"`
	Port           int  `json:"port"`
	// Host 是监听地址。非回环地址时 requireToken 强制为真（ADR-015）。
	Host string `json:"host"`
	// RequireToken 是“读取面要不要密码”。新装默认为假；从 v1 迁过来的为真。
	RequireToken bool   `json:"requireToken"`
	DBPath       string `json:"dbPath"`
	KBs          []KB   `json:"kbs"`
}

// IsLoopbackHost 判断是不是只绑本机。
func IsLoopbackHost(h string) bool {
	switch strings.ToLower(strings.TrimSpace(h)) {
	case "", "127.0.0.1", "localhost", "::1", "[::1]":
		return true
	}
	return false
}

// EffectiveRequireToken 是**实际生效**的读取密码开关。
//
// 绑了非回环地址就一定为真：否则等于把知识库公开给整个局域网（ADR-015 的硬不变式）。
func (c *Config) EffectiveRequireToken() bool {
	return c.RequireToken || !IsLoopbackHost(c.Host)
}

// ConfigDir 返回各平台的配置目录。
func ConfigDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv(ConfigDirEnv)); v != "" {
		return v, nil
	}
	switch runtime.GOOS {
	case "windows":
		base := os.Getenv("APPDATA")
		if base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, "AppData", "Roaming")
		}
		return filepath.Join(base, "kb-lite"), nil
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Application Support", "kb-lite"), nil
	default:
		if v := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); v != "" {
			return filepath.Join(v, "kb-lite"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".config", "kb-lite"), nil
	}
}

// DefaultConfig 是首次运行时的配置（还不落盘）。
func DefaultConfig(dir string) *Config {
	return &Config{
		SchemaVersion: SchemaVersion,
		Port:          DefaultPort,
		Host:          DefaultHost,
		// 新装默认不要密码（ADR-015）。绑局域网时 EffectiveRequireToken 会强制为真。
		RequireToken: false,
		DBPath:       filepath.Join(dir, DBFileName),
	}
}

// Load 读 config.json；文件不存在时返回默认配置，**不写盘**。
func Load(dir string) (*Config, error) {
	p := filepath.Join(dir, ConfigFileName)
	b, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(dir), nil
		}
		return nil, err
	}
	var c Config
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s 解析失败：%w", p, err)
	}

	migrated := false
	if c.SchemaVersion < SchemaVersion {
		// v1 只有一把 token，且读取面**强制**要密码。迁移时必须保持“要密码”，
		// 否则升级会默默把用户的门打开。用户想关自己去设置里关。
		c.RequireToken = true
		c.SetupCompleted = true // 已经在用了，不要再弹向导
		c.SchemaVersion = SchemaVersion
		migrated = true
	}
	if c.Port == 0 {
		c.Port = DefaultPort
	}
	if c.Host == "" {
		c.Host = DefaultHost
	}
	if c.DBPath == "" {
		c.DBPath = filepath.Join(dir, DBFileName)
	}
	if migrated {
		// 迁移后立刻落盘：否则下次启动又会走一遍迁移，而用户此间的改动会丢。
		_ = c.Save(dir)
	}
	return &c, nil
}

// Save 原子地写 config.json（临时文件 + rename），避免写一半断电留下半个 JSON。
func (c *Config) Save(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	p := filepath.Join(dir, ConfigFileName)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// LoadOrCreateReadToken 读 `<dir>/read-token`；没有就生成。
//
// 会把 v0.2 的旧文件 `<dir>/token` 迁过来（直接改名，不丢用户已配好的 agent）。
func LoadOrCreateReadToken(dir string) (string, error) {
	return loadOrCreateToken(dir, ReadTokenFileName, legacyTokenFileName)
}

// LoadOrCreateAPIToken 读 `<dir>/api-token`；没有就生成。
//
// 只有 UI 与 CLI 用它，**界面上不展示**（ADR-015）。
func LoadOrCreateAPIToken(dir string) (string, error) {
	return loadOrCreateToken(dir, APITokenFileName, "")
}

func loadOrCreateToken(dir, name, migrateFrom string) (string, error) {
	p := filepath.Join(dir, name)
	if b, err := os.ReadFile(p); err == nil {
		if tok := strings.TrimSpace(string(b)); tok != "" {
			return tok, nil
		}
	} else if !os.IsNotExist(err) {
		return "", err
	}

	if migrateFrom != "" {
		old := filepath.Join(dir, migrateFrom)
		if _, err := os.Stat(old); err == nil {
			// 改名失败也无所谓：下面还能读旧文件。
			_ = os.Rename(old, p)
			if b, err := os.ReadFile(p); err == nil {
				if tok := strings.TrimSpace(string(b)); tok != "" {
					return tok, nil
				}
			}
		}
	}

	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(buf)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(p, []byte(tok+"\n"), 0o600); err != nil {
		return "", err
	}
	// Windows 上 Chmod 基本是空操作，非 Windows 上是真的收紧权限。
	_ = os.Chmod(p, 0o600)
	return tok, nil
}

// NewKBID 生成 `k` + 8 位 hex 的短 id（docs/01 的 `k1f3a9c2`）。
func NewKBID() (string, error) {
	buf := make([]byte, 4)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "k" + hex.EncodeToString(buf), nil
}

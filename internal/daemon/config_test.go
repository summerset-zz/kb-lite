package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingReturnsDefaults(t *testing.T) {
	dir := t.TempDir()
	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.Port != DefaultPort || c.SchemaVersion != SchemaVersion {
		t.Errorf("默认配置 = %+v", c)
	}
	if _, err := os.Stat(filepath.Join(dir, ConfigFileName)); !os.IsNotExist(err) {
		t.Error("Load 不该写盘")
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := DefaultConfig(dir)
	c.KBs = []KB{{
		ID: "k1f3a9c2", Name: "我的笔记", RootPath: `D:\notes`,
		Enabled: true, Ignore: []string{"_drafts"},
	}}
	if err := c.Save(dir); err != nil {
		t.Fatal(err)
	}

	got, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.KBs) != 1 || got.KBs[0].Name != "我的笔记" || !got.KBs[0].Enabled {
		t.Fatalf("读回 = %+v", got.KBs)
	}
}

func TestEnabledDefaultsToTrue(t *testing.T) {
	dir := t.TempDir()
	// 手写一份缺 enabled 的 config.json —— 这是最容易出事的情况。
	writeFile(t, filepath.Join(dir, ConfigFileName),
		`{"schemaVersion":1,"port":7331,"kbs":[{"id":"k1","name":"甲","rootPath":"C:\\notes"}]}`)

	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !c.KBs[0].Enabled {
		t.Error("缺省 enabled 应当是 true，否则接口会静默变 503")
	}

	// 显式 false 要保住。
	writeFile(t, filepath.Join(dir, ConfigFileName),
		`{"schemaVersion":1,"kbs":[{"id":"k1","name":"甲","rootPath":"C:\\notes","enabled":false}]}`)
	c, err = Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.KBs[0].Enabled {
		t.Error("显式 enabled:false 被覆盖了")
	}
}

func TestLoadBrokenConfig(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ConfigFileName), `{not json`)
	if _, err := Load(dir); err == nil {
		t.Error("坏 JSON 应当报错，而不是静默用默认值")
	}
}

func TestToken(t *testing.T) {
	dir := t.TempDir()

	first, err := LoadOrCreateReadToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Errorf("token 长度 = %d，想要 64（32 字节 hex）", len(first))
	}

	second, err := LoadOrCreateReadToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Error("第二次调用应当复用已有 token")
	}

	info, err := os.Stat(filepath.Join(dir, ReadTokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	// Windows 上 Chmod 基本是空操作，这里只断言非 Windows 的权限位。
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("token 权限 = %v，想要 0600", info.Mode().Perm())
	}
}

// TestAPITokenIsSeparate 锁住 ADR-015：两个凭据必须是两把钥匙。
func TestAPITokenIsSeparate(t *testing.T) {
	dir := t.TempDir()
	read, err := LoadOrCreateReadToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	api, err := LoadOrCreateAPIToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if read == api {
		t.Error("读取密码与管理凭据不能是同一个值")
	}
	if _, err := os.Stat(filepath.Join(dir, APITokenFileName)); err != nil {
		t.Errorf("api-token 没落盘：%v", err)
	}
}

// TestLegacyTokenMigrates 锁住 v0.2 的 `token` 会被改名成 `read-token`。
func TestLegacyTokenMigrates(t *testing.T) {
	dir := t.TempDir()
	const old = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	if err := os.WriteFile(filepath.Join(dir, legacyTokenFileName), []byte(old+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := LoadOrCreateReadToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != old {
		t.Errorf("迁移后的 token = %q，想要沿用旧值（否则已配好的 agent 全断）", got)
	}
}

// TestConfigMigrationKeepsTokenOn 是升级安全的关键一条：
// v1 是“读取面强制要密码”，迁移到 v2 不能把这道门悄悄打开。
func TestConfigMigrationKeepsTokenOn(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, ConfigFileName),
		`{"schemaVersion":1,"port":7331,"kbs":[{"id":"k1","name":"甲","rootPath":"C:\\notes"}]}`)

	c, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if c.SchemaVersion != SchemaVersion {
		t.Errorf("schemaVersion = %d，想要 %d", c.SchemaVersion, SchemaVersion)
	}
	if !c.RequireToken {
		t.Error("从 v1 迁移过来必须保持“要密码”")
	}
	if !c.SetupCompleted {
		t.Error("已有配置的用户不该再被弹一次向导")
	}
	if c.Host != DefaultHost {
		t.Errorf("host = %q，想要 %q", c.Host, DefaultHost)
	}

	// 迁移要落盘：否则下次启动又迁一遍，中间的用户改动会丢。
	raw, err := os.ReadFile(filepath.Join(dir, ConfigFileName))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"schemaVersion": 2`) {
		t.Errorf("迁移没有写回：%s", raw)
	}
}

// TestFreshConfigHasNoToken 锁住“新装默认不要密码”。
func TestFreshConfigHasNoToken(t *testing.T) {
	c, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if c.RequireToken {
		t.Error("新装默认不该要密码")
	}
	if c.SetupCompleted {
		t.Error("新装应当进向导")
	}
	if c.EffectiveRequireToken() {
		t.Error("只有本机时，生效值应当是“不要密码”")
	}
}

// TestLanForcesToken 锁住硬不变式：绑了非回环地址就必须有密码。
func TestLanForcesToken(t *testing.T) {
	c := DefaultConfig(t.TempDir())
	c.Host = "0.0.0.0"
	if !c.EffectiveRequireToken() {
		t.Error("绑 0.0.0.0 时访问密码必须强制为开")
	}
	for _, h := range []string{"127.0.0.1", "localhost", "::1", ""} {
		c.Host = h
		if !IsLoopbackHost(h) {
			t.Errorf("%q 应当被当成回环地址", h)
		}
	}
}

func TestNewKBID(t *testing.T) {
	id, err := NewKBID()
	if err != nil {
		t.Fatal(err)
	}
	if len(id) != 9 || id[0] != 'k' {
		t.Errorf("id = %q，想要 k + 8 位 hex", id)
	}
	if other, _ := NewKBID(); other == id {
		t.Error("两次生成的 id 相同")
	}
}

func TestConfigDirHonorsEnv(t *testing.T) {
	want := t.TempDir()
	t.Setenv(ConfigDirEnv, want)
	got, err := ConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("ConfigDir() = %q，想要 %q", got, want)
	}
}

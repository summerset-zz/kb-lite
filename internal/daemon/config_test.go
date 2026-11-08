package daemon

import (
	"os"
	"path/filepath"
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

	first, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 64 {
		t.Errorf("token 长度 = %d，想要 64（32 字节 hex）", len(first))
	}

	second, err := LoadOrCreateToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Error("第二次调用应当复用已有 token")
	}

	info, err := os.Stat(filepath.Join(dir, TokenFileName))
	if err != nil {
		t.Fatal(err)
	}
	// Windows 上 Chmod 基本是空操作，这里只断言非 Windows 的权限位。
	if info.Mode().Perm()&0o077 != 0 && os.PathSeparator == '/' {
		t.Errorf("token 权限 = %v，想要 0600", info.Mode().Perm())
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

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestHarnessDetection 守「每个 harness 的目录各不相同，探测只是尽力而为」。
//
// 探测结果只影响界面默认显示什么，**不决定允不允许装**：环境变量被改到别处、
// 便携版、刚装还没跑过，都会漏判。所以这里除了探测本身，还断言“没探测到也能装”。
func TestHarnessDetection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)        // macOS / Linux
	t.Setenv("USERPROFILE", home) // Windows
	t.Setenv(workbuddyConfigEnv, "")

	for _, h := range Harnesses {
		if found, err := h.Detected(); err != nil {
			t.Fatalf("%s 探测报错：%v", h.ID, err)
		} else if found {
			t.Errorf("空目录里不该发现 %s", h.ID)
		}
	}

	// 建出各自的目录。
	for _, dir := range []string{".agents", ".claude", ".workbuddy"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, h := range Harnesses {
		found, err := h.Detected()
		if err != nil || !found {
			t.Errorf("建了目录之后应该发现 %s（found=%v err=%v）", h.ID, found, err)
		}
	}

	// skills 根目录：三个 harness 必须是三条不同的路径，不能有“通用目录”。
	seen := map[string]string{}
	for _, h := range Harnesses {
		root, err := h.SkillsRoot()
		if err != nil {
			t.Fatalf("%s：%v", h.ID, err)
		}
		if prev, dup := seen[root]; dup {
			t.Errorf("%s 与 %s 的 skills 目录相同（%s），说明有一个不是它真正读的位置", h.ID, prev, root)
		}
		seen[root] = h.ID
	}
	if got, _ := harnessByID("workbuddy"); got.ID != "workbuddy" {
		t.Fatal("注册表里没有 workbuddy")
	}
	wb, _ := harnessByID("workbuddy")
	if root, err := wb.SkillsRoot(); err != nil || root != filepath.Join(home, ".workbuddy", "skills") {
		t.Errorf("workbuddy 默认目录 = %q（err=%v），想要 ~/.workbuddy/skills", root, err)
	}

	// 它自己的环境变量优先。
	custom := t.TempDir()
	t.Setenv(workbuddyConfigEnv, custom)
	if root, err := wb.SkillsRoot(); err != nil || root != filepath.Join(custom, "skills") {
		t.Errorf("设了 %s 之后目录 = %q（err=%v），想要 %q",
			workbuddyConfigEnv, root, err, filepath.Join(custom, "skills"))
	}
	if found, err := wb.Detected(); err != nil || !found {
		t.Errorf("环境变量指向的目录存在时应当发现 workbuddy（found=%v err=%v）", found, err)
	}
}

// 没探测到的 harness 照样能装：探测会漏，不能因为没认出来就把用户挡在门外。
func TestInstallWorksWithoutDetection(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv(workbuddyConfigEnv, "")

	h, ok := harnessByID("workbuddy")
	if !ok {
		t.Fatal("注册表里没有 workbuddy")
	}
	if found, _ := h.Detected(); found {
		t.Fatal("这个用例要的是“没探测到”的状态")
	}

	root, dir, err := skillDir("maintain", "workbuddy")
	if err != nil {
		t.Fatalf("没探测到也应当算得出安装路径：%v", err)
	}
	if root == "" || dir != "kbl-maintain" {
		t.Errorf("root=%q dir=%q", root, dir)
	}

	// 状态里能看出“没检测到”，但 installed/upToDate 照旧按文件判断。
	st, err := SkillStateFor("maintain", "workbuddy", "内容")
	if err != nil {
		t.Fatal(err)
	}
	if st.Detected {
		t.Error("没装 harness 时 detected 应当是 false")
	}
	if st.Installed {
		t.Error("还没写文件，installed 应当是 false")
	}

	// 真装一次，路径要落在它自己的目录下。
	got, err := InstallSkill("maintain", "workbuddy", "内容")
	if err != nil {
		t.Fatalf("没探测到时安装失败：%v", err)
	}
	wantSuffix := filepath.Join(".workbuddy", "skills", "kbl-maintain", "SKILL.md")
	if !strings.HasSuffix(got.Path, wantSuffix) {
		t.Errorf("装到了 %q，想要以 %q 结尾", got.Path, wantSuffix)
	}
}

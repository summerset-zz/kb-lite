package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 模板与 docs/skill-template.md 必须逐字一致。
//
// 这是 docs/04「同源」要求的落地：改了接口却忘了改 skill，比没有 skill 更糟。
// 注意比较前要去掉 `{{#if X}}` / `{{/if}}` 门控行 —— 那是实现细节，不该出现在规格书里。
func TestSkillTemplateMatchesDocs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "skill-template.md"))
	if err != nil {
		t.Fatalf("读不到 docs/skill-template.md：%v", err)
	}
	want := strings.TrimRight(extractFencedBlock(normalizeNewlines(string(raw)), "```markdown"), "\n")
	if want == "" {
		t.Fatal("docs/skill-template.md 里找不到 ```markdown 围栏块")
	}

	// 尾部的空行不算契约的一部分。
	got := strings.TrimRight(stripGates(normalizeNewlines(skillTemplate)), "\n")
	if got != want {
		t.Errorf("skill_template.md 与 docs/skill-template.md 不一致。\n"+
			"先改文档再改模板（或反过来），两边必须同一份文本。\n"+
			"模板 %d 字节 / %d 行；文档 %d 字节 / %d 行\n--- 模板尾 ---\n%q\n--- 文档尾 ---\n%q\n综合差异：%s",
			len(got), strings.Count(got, "\n")+1,
			len(want), strings.Count(want, "\n")+1,
			tail(got, 60), tail(want, 60), firstDiff(got, want))
	}
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func TestRenderSkillGatesManifest(t *testing.T) {
	values := map[string]string{
		"BASE_URL":  "http://127.0.0.1:7331",
		"AUTH":      "",
		"AUTH_NOTE": "不需要密码",
	}

	on := renderSkill(skillTemplate, values, map[string]bool{"manifest": true})
	if !strings.Contains(on, "/manifest?budget=2000") {
		t.Error("manifest 功能打开时，文档里应当有 manifest 的用法")
	}

	off := renderSkill(skillTemplate, values, nil)
	if strings.Contains(off, "/manifest") {
		t.Error("manifest 没实现时，文档里不能出现它 —— 那会让 agent 调用一个不存在的接口")
	}
	// 关掉之后剩下的部分仍然完整。
	if !strings.Contains(off, "/entry/ops/runbook.md/toc") {
		t.Error("门控不该连「看目录」那段一起吃掉")
	}

	// 占位符必须全部被替换掉（条件块里的也算）。
	for _, leftover := range []string{"{{BASE_URL}}", "{{AUTH}}", "{{AUTH_NOTE}}", "{{#if", "{{/if}}"} {
		if strings.Contains(off, leftover) {
			t.Errorf("渲染后仍残留 %s", leftover)
		}
	}
}

// 说明书要带一节排错：用户连不上时的第一反应是问 AI，而 AI 该能说出“接下来谁做什么”。
// 这个测试同时锁住两件事：四种典型症状都在，以及 release 链接没被顺手删掉。
func TestSkillHasTroubleshooting(t *testing.T) {
	text := RenderSkillText(nil, "http://127.0.0.1:7331", "", nil)

	for _, want := range []string{
		"连不上怎么办",
		"Failed to connect", // 没启动
		"端口对不上",             // 端口错了
		"unauthorized",      // 缺密码（与 guard 里的 code 一致）
		"paused",            // 服务被暂停（同上）
		"https://github.com/summerset-zz/kb-lite/releases",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("说明书里少了 %q", want)
		}
	}
}
func TestSkillTextHasNoKBList(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "a.md"), "正文\n")
	// 名字故意取一个不会出现在样例里的：样例里有“我的笔记”，不能用它判断。
	mgr := NewManager(&Config{KBs: []KB{{ID: "k1", Name: "独一无二的库名", RootPath: root, Enabled: true}}},
		BuildOptions{Workers: 1})
	if err := mgr.Rebuild(); err != nil {
		t.Fatal(err)
	}

	text := RenderSkillText(mgr.Index(), "http://127.0.0.1:7331", "", DefaultFeatures)
	if strings.Contains(text, "独一无二的库名") {
		t.Error("说明书里不该写具体知识库的名字：让 agent 每次 GET /kb 自己取")
	}
	if !strings.Contains(text, "/kb\"") {
		t.Error("说明书里应当告诉 agent 先 GET /kb")
	}
}

func TestHumanBytes(t *testing.T) {
	cases := map[int64]string{
		512:             "512 B",
		2048:            "2.0 KB",
		2201398:         "2.1 MB",
		5 * 1024 * 1024: "5.0 MB",
	}
	for in, want := range cases {
		if got := humanBytes(in); got != want {
			t.Errorf("humanBytes(%d) = %q，想要 %q", in, got, want)
		}
	}
}

// ---- 小工具 ----

// stripGates 去掉 `{{#if X}}` 与 `{{/if}}` 所在的行。
func stripGates(s string) string {
	lines := strings.Split(s, "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "{{#if ") || trimmed == "{{/if}}" {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}

// firstDiff 返回两段文本第一处不同的上下文，方便定位。
func firstDiff(a, b string) string {
	al := strings.Split(a, "\n")
	bl := strings.Split(b, "\n")
	for i := 0; i < len(al) || i < len(bl); i++ {
		var av, bv string
		if i < len(al) {
			av = al[i]
		}
		if i < len(bl) {
			bv = bl[i]
		}
		if av != bv {
			return "第 " + itoa(i+1) + " 行：\n  " + av + "\n  " + bv
		}
	}
	return "（没有差异）"
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

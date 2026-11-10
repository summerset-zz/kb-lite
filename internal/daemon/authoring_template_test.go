package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 写作模板与 docs/authoring-template.md 必须逐字一致。
//
// 它会被 /api/skill/install 写到用户的 skill 目录里，改动必须两边同步，
// 否则“界面上装的”和“文档里说的”就不是同一份东西。
func TestAuthoringTemplateMatchesDocs(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "docs", "authoring-template.md"))
	if err != nil {
		t.Fatalf("读不到 docs/authoring-template.md：%v", err)
	}
	want := strings.TrimRight(normalizeNewlines(string(raw)), "\n")
	got := strings.TrimRight(normalizeNewlines(authoringTemplate), "\n")
	if got != want {
		t.Errorf("authoring_template.md 与 docs/authoring-template.md 不一致（模板 %d 字节 / 文档 %d 字节）",
			len(got), len(want))
	}
}

// TestAuthoringTemplateFeatureGating 守“不存在的接口不能出现在说明书里”。
//
// 整理侧的 agent 会照着说明去调 check。如果这个部署没有那条路由，它只会一直撞 404，
// 而错误看起来像“我写错了”。所以那段必须由 features 门控。
func TestAuthoringTemplateFeatureGating(t *testing.T) {
	on := RenderAuthoringText("http://127.0.0.1:7331", "", DefaultFeatures)
	if !strings.Contains(on, "/check") {
		t.Error("默认功能表里有 check，说明书就该提到它")
	}
	for _, leftover := range []string{"{{#if", "{{/if}}", "{{AUTH}}", "{{AUTH_NOTE}}", "{{BASE_URL}}"} {
		if strings.Contains(on, leftover) {
			t.Errorf("渲染后仍残留 %s", leftover)
		}
	}
	// 门控只该吃掉这一段，前后的步骤都要在。
	for _, want := range []string{"一批一批写", "让用户刷新索引"} {
		if !strings.Contains(on, want) {
			t.Errorf("门控把 %q 一起吞掉了", want)
		}
	}

	off := RenderAuthoringText("http://127.0.0.1:7331", "", nil)
	if strings.Contains(off, "/check") {
		t.Error("check 关掉后，说明书里不该出现这条接口")
	}
	if strings.Contains(off, "{{#if") || strings.Contains(off, "{{/if}}") {
		t.Error("条件块没被处理掉")
	}
}

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

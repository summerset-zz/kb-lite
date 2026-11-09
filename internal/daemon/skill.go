package daemon

import (
	_ "embed"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

//go:embed skill_template.md
var skillTemplate string

// DefaultFeatures 是「这个版本已经实现了哪些可选接口」。
//
// 只有**真的存在路由**的才能出现在 /skill.md 里（docs/04 末段：加了参数忘了改 skill，
// 比没有 skill 更糟；反过来写了不存在的接口，agent 会一直撞 404）。
var DefaultFeatures = map[string]bool{"manifest": true}

// handleSkill 渲染 `GET /skill.md`（docs/04 末段 + docs/skill-template.md）。
//
// 「必须与实现同源」这条在这里落成两件事：
//  1. 可选功能用 `{{#if NAME}}` 门控 —— **没注册的路由不会出现在文档里**。
//     v0.1 没有 manifest（它在 docs/09 里排在 v0.2），所以那段现在渲染不出来；
//     v0.2 加上路由、把 Features["manifest"] 打开，它自动出现。
//  2. skill_template_test.go 把这份模板与 docs/skill-template.md 逐字对齐 ——
//     改一边不改另一边会直接红。加了参数忘了改 skill，比没有 skill 更糟。
func (h *Handler) handleSkill(w http.ResponseWriter, r *http.Request) {
	base := "http://" + r.Host
	if r.Host == "" {
		base = "http://127.0.0.1:" + strconv.Itoa(h.opts.Port)
	}
	out := RenderSkillText(h.mgr.Index(), base, h.token, h.opts.Features)

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}

// RenderSkillText 渲染 `/skill.md` 的正文。HTTP 与离线 CLI 共用这一份。
func RenderSkillText(idx *Index, base, token string, features map[string]bool) string {
	return renderSkill(skillTemplate, map[string]string{
		"BASE_URL":  base,
		"TOKEN":     token,
		"KB_LIST":   kbListText(idx),
		"KB_SAMPLE": url.PathEscape(sampleKBName(idx)),
	}, features)
}

// renderSkill 先处理 `{{#if X}}…{{/if}}`，再替换 `{{KEY}}`。
//
// 顺序不能反：条件块里也有占位符，反过来会留下没被替换的 `{{TOKEN}}`。
func renderSkill(tmpl string, values map[string]string, flags map[string]bool) string {
	out := renderConditionals(tmpl, flags)
	for k, v := range values {
		out = strings.ReplaceAll(out, "{{"+k+"}}", v)
	}
	return out
}

func renderConditionals(s string, flags map[string]bool) string {
	const open, closeTag = "{{#if ", "{{/if}}"

	var b strings.Builder
	for {
		i := strings.Index(s, open)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		b.WriteString(s[:i])

		j := strings.Index(s[i:], "}}")
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		name := strings.ToUpper(strings.TrimSpace(s[i+len(open) : i+j]))
		bodyStart := i + j + 2

		k := strings.Index(s[bodyStart:], closeTag)
		if k < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		if flags[name] || flags[strings.ToLower(name)] {
			b.WriteString(s[bodyStart : bodyStart+k])
		}
		s = s[bodyStart+k+len(closeTag):]
	}
}

// kbListText 生成 `- 我的笔记（128 篇，2.1 MB）` 这种清单。
func kbListText(idx *Index) string {
	if len(idx.KBs) == 0 {
		return "（还没有可用的知识库）"
	}
	lines := make([]string, 0, len(idx.KBs))
	for _, kb := range idx.KBs {
		lines = append(lines, fmt.Sprintf("- %s（%d 篇，%s）", kb.KB.Name, len(kb.Entries), humanBytes(kb.Bytes)))
	}
	return strings.Join(lines, "\n")
}

func sampleKBName(idx *Index) string {
	if len(idx.KBs) == 0 {
		return "示例"
	}
	return idx.KBs[0].KB.Name
}

func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

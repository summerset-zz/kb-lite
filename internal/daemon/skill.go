package daemon

import (
	_ "embed"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

//go:embed skill_template.md
var skillTemplate string

//go:embed authoring_template.md
var authoringTemplate string

//go:embed maintain_template.md
var maintainTemplate string

// RenderAuthoringText 返回“教 AI 怎么写”的那份说明。
//
// 它也要带服务地址与鉴权：整理完得知道**写到哪个目录**（`GET /kb` 的 rootPath），
// 而那是要请求服务的。不带地址的版本只能让用户自己拿主意，那是把难题还给他。
//
// features 与 /skill.md 同一套 `{{#if NAME}}` 门控：没注册的接口不能出现在文档里，
// 否则 agent 会照着说明一直撞 404（docs/04 末段）。
//
// 与 /skill.md **分成两份**（ADR-017）：合成一份后，agent 在只想查东西的时候
// 也会照写作规范去动用户的文件。
func RenderAuthoringText(base, token string, features map[string]bool) string {
	auth, authNote := authValues(token)
	// 模板文件里前面有一段写给维护者的说明，真正的 SKILL.md 在 ```markdown 围栏里。
	// 这里把围栏里的那份取出来当正文 —— 装到用户目录的文件必须**第一行就是 `---`**，
	// 否则 harness 认不出这是 skill（WorkBuddy 就是这么报的：缺 frontmatter）。
	body := extractFencedBlock(normalizeNewlines(authoringTemplate), "```markdown")
	if body == "" {
		// 围栏被改坏了也不能把一份空文件写到用户目录里。
		body = authoringTemplate
	}
	return renderSkill(body, map[string]string{
		"BASE_URL":  base,
		"AUTH":      auth,
		"AUTH_NOTE": authNote,
	}, features)
}

// normalizeNewlines 把 CRLF 折成 LF。模板在仓库里是 CRLF，比较与提取前先统一。
func normalizeNewlines(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "\n")
}

// extractFencedBlock 取 `fence`（如 "```markdown"）那一段围栏的内容，不含围栏行。
//
// 注意块**内部**还有 ```bash / ```json 这些内层围栏：只有「开带有语言标识、关是光秃兀的」
// 那一对才算内层，其余的光秃 ``` 就是外层结束。不区分的话会在第一个 ```bash 结束时被截断。
func extractFencedBlock(s, fence string) string {
	lines := strings.Split(s, "\n")

	start := -1
	for i, line := range lines {
		if line == fence {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}

	inInner := false
	for i := start; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(line, "```") {
			if inInner {
				inInner = false
				continue
			}
			if line != "```" {
				inInner = true
				continue
			}
			return strings.Join(lines[start:i], "\n")
		}
	}
	return ""
}

// RenderMaintainText 返回第三份说明书的正文：教 agent 增删改知识库登记。
//
// 与另外两份不同，它**没有任何占位符**：维护走 `kb-lite` 命令行，CLI 自己从配置目录
// 读管理凭据，所以不需要服务地址，也不需要访问密码。
// 管理凭据永远不进文档（ADR-015），这也是它只能这么写的原因。
func RenderMaintainText() string {
	return maintainTemplate
}

// DefaultFeatures 是「这个版本已经实现了哪些可选接口」。
//
// 只有**真的存在路由**的才能出现在两份说明书里（docs/04 末段：加了参数忘了改 skill，
// 比没有 skill 更糟；反过来写了不存在的接口，agent 会一直撞 404）。
var DefaultFeatures = map[string]bool{
	"manifest": true,
	// `POST /kb/{name}/check`：写之前自查。authoring.md 第 4 步靠它。
	"check": true,
}

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
		base = "http://127.0.0.1:" + strconv.Itoa(h.Port())
	}
	// 读取密码没开就不把密码写进文档：那是给用户看的纸，不该多一个用不上的东西。
	tok := ""
	if h.requireToken.Load() {
		tok = h.ReadToken()
	}
	out := RenderSkillText(h.mgr.Index(), base, tok, h.opts.Features)

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(out))
}

// handleAuthoring 输出 `GET /authoring.md`（教 AI 怎么写的那份说明）。
//
// 与 `/skill.md` 平级、分开取（ADR-017）：UI 的完成页与设置页各给一个“复制”按钮。
// 它同样属于读取面，开了访问密码就要带密码。
func (h *Handler) handleAuthoring(w http.ResponseWriter, r *http.Request) {
	base := "http://" + r.Host
	if r.Host == "" {
		base = "http://127.0.0.1:" + strconv.Itoa(h.Port())
	}
	tok := ""
	if h.requireToken.Load() {
		tok = h.ReadToken()
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(RenderAuthoringText(base, tok, h.opts.Features)))
}

// handleMaintain 输出 `GET /maintain.md`（教 agent 增删改知识库登记的那份）。
//
// 与另外两份平级、分开取（ADR-017）。正文里没有占位符：维护走 CLI，
// CLI 自己带管理凭据，所以这份文档本身不需要地址与密码。
// 它仍然属于读取面，开了访问密码就要带密码。
func (h *Handler) handleMaintain(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte(RenderMaintainText()))
}

// RenderSkillText 渲染 `/skill.md` 的正文。HTTP 与离线 CLI 共用这一份。//
// `idx` 已经用不上了（库列表改成让 agent 自己 `GET /kb` 取），但保留参数：
// 调用方本来就拿着 Index，保留它免得以后要加回一点点动态信息时再改一圈。
//
// token 为空表示读取面不需要访问密码。这时 `{{AUTH}}` 渲染成空、`{{AUTH_NOTE}}`
// 换成一句“不需要密码”。**不能**留一个空的 `Bearer `：agent 会以为自己的头写错了。
func RenderSkillText(idx *Index, base, token string, features map[string]bool) string {
	auth, authNote := authValues(token)
	return renderSkill(skillTemplate, map[string]string{
		"BASE_URL":  base,
		"AUTH":      auth,
		"AUTH_NOTE": authNote,
	}, features)
}

// authValues 把 token 翻成两个占位符的值。两份文档共用，口径必须一致。
func authValues(token string) (auth, authNote string) {
	if token == "" {
		return "", "这个服务没有启用访问密码，直接请求即可。"
	}
	return `-H "Authorization: Bearer ` + token + `" `,
		fmt.Sprintf("每个请求都要带 `-H \"Authorization: Bearer %s\"`", token)
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

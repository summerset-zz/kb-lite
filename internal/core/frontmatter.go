package core

import (
	"strings"

	"gopkg.in/yaml.v3"
)

// Frontmatter 是文件开头那段 YAML 里我们认识的键。
//
// 其余键不解释，但整个对象原样留在 Raw 里 —— 将来要加字段不用改解析器。
type Frontmatter struct {
	Title   string
	Summary string
	// Tags 既接受 YAML 数组，也接受 `tags: ops, 上线` 这种逗号串。
	Tags []string
	// Skip 表示这一篇整篇跳过：draft: true / publish: false / kb_ignore: true。
	Skip bool
	// Raw 是原样的 YAML 对象。
	Raw map[string]any
}

// SplitFrontmatter 把文件拆成 frontmatter 与正文。
//
// 只认**第一行就是 `---`** 的情况（docs/02-slicing.md「Frontmatter」）。
//
// 两个刻意的行为：
//   - 没有闭合的 `---`：当成没有 frontmatter，整份原样返回（那几行很可能只是分隔线）；
//   - YAML 解析失败：**也原样返回整份**，并给一条 warn。不能静默吞掉用户内容。
//
// body 是 frontmatter 之后的部分（不含分隔线），未做任何清洗。
func SplitFrontmatter(src string) (fm Frontmatter, body, warn string) {
	lines := splitLines(src)
	if len(lines) == 0 || strings.TrimRight(lines[0], " \t") != "---" {
		return Frontmatter{}, src, ""
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		switch strings.TrimSpace(lines[i]) {
		case "---", "...":
			end = i
		}
		if end >= 0 {
			break
		}
	}
	if end < 0 {
		return Frontmatter{}, src, ""
	}

	raw := strings.Join(lines[1:end], "\n")
	var obj map[string]any
	if err := yaml.Unmarshal([]byte(raw), &obj); err != nil {
		// 手写 frontmatter 里「值中间出现未加引号的 `:`」太常见（`in: a TTF`），
		// 严格模式会把整篇降级成「没有 frontmatter」，白白丢掉标题与摘要。
		// 所以先试按行宽松解析，拿不到任何键才真的放弃。
		obj = lenientParse(raw)
		if obj == nil {
			return Frontmatter{}, src, "frontmatter 解析失败，已当作正文保留：" + err.Error()
		}
		warn = "frontmatter 不是合法 YAML，已按行宽松解析：" + err.Error()
	}

	fm = Frontmatter{
		Title:   strings.TrimSpace(firstYAMLString(obj, "title", "name")),
		Summary: strings.TrimSpace(firstYAMLString(obj, "summary", "description")),
		Tags:    firstYAMLTags(obj, "tags", "keywords"),
		Raw:     obj,
	}
	if yamlBool(obj["draft"]) || yamlBool(obj["kb_ignore"]) {
		fm.Skip = true
	}
	if b, ok := yamlBoolValue(obj["publish"]); ok && !b {
		fm.Skip = true
	}
	return fm, strings.Join(lines[end+1:], "\n"), warn
}

// firstYAMLString 按顺序取第一个非空字符串（第一个是主键，其余是别名）。
func firstYAMLString(obj map[string]any, keys ...string) string {
	for _, k := range keys {
		if s := yamlString(obj[k]); strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func firstYAMLTags(obj map[string]any, keys ...string) []string {
	for _, k := range keys {
		if tags := yamlTags(obj[k]); len(tags) > 0 {
			return tags
		}
	}
	return nil
}

// lenientParse 是 strict YAML 失败后的回退：只认顶格的 `key: value`。
//
// 值取该行剩下部分（去首尾引号），缩进行（嵌套对象 / 块序列）一律忽略。
// 一个键都拿不到时返回 nil，表示这次真的救不回来。
func lenientParse(raw string) map[string]any {
	obj := make(map[string]any)
	for _, line := range splitLines(raw) {
		if line == "" || line[0] == ' ' || line[0] == '\t' {
			continue // 空行 / 缩进行
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		i := strings.IndexByte(line, ':')
		if i <= 0 {
			continue
		}
		key := strings.TrimSpace(line[:i])
		if !isYAMLKey(key) {
			continue
		}
		obj[key] = strings.Trim(strings.TrimSpace(line[i+1:]), `"'`)
	}
	if len(obj) == 0 {
		return nil
	}
	return obj
}

func isYAMLKey(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		ok := r == '_' || r == '-' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9')
		if !ok {
			return false
		}
	}
	return true
}

func yamlString(v any) string {
	s, _ := v.(string)
	return s
}

// yamlBool 接受 true 与 "true" —— 宽松解析出来的值全是字符串。
func yamlBool(v any) bool {
	b, _ := yamlBoolValue(v)
	return b
}

func yamlBoolValue(v any) (bool, bool) {
	switch t := v.(type) {
	case bool:
		return t, true
	case string:
		switch strings.ToLower(strings.TrimSpace(t)) {
		case "true", "yes", "on":
			return true, true
		case "false", "no", "off":
			return false, true
		}
	}
	return false, false
}

// yamlTags 归一化 tags：数组逐项 trim，字符串按逗号切分，丢掉空项。
func yamlTags(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		var out []string
		for _, part := range strings.Split(t, ",") {
			// 每一项都要去引号：`keywords: "@font-face", src` 这种写法很常见。
			if p := strings.Trim(strings.TrimSpace(part), `"'`); p != "" {
				out = append(out, p)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s := strings.TrimSpace(yamlString(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	case []string:
		out := make([]string, 0, len(t))
		for _, s := range t {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

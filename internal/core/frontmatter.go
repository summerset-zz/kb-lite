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

	var obj map[string]any
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &obj); err != nil {
		return Frontmatter{}, src, "frontmatter 解析失败，已当作正文保留：" + err.Error()
	}

	fm = Frontmatter{
		Title:   strings.TrimSpace(yamlString(obj["title"])),
		Summary: strings.TrimSpace(yamlString(obj["summary"])),
		Tags:    yamlTags(obj["tags"]),
		Raw:     obj,
	}
	if yamlBool(obj["draft"]) || yamlBool(obj["kb_ignore"]) {
		fm.Skip = true
	}
	if v, ok := obj["publish"].(bool); ok && !v {
		fm.Skip = true
	}
	return fm, strings.Join(lines[end+1:], "\n"), ""
}

func yamlString(v any) string {
	s, _ := v.(string)
	return s
}

func yamlBool(v any) bool {
	b, _ := v.(bool)
	return b
}

// yamlTags 归一化 tags：数组逐项 trim，字符串按逗号切分，丢掉空项。
func yamlTags(v any) []string {
	switch t := v.(type) {
	case nil:
		return nil
	case string:
		var out []string
		for _, part := range strings.Split(t, ",") {
			if p := strings.TrimSpace(part); p != "" {
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

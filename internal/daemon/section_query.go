package daemon

import (
	"strconv"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// SectionQuery 是一次「读一节」的请求。
type SectionQuery struct {
	// N 不为 nil 时优先 —— docs/04：给了 n 就绝不看 Title。
	N     *int
	Title string
	// Offset 从本节正文第几个字符开始；< 0 视作 0。
	Offset int
	// MaxChars <= 0 时用 core.DefaultMaxSectionChars，上限 maxSectionCharsC。
	MaxChars int
	// Fallback：本节自己没有正文时，换成第一个有内容的子节。
	Fallback bool
	// Rev 是调用方上次读到的版本；不一致时在 Note 里提醒。
	Rev string
}

// SectionAnswer 是要么命中、要么给候选的结果。
type SectionAnswer struct {
	OK bool
	// Message 是 OK == false 时的 error 文案（docs/04 的三种定位失败）。
	Message string
	Section core.Section
	Content string
	Note    string
}

// AnswerSection 是「读一节」的全部业务逻辑。
//
// **HTTP handler 与离线 CLI 共用这一份**。空父节换子节、超长截断、rev 漂移这三件事
// 恰恰是模型最容易踩错的地方，两边各写一份必然慢慢分岔（这就是 docs/05 说的
// 「等于把业务逻辑实现两遍」）。
//
// 调用方必须保证 q.N != nil 或 q.Title != ""（参数合法性由调用方返回 400）。
func AnswerSection(e *core.Entry, q SectionQuery) SectionAnswer {
	var target core.Section

	if q.N != nil {
		s, found := findSection(e.Sections, *q.N)
		if !found {
			return SectionAnswer{Message: ErrorSectionNotFound(e.Title, *q.N, len(e.Sections))}
		}
		target = s
	} else {
		matches := MatchSectionsByTitle(e.Sections, q.Title)
		switch len(matches) {
		case 0:
			return SectionAnswer{Message: ErrorTitleNotFound(e.Title, q.Title)}
		case 1:
			target = matches[0]
		default:
			// 同名多节：不替调用方挑，把编号清单还给它。
			return SectionAnswer{Message: ErrorDuplicateTitle(q.Title, e.Title, len(matches))}
		}
	}

	note := ""
	section := target

	if section.Chars == 0 && q.Fallback {
		child, others, found := firstContentChild(e.Sections, section)
		if !found {
			return SectionAnswer{
				OK:      true,
				Section: section,
				Content: "",
				Note:    NoteSectionEmpty(section, countChildren(e.Sections, section)),
			}
		}
		note = NoteFallbackChild(section, child, others)
		section = child
	}

	total := section.Chars
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	maxChars := clamp(q.MaxChars, 1, maxSectionCharsC)
	if q.MaxChars <= 0 {
		maxChars = core.DefaultMaxSectionChars
	}

	runes := []rune(section.Content)
	if offset > len(runes) {
		offset = len(runes)
	}
	end := offset + maxChars
	if end > len(runes) {
		end = len(runes)
	}
	content := string(runes[offset:end])
	if end < len(runes) {
		note = joinNotes(note, NoteTruncated(total, offset, end-offset))
	}

	// rev 回传：内容被别人改过时不静默，而是提醒编号可能已经变了。
	if q.Rev != "" && q.Rev != e.Rev {
		note = joinNotes(NoteRevChanged(q.Rev, e.Rev), note)
	}

	return SectionAnswer{OK: true, Section: section, Content: content, Note: note}
}

// ParseSectionQuery 把 query string 解析成 SectionQuery。
//
// 返回的 message 非空表示参数不合法（调用方回 400）。
func ParseSectionQuery(get func(string) string) (SectionQuery, string) {
	q := SectionQuery{
		Title:    get("title"),
		Offset:   intParam(get("offset"), 0),
		MaxChars: intParam(get("maxChars"), 0),
		Fallback: get("fallback") != "0",
		Rev:      get("rev"),
	}
	if rawN := firstNonEmpty(get("n"), get("section")); rawN != "" {
		n, err := strconv.Atoi(rawN)
		if err != nil {
			return q, "n 必须是整数（0 = 导言）。"
		}
		q.N = &n
		return q, ""
	}
	if q.Title == "" {
		return q, "要么给 n，要么给 title。"
	}
	return q, ""
}

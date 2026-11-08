package daemon

import (
	"net/http"
	"strconv"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// sectionResponse 是成功读一节时的响应（docs/04「读一节的正文」）。
type sectionResponse struct {
	OK           bool     `json:"ok"`
	Title        string   `json:"title"`
	Path         string   `json:"path"`
	Rev          string   `json:"rev"`
	Section      int      `json:"section"`
	Level        int      `json:"level"`
	SectionTitle string   `json:"sectionTitle"`
	SectionPath  []string `json:"sectionPath"`
	PathText     string   `json:"pathText"`
	Chars        int      `json:"chars"`
	Content      string   `json:"content"`
	Note         string   `json:"note,omitempty"`
}

// sectionMissResponse 是「这个节不存在」时的响应：**200 + ok:false + candidates**。
//
// 这是 ADR-010 的核心：调用方是模型，404 会让它重试或放弃；
// 把「不能这么查」和「你可以试这些编号」直接端给它，它下一次就对了。
type sectionMissResponse struct {
	OK           bool        `json:"ok"`
	Error        string      `json:"error"`
	Candidates   []Candidate `json:"candidates"`
	SectionCount int         `json:"sectionCount"`
}

func (h *Handler) handleSection(w http.ResponseWriter, r *http.Request, kb *KBIndex, rel string) {
	e, ok := h.lookupEntry(w, kb, rel)
	if !ok {
		return
	}

	query, badParam := ParseSectionQuery(r.URL.Query().Get)
	if badParam != "" {
		writeError(w, http.StatusBadRequest, "invalid_param", badParam)
		return
	}

	answer := AnswerSection(e, query)
	if !answer.OK {
		writeSectionMiss(w, e, answer.Message)
		return
	}

	writeJSON(w, http.StatusOK, sectionResponse{
		OK: true, Title: e.Title, Path: e.RelPath, Rev: e.Rev,
		Section:      answer.Section.No,
		Level:        answer.Section.Level,
		SectionTitle: answer.Section.Title,
		SectionPath:  answer.Section.Path,
		PathText:     answer.Section.PathText,
		Chars:        answer.Section.Chars,
		Content:      answer.Content,
		Note:         answer.Note,
	})
}

func writeSectionMiss(w http.ResponseWriter, e *core.Entry, message string) {
	writeJSON(w, http.StatusOK, sectionMissResponse{
		OK:           false,
		Error:        message,
		Candidates:   CandidatesOf(e.Sections),
		SectionCount: len(e.Sections),
	})
}

// nearbyItem 是 `/nearby` 里的一节。
type nearbyItem struct {
	Section      int    `json:"section"`
	SectionTitle string `json:"sectionTitle"`
	Content      string `json:"content"`
}

type nearbyResponse struct {
	OK     bool         `json:"ok"`
	Path   string       `json:"path"`
	Center int          `json:"center"`
	Radius int          `json:"radius"`
	Items  []nearbyItem `json:"items"`
}

// handleNearby 取目标节及前后各 radius 节（docs/04）。
//
// 目前只允许 radius=1：给「答案跨了两个相邻小节」用，更大的窗口应该直接读整页。
func (h *Handler) handleNearby(w http.ResponseWriter, r *http.Request, kb *KBIndex, rel string) {
	e, ok := h.lookupEntry(w, kb, rel)
	if !ok {
		return
	}
	q := r.URL.Query()

	rawN := q.Get("n")
	if rawN == "" {
		writeError(w, http.StatusBadRequest, "invalid_param", "nearby 必须给 n。")
		return
	}
	n, err := strconv.Atoi(rawN)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_param", "n 必须是整数。")
		return
	}
	radius := intParam(q.Get("radius"), 1)
	if radius != 1 {
		writeError(w, http.StatusBadRequest, "invalid_param", "radius 目前只允许 1。")
		return
	}
	if _, found := findSection(e.Sections, n); !found {
		writeSectionMiss(w, e, ErrorSectionNotFound(e.Title, n, len(e.Sections)))
		return
	}

	resp := nearbyResponse{OK: true, Path: e.RelPath, Center: n, Radius: radius,
		Items: make([]nearbyItem, 0, 3)}
	for i := n - radius; i <= n+radius; i++ {
		s, found := findSection(e.Sections, i)
		if !found {
			continue
		}
		resp.Items = append(resp.Items, nearbyItem{
			Section:      s.No,
			SectionTitle: s.Title,
			Content:      truncateRunesSafe(s.Content, core.DefaultMaxSectionChars),
		})
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---- 小工具 ----

func findSection(sections []core.Section, n int) (core.Section, bool) {
	for _, s := range sections {
		if s.No == n {
			return s, true
		}
	}
	return core.Section{}, false
}

func indexOfSection(sections []core.Section, n int) int {
	for i, s := range sections {
		if s.No == n {
			return i
		}
	}
	return -1
}

// childRange 返回 parent 的子节在 sections 里的 [start, end) 区间。
//
// 子节 = 紧跟在它后面、层级比它深的那些（遇到第一个层级不比它深的就停）。
func childRange(sections []core.Section, parent core.Section) (int, int) {
	idx := indexOfSection(sections, parent.No)
	if idx < 0 {
		return 0, 0
	}
	end := idx + 1
	for end < len(sections) && sections[end].Level > parent.Level {
		end++
	}
	return idx + 1, end
}

// firstContentChild 找到第一个有正文的子节，并返回「除它之外还有几个子节」。
func firstContentChild(sections []core.Section, parent core.Section) (core.Section, int, bool) {
	start, end := childRange(sections, parent)
	for i := start; i < end; i++ {
		if sections[i].Chars > 0 {
			return sections[i], (end - start) - 1, true
		}
	}
	return core.Section{}, 0, false
}

func countChildren(sections []core.Section, parent core.Section) int {
	start, end := childRange(sections, parent)
	return end - start
}

func intParam(v string, fallback int) int {
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// joinNotes 把两条 note 拼起来（宁可分号也不能丢信息）。
func joinNotes(a, b string) string {
	switch {
	case a == "":
		return b
	case b == "":
		return a
	default:
		return a + " " + b
	}
}

// truncateRunesSafe 按字符截断，避免切在多字节字符中间。
func truncateRunesSafe(s string, max int) string {
	if max <= 0 {
		return ""
	}
	count := 0
	for i := range s {
		if count == max {
			return s[:i]
		}
		count++
	}
	return s
}

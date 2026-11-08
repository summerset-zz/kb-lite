package daemon

import (
	"fmt"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// 这一整个文件都是**对外文案**。
//
// docs/04 说得很清楚：「`note` 的三种文案（**照抄，它们直接影响模型行为**）」。
// 它们不是日志、不是调试信息 —— 模型会照着 `note` 决定「要不要重试 / 要不要续读 / 换哪一节」，
// 所以改一个字都算改契约。CI 里应当有测试把它们逐字锁住（见 notes_test.go）。
//
// ⚠️ 不要顺手「润色」。比如第 2 条里的「**别在这一节上重试**」，加粗是刻意的：
// 线上实测，不加这句模型会在同一个空节上反复重试。

// NoteFallbackChild 是「空父节换子节」的说明。
//
// otherChildren 是父节下面**除返回的这一节之外**还有几个子节；0 时不追加末句。
func NoteFallbackChild(parent, child core.Section, otherChildren int) string {
	s := fmt.Sprintf(
		"第 %d 节「%s」自身没有正文（那是父标题），这里给的是它第一个有内容的子节：第 %d 节「%s」的正文。",
		parent.No, parent.Title, child.No, child.Title,
	)
	if otherChildren > 0 {
		s += fmt.Sprintf("这一节下面还有 %d 个子节，要读别的就按编号再读一次。", otherChildren)
	}
	return s
}

// NoteSectionEmpty 是「这一块确实没有内容」的说明。
//
// children 是这一节下面的子节数。为 0 时省掉那个括注 —— 否则会写出
// 「它下面那 0 个子节也都没有正文」这种明显是机器拼出来的句子，反而削弱这句话的可信度。
func NoteSectionEmpty(s core.Section, children int) string {
	var mid string
	if children > 0 {
		mid = fmt.Sprintf("（它下面那 %d 个子节也都没有正文）", children)
	}
	return fmt.Sprintf(
		"第 %d 节「%s」只有标题、没有正文%s—— 这一块就是没有内容，**别在这一节上重试**。"+
			"这一页别的节按目录里的编号再读（目录行尾带着每节的字数）。",
		s.No, s.Title, mid,
	)
}

// NoteTruncated 是「这一节太长，被截断了」的说明。
//
// shown 是本次返回的字符数，nextOffset 是下次该传的 offset。
func NoteTruncated(total, offset, shown int) string {
	if offset <= 0 {
		return fmt.Sprintf("本节共 %d 字，这里给了前 %d 字。继续读传 offset=%d。", total, shown, offset+shown)
	}
	return fmt.Sprintf("本节共 %d 字，这里给了第 %d–%d 字。继续读传 offset=%d。",
		total, offset+1, offset+shown, offset+shown)
}

// NoteRevChanged 是「你读的是旧版本」的说明。
//
// 这条让「读目录 → 读正文」这种两步操作在文件被人改掉时，不至于静默读到别的内容。
func NoteRevChanged(oldRev, newRev string) string {
	return fmt.Sprintf("内容已更新（rev 由 %s 变为 %s），你上次读到的编号可能已经变了，建议先重新看 /toc。",
		oldRev, newRev)
}

// ErrorTitleNotFound 是按标题定位失败时的 `error` 文案（200 + ok:false）。
func ErrorTitleNotFound(entryTitle, query string) string {
	return fmt.Sprintf(
		"《%s》里没有标题对得上「%s」。下面是这一页的章节清单，n 只能用 candidates 里的编号（不要自己数）：",
		entryTitle, query,
	)
}

// ErrorSectionNotFound 是编号越界时的 `error` 文案。
//
// docs/04 只给了标题定位失败的措辞，这里按同一口径写 —— 调用方（模型）看到的东西
// 必须是同一套：说明为什么不行 + 给出真实的编号清单。
func ErrorSectionNotFound(entryTitle string, n int, sectionCount int) string {
	return fmt.Sprintf(
		"《%s》里没有第 %d 节（这一页一共 %d 节）。下面是这一页的章节清单，n 只能用 candidates 里的编号（不要自己数）：",
		entryTitle, n, sectionCount,
	)
}

// ErrorDuplicateTitle 是「标题对上了多节」时的 `error` 文案。
func ErrorDuplicateTitle(query, entryTitle string, count int) string {
	return fmt.Sprintf(
		"章节标题「%s」在《%s》里对上了 %d 节，没法替你挑（同名章节很常见）。请把 n 换成下面 candidates 里的编号再读一次。",
		query, entryTitle, count,
	)
}

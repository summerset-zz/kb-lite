package daemon

import (
	"testing"

	"github.com/summerset-zz/kb-lite/internal/core"
)

// 这些文案是**契约**，不是日志。docs/04 的原话是「照抄，它们直接影响模型行为」。
// 这个测试的作用是：任何人想「顺手润色」时，会先被这里拦一下。

func TestNoteFallbackChildExactText(t *testing.T) {
	parent := core.Section{No: 2, Title: "回滚"}
	child := core.Section{No: 3, Title: "快速回滚"}

	got := NoteFallbackChild(parent, child, 0)
	want := "第 2 节「回滚」自身没有正文（那是父标题），这里给的是它第一个有内容的子节：第 3 节「快速回滚」的正文。"
	if got != want {
		t.Errorf("note =\n  %q\n想要\n  %q", got, want)
	}

	got = NoteFallbackChild(parent, child, 2)
	want += "这一节下面还有 2 个子节，要读别的就按编号再读一次。"
	if got != want {
		t.Errorf("note =\n  %q\n想要\n  %q", got, want)
	}
}

func TestNoteSectionEmptyExactText(t *testing.T) {
	s := core.Section{No: 5, Title: "附录"}

	got := NoteSectionEmpty(s, 3)
	want := "第 5 节「附录」只有标题、没有正文（它下面那 3 个子节也都没有正文）—— 这一块就是没有内容，" +
		"**别在这一节上重试**。这一页别的节按目录里的编号再读（目录行尾带着每节的字数）。"
	if got != want {
		t.Errorf("note =\n  %q\n想要\n  %q", got, want)
	}

	// 没有子节时省掉那个括注（避免出现「它下面那 0 个子节」）。
	got = NoteSectionEmpty(s, 0)
	want = "第 5 节「附录」只有标题、没有正文—— 这一块就是没有内容，" +
		"**别在这一节上重试**。这一页别的节按目录里的编号再读（目录行尾带着每节的字数）。"
	if got != want {
		t.Errorf("note =\n  %q\n想要\n  %q", got, want)
	}
}

func TestNoteTruncatedExactText(t *testing.T) {
	got := NoteTruncated(4213, 0, 1500)
	want := "本节共 4213 字，这里给了前 1500 字。继续读传 offset=1500。"
	if got != want {
		t.Errorf("note =\n  %q\n想要\n  %q", got, want)
	}

	// 续读时「前 N 字」是错的，改成区间。
	got = NoteTruncated(4213, 1500, 1500)
	want = "本节共 4213 字，这里给了第 1501–3000 字。继续读传 offset=3000。"
	if got != want {
		t.Errorf("note =\n  %q\n想要\n  %q", got, want)
	}
}

func TestNoteRevChangedExactText(t *testing.T) {
	got := NoteRevChanged("a1b2c3d4", "e5f6a7b8")
	want := "内容已更新（rev 由 a1b2c3d4 变为 e5f6a7b8），你上次读到的编号可能已经变了，建议先重新看 /toc。"
	if got != want {
		t.Errorf("note =\n  %q\n想要\n  %q", got, want)
	}
}

func TestErrorTitleNotFoundExactText(t *testing.T) {
	got := ErrorTitleNotFound("运维手册", "部署")
	want := "《运维手册》里没有标题对得上「部署」。下面是这一页的章节清单，n 只能用 candidates 里的编号（不要自己数）："
	if got != want {
		t.Errorf("error =\n  %q\n想要\n  %q", got, want)
	}
}

func TestErrorDuplicateTitleExactText(t *testing.T) {
	got := ErrorDuplicateTitle("外部链接", "某条目", 2)
	want := "章节标题「外部链接」在《某条目》里对上了 2 节，没法替你挑（同名章节很常见）。请把 n 换成下面 candidates 里的编号再读一次。"
	if got != want {
		t.Errorf("error =\n  %q\n想要\n  %q", got, want)
	}
}

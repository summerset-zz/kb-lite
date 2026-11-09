package daemon

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildIndexWithUndecodableFileDoesNotPanic 守一个能把整个 daemon 带走的老 bug。
//
// 认不出编码的文件（二进制改名叫 .md、坏掉的 UTF-8）解码失败时，parseEntry 曾经
// 返回 `nil, "", warns` —— 也就是“没跳过、但也没有条目”。调用方于是把 nil 条目塞进
// 待写入列表，Store.SaveKB 解引用它就 panic。panic 发生在整理的后台 goroutine 里，
// 整个进程直接退出：用户看到的是“刚添加完知识库，服务没了”。
func TestBuildIndexWithUndecodableFileDoesNotPanic(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "二进制.md"), []byte("\xff\xfe\x80\x81\x82"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "正常.md"), "# 标题\n\n正文。\n")

	store := openTestStore(t)
	cfg := &Config{KBs: []KB{{ID: "k1", Name: "笔记", RootPath: root, Enabled: true}}}
	// 这里曾经直接 panic（goroutine 里 panic = 进程退出）。
	k := buildWith(t, NewManager(cfg, BuildOptions{Workers: 1, Store: store}))

	if len(k.Entries) != 1 {
		t.Errorf("条目数 = %d，想要 1（认不出编码的那篇应当整篇跳过）", len(k.Entries))
	}
	if len(k.Skipped) != 1 {
		t.Errorf("跳过数 = %d，想要 1：认不出的文件要记成跳过，而不是“没跳过也没条目”", len(k.Skipped))
	}
}

package daemon

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"unicode/utf8"
)

// postJSON 发一次带 JSON 体的读取面请求（check 是 `POST /kb/…`，doRequest 只会发 GET）。
func postJSON(t *testing.T, srv *httptest.Server, path string, body any) (int, map[string]any, string) {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var parsed map[string]any
	_ = json.Unmarshal(raw, &parsed)
	return resp.StatusCode, parsed, string(raw)
}

// checkOne 检查一条，把响应收成结构体（断言比 map 好读）。
func checkOne(t *testing.T, srv *httptest.Server, kb string, item CheckItem) CheckResult {
	t.Helper()
	status, body, raw := postJSON(t, srv, "/kb/"+url.PathEscape(kb)+"/check",
		map[string]any{"items": []CheckItem{item}})
	if status != http.StatusOK {
		t.Fatalf("status = %d：%s", status, raw)
	}
	items, _ := body["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v", body["items"])
	}
	m := items[0].(map[string]any)
	res := CheckResult{
		Path:     asString(m["path"]),
		Included: m["included"] == true,
		Reason:   asString(m["reason"]),
		Message:  asString(m["message"]),
		Title:    asString(m["title"]),
		Summary:  asString(m["summary"]),
	}
	if ws, ok := m["warnings"].([]any); ok {
		for _, w := range ws {
			wm := w.(map[string]any)
			res.Warnings = append(res.Warnings, CheckWarning{
				Kind: asString(wm["kind"]), Message: asString(wm["message"]),
			})
		}
	}
	return res
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

// TestCheckContent 守“写之前能不能问清会不会被收录”。
//
// 每一条都是一个真实会发生的失误：写进 `.git`、扩展名写成 `.pdf`、
// frontmatter 里留着 draft、只写了元数据没有正文。
func TestCheckContent(t *testing.T) {
	srv := newTestServer(t)

	t.Run("能收录", func(t *testing.T) {
		res := checkOne(t, srv, "笔记", CheckItem{
			Path:  "docs/部署.md",
			Head:  "---\ntitle: 部署\ntags: [ops]\n---\n\n把服务发到线上。\n",
			Chars: 120,
			Bytes: 340,
		})
		if !res.Included || res.Reason != "" {
			t.Fatalf("应当能收录：%+v", res)
		}
		if res.Title != "部署" {
			t.Errorf("title = %q，想要 frontmatter 里那个", res.Title)
		}
		if res.Path != "docs/部署.md" {
			t.Errorf("path = %q", res.Path)
		}
	})

	t.Run("标题取值链", func(t *testing.T) {
		// 没有 frontmatter，也不是从标题开头：标题取文件名，而 README 没有信息量，
		// 于是取父目录名。这是 harness 自己算不出来、最该在这里看见的一条。
		res := checkOne(t, srv, "笔记", CheckItem{
			Path:  "docs/api/README.md",
			Head:  "这个目录放接口文档。\n",
			Chars: 12,
			Bytes: 30,
		})
		if !res.Included || res.Title != "api" {
			t.Fatalf("title = %q，想要 api：%+v", res.Title, res)
		}
	})

	t.Run("路径越界", func(t *testing.T) {
		for _, p := range []string{"../外面.md", "/etc/passwd", "C:\\notes\\a.md", "."} {
			res := checkOne(t, srv, "笔记", CheckItem{Path: p, Head: "正文。", Chars: 3, Bytes: 9})
			if res.Included || res.Reason != CheckOutside {
				t.Errorf("%q 应当被判 outside：%+v", p, res)
			}
		}
	})

	t.Run("被忽略的目录与扩展名", func(t *testing.T) {
		cases := []struct {
			path string
			want string
		}{
			{".git/config.md", CheckIgnored},
			{".attachments/a.md", CheckIgnored},
			{"node_modules/pkg/readme.md", CheckIgnored},
			{"docs/a.pdf", CheckNotMatching},
		}
		for _, c := range cases {
			res := checkOne(t, srv, "笔记", CheckItem{Path: c.path, Head: "正文。", Chars: 3, Bytes: 9})
			if res.Included || res.Reason != c.want {
				t.Errorf("%q：reason = %q，想要 %q（%+v）", c.path, res.Reason, c.want, res)
			}
		}
	})

	t.Run("草稿", func(t *testing.T) {
		res := checkOne(t, srv, "笔记", CheckItem{
			Path:  "docs/草稿.md",
			Head:  "---\ntitle: 还没写完\ndraft: true\n---\n\n正文其实有。\n",
			Chars: 40,
			Bytes: 80,
		})
		if res.Included || res.Reason != CheckDraft {
			t.Fatalf("reason = %q，想要 draft：%+v", res.Reason, res)
		}
	})

	t.Run("空正文", func(t *testing.T) {
		res := checkOne(t, srv, "笔记", CheckItem{Path: "docs/空.md", Head: "", Chars: 0})
		if res.Included || res.Reason != CheckEmpty {
			t.Fatalf("reason = %q，想要 empty：%+v", res.Reason, res)
		}
		// 只有 frontmatter 的元数据卡片：head 就是全文，也该判 empty。
		head := "---\nname: 某字体\n---\n"
		res = checkOne(t, srv, "笔记", CheckItem{
			Path:  "creds/x/meta.md",
			Head:  head,
			Chars: utf8.RuneCountInString(head),
		})
		if res.Included || res.Reason != CheckEmpty {
			t.Fatalf("纯元数据卡片的 reason = %q，想要 empty：%+v", res.Reason, res)
		}
	})

	t.Run("太大", func(t *testing.T) {
		res := checkOne(t, srv, "笔记", CheckItem{
			Path: "docs/大.md", Head: "正文。", Chars: 3, Bytes: DefaultMaxFileBytes + 1,
		})
		if res.Included || res.Reason != CheckTooLarge {
			t.Fatalf("reason = %q，想要 too-large：%+v", res.Reason, res)
		}
	})

	t.Run("开头被截断时判不准", func(t *testing.T) {
		// frontmatter 没闭合，chars 说明文件还有内容：不能当成“没有 frontmatter”放过去。
		res := checkOne(t, srv, "笔记", CheckItem{
			Path:  "docs/长设置.md",
			Head:  "---\ntitle: 某个很长的标题\nsummary: 还没写完的设置\n",
			Chars: 900,
			Bytes: 2400,
		})
		if res.Included || res.Reason != CheckHeadTooShort {
			t.Fatalf("reason = %q，想要 head-too-short：%+v", res.Reason, res)
		}
		// 只看到 frontmatter、正文在 head 之后，同样是判不准。
		res = checkOne(t, srv, "笔记", CheckItem{
			Path:  "docs/设置很长.md",
			Head:  "---\ntitle: 标题\n---\n",
			Chars: 900,
			Bytes: 2400,
		})
		if res.Included || res.Reason != CheckHeadTooShort {
			t.Fatalf("正文在 head 之后时 reason = %q，想要 head-too-short", res.Reason)
		}
		// 整篇就是一个没闭合的 `---`：不是截断，是作者写坏了。core 会把它当正文收录，
		// 那这里就应当回 included，并给一条 frontmatter 提醒。
		unclosed := "---\ntitle: 忘了闭合\n\n开头少了一条分隔线。\n"
		res = checkOne(t, srv, "笔记", CheckItem{
			Path:  "docs/没闭合.md",
			Head:  unclosed,
			Chars: utf8.RuneCountInString(unclosed),
		})
		if !res.Included {
			t.Fatalf("整篇就是没闭合的 --- 时应当照样收录：%+v", res)
		}
	})

	t.Run("替换字符给提醒但不拦", func(t *testing.T) {
		res := checkOne(t, srv, "笔记", CheckItem{
			Path:  "docs/转坏了.md",
			Head:  "# 标题\n\n这段是从 PDF 转的，有个字丢了：\ufffd\n",
			Chars: 30,
			Bytes: 80,
		})
		if !res.Included {
			t.Fatalf("有替换字符也应当照常收录：%+v", res)
		}
		found := false
		for _, w := range res.Warnings {
			if w.Kind == "replacement-char" {
				found = true
			}
		}
		if !found {
			t.Errorf("应当给一条 replacement-char 提醒：%+v", res.Warnings)
		}
	})
}

// TestCheckRespectsKBIgnore 守“这个库自己的 ignore 规则也要算”，否则用户加了忽略
// 却发现 AI 还在往里写。
func TestCheckRespectsKBIgnore(t *testing.T) {
	srv := newTestServer(t)
	status, _, raw := adminReq(t, srv, http.MethodPost, "/api/kbs", kbWriteReq{
		Name: "有忽略的库", RootPath: t.TempDir(), Ignore: []string{"_drafts"},
	})
	if status != http.StatusCreated {
		t.Fatalf("建库失败：%s", raw)
	}

	res := checkOne(t, srv, "有忽略的库", CheckItem{
		Path: "_drafts/a.md", Head: "正文。", Chars: 3, Bytes: 9,
	})
	if res.Included || res.Reason != CheckIgnored {
		t.Fatalf("reason = %q，想要 ignored：%+v", res.Reason, res)
	}
}

// TestCheckLimits 守两道上限与四个 4xx。
func TestCheckLimits(t *testing.T) {
	srv := newTestServer(t)
	path := "/kb/" + url.PathEscape("笔记") + "/check"

	items := make([]CheckItem, MaxCheckItems+1)
	for i := range items {
		items[i] = CheckItem{Path: "a.md", Head: "正文。", Chars: 3, Bytes: 9}
	}
	status, _, raw := postJSON(t, srv, path, map[string]any{"items": items})
	if status != http.StatusBadRequest {
		t.Fatalf("超过条数上限 status = %d：%s", status, raw)
	}

	long := bytes.Repeat([]byte("a"), MaxCheckHead+1)
	status, _, raw = postJSON(t, srv, path, map[string]any{
		"items": []CheckItem{{Path: "a.md", Head: string(long), Chars: 3000}},
	})
	if status != http.StatusBadRequest {
		t.Fatalf("head 超长 status = %d：%s", status, raw)
	}

	status, _, raw = postJSON(t, srv, path, map[string]any{"items": []CheckItem{}})
	if status != http.StatusBadRequest {
		t.Fatalf("空 items status = %d：%s", status, raw)
	}

	status, _, _ = postJSON(t, srv, "/kb/"+url.PathEscape("没有")+"/check",
		map[string]any{"items": []CheckItem{{Path: "a.md", Head: "正文。", Chars: 3}}})
	if status != http.StatusNotFound {
		t.Errorf("不存在的库 status = %d，想要 404", status)
	}
}

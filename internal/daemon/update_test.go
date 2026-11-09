package daemon

import "testing"

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.3.0", "0.3.0", 0},
		{"v0.3.0", "0.3.0", 0},
		{"V0.3.0", "v0.3.0", 0},
		{"0.4.0", "0.3.9", 1},
		{"0.3.10", "0.3.9", 1}, // 数字比较，不是字符串比较
		{"0.3.0", "1.0.0", -1},
		{"0.4.0-dev", "0.3.9", 1},
		{"0.4.0-dev", "0.4.0", -1}, // 同数字时，带后缀的算旧
		{"0.4.0", "0.4.0-dev", 1},
		{"0.4.0-dev", "0.4.0-rc", 0}, // 两个都是预发布：不细分
		{"2.0", "1.9.9", 1},          // 位数不够按 0 补
		{"1.2.3.4", "1.2.3", 0},      // 只看前三段
	}
	for _, c := range cases {
		if got := CompareVersions(c.a, c.b); got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d，想要 %d", c.a, c.b, got, c.want)
		}
	}
}

func TestSplitVersion(t *testing.T) {
	nums, pre := splitVersion("v0.3.1-dev")
	if nums != [3]int{0, 3, 1} || pre != "dev" {
		t.Fatalf("得到 %v / %q", nums, pre)
	}
}

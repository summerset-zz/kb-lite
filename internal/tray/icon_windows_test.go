//go:build windows

package tray

import (
	"bytes"
	"image/png"
	"testing"
)

// 托盘图标的两张源图必须能解开、必须是正方形、四周必须是透明的。
//
// 最后那条不是洁癖：托盘底色是任务栏的深浅两态，源图一旦换成白底不透明，
// 图标在深色任务栏上会变成一个刺眼的白方块。这件事只有跑一次断言才拦得住。
func TestIconAssets(t *testing.T) {
	for _, c := range []struct {
		name string
		data []byte
	}{
		{"normal.png", iconNormalPNG},
		{"warn.png", iconWarnPNG},
	} {
		img, err := png.Decode(bytes.NewReader(c.data))
		if err != nil {
			t.Fatalf("%s 解不开：%v", c.name, err)
		}
		b := img.Bounds()
		if b.Dx() != b.Dy() {
			t.Errorf("%s 是 %dx%d，要正方形", c.name, b.Dx(), b.Dy())
		}
		if b.Dx() < 256 {
			t.Errorf("%s 只有 %dpx，太少了", c.name, b.Dx())
		}

		// 缩到托盘实际用的尺寸，看边缘是不是透明。
		small := downscale(img, 16)
		corner := small.NRGBAAt(0, 0)
		if corner.A != 0 {
			t.Errorf("%s 的左上角不透明（alpha=%d），托盘里会是个方块", c.name, corner.A)
		}
		// 中间必须画了点东西，否则图标等于空的。
		if mid := small.NRGBAAt(8, 8); mid.A == 0 {
			t.Errorf("%s 缩小之后正中间是透明的，图标是空的", c.name)
		}
	}
}

// 缩小之后不能把颜色拉到全黑：透明边缘如果渗进黑色，16 像素的图标会有一圈黑边。
func TestDownscaleKeepsColor(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(iconNormalPNG))
	if err != nil {
		t.Fatal(err)
	}
	small := downscale(img, 16)

	// 找最亮的那个不透明像素，它应该还带着主色的绿（R 明显小于 G）。
	var bestA uint8
	var best [3]uint8
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			c := small.NRGBAAt(x, y)
			if c.A > bestA {
				bestA, best = c.A, [3]uint8{c.R, c.G, c.B}
			}
		}
	}
	if bestA < 200 {
		t.Fatalf("没有足够不透明的像素（最亮 alpha=%d）", bestA)
	}
	if best[1] <= best[0] {
		t.Errorf("主色变成了 %v，绿色通道该比红色高，颜色被拉暗了", best)
	}
}

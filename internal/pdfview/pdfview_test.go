package pdfview

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRenderPage 最小 PDF：能开、能数页、能渲染出非空白位图。
func TestRenderPage(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "sample.pdf"))
	if err != nil {
		t.Skip("没有样例 PDF")
	}
	doc, err := Default.Open(data)
	if err != nil {
		t.Fatalf("打开：%v", err)
	}
	defer doc.Close()
	if doc.Pages() < 1 {
		t.Fatal("页数不对")
	}
	w, h, err := doc.Size(0)
	if err != nil || w <= 0 || h <= 0 {
		t.Fatalf("尺寸 = %v x %v，%v", w, h, err)
	}
	img, err := doc.Render(0, 96)
	if err != nil {
		t.Fatalf("渲染：%v", err)
	}
	if img == nil || img.Bounds().Dx() < 50 {
		t.Fatalf("位图不对：%v", img.Bounds())
	}
}

// TestChineseEmbedded 内嵌子集字体的中文 PDF：能渲染、能取回中文。
func TestChineseEmbedded(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "cjk-embedded.pdf"))
	if err != nil {
		t.Skip("没有中文样例")
	}
	doc, err := Default.Open(data)
	if err != nil {
		t.Fatalf("打开：%v", err)
	}
	defer doc.Close()

	text, err := doc.Text(0)
	if err != nil {
		t.Fatalf("取文字：%v", err)
	}
	if !strings.Contains(text, "中文渲染测试") {
		t.Errorf("文字 = %q", text)
	}
	img, err := doc.Render(0, 150)
	if err != nil {
		t.Fatalf("渲染：%v", err)
	}
	b := img.Bounds()
	if b.Dx() < 1000 || b.Dy() < 1400 {
		t.Errorf("150DPI 的位图尺寸不对：%v", b)
	}
	// 有字就一定有深色像素
	dark := 0
	for y := b.Min.Y; y < b.Max.Y; y += 3 {
		for x := b.Min.X; x < b.Max.X; x += 3 {
			r, g, bb, _ := img.At(x, y).RGBA()
			if r>>8 < 128 && g>>8 < 128 && bb>>8 < 128 {
				dark++
			}
		}
	}
	if dark < 100 {
		t.Errorf("画面几乎全白，深色像素只有 %d", dark)
	}
}

// Package pdfview 把 PDF 页渲染成位图。
//
// 用的是 gen2brain/folio：**纯 Go** 的文档渲染器（无 cgo、无 wasm），
// PDF 的路径、文字、图像、渐变、图案、透明、注释、图层都支持，Apache-2.0。
//
// 换掉 PDFium（wasm）的原因：folio 快一个量级、没有 1.9 秒的冷启动、
// 依赖只有 golang.org/x/net，二进制少 6MB；中文（内嵌子集字体）渲染与
// PDFium 逐像素一致（差异只在抗锯齿）。
//
// 已知缺口：PDF 只引用标准 CJK 字体、自己不嵌字体时，folio 缺 Adobe CMap，
// 这类老 PDF 会出乱码——PDFium 的 wasm 构建同样不行，所以不是退步。
package pdfview

import (
	"bytes"
	"fmt"
	"image"

	"github.com/gen2brain/folio/pdf"
)

// Renderer 打开文档。folio 没有全局状态，留这个类型只是让调用处不用改。
type Renderer struct{}

// Default 是应用共用的入口。
var Default = &Renderer{}

// Doc 是一份打开着的 PDF。
type Doc struct {
	doc   *pdf.Document
	pages []*pdf.Page
}

// Open 打开一份 PDF（从内存）。
func (r *Renderer) Open(data []byte) (*Doc, error) {
	if len(data) == 0 {
		return nil, fmt.Errorf("文件是空的")
	}
	doc, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, err
	}
	n := doc.NumPages()
	if n <= 0 {
		doc.Close()
		return nil, fmt.Errorf("这份 PDF 里没有页面")
	}
	return &Doc{doc: doc, pages: make([]*pdf.Page, n)}, nil
}

// Pages 返回页数。
func (d *Doc) Pages() int {
	if d == nil || d.doc == nil {
		return 0
	}
	return d.doc.NumPages()
}

// page 取第 i 页（解析一次就缓存）。
func (d *Doc) page(i int) (*pdf.Page, error) {
	if d == nil || d.doc == nil {
		return nil, fmt.Errorf("文档没打开")
	}
	if i < 0 || i >= len(d.pages) {
		return nil, fmt.Errorf("页码 %d 超出范围", i)
	}
	if d.pages[i] == nil {
		p, err := d.doc.Page(i)
		if err != nil {
			return nil, err
		}
		d.pages[i] = p
	}
	return d.pages[i], nil
}

// Size 返回某页的尺寸（单位：点，1/72 英寸）。
func (d *Doc) Size(i int) (float32, float32, error) {
	p, err := d.page(i)
	if err != nil {
		return 0, 0, err
	}
	r := p.MediaBox()
	return r.X1 - r.X0, r.Y1 - r.Y0, nil
}

// Render 把第 i 页渲染成位图。dpi 越大越清晰，也越占内存。
func (d *Doc) Render(i int, dpi float32) (image.Image, error) {
	p, err := d.page(i)
	if err != nil {
		return nil, err
	}
	if dpi < 24 {
		dpi = 24
	}
	if dpi > 600 {
		dpi = 600
	}
	return p.ImageDPI(float64(dpi))
}

// Text 取某页的纯文本（给搜索、复制用）。
func (d *Doc) Text(i int) (string, error) {
	p, err := d.page(i)
	if err != nil {
		return "", err
	}
	return p.Text()
}

// Char 是页面上一个字符的框（单位：点，**左上角为原点**，和渲染出来的位图一致）。
type Char struct {
	R              rune
	X0, Y0, X1, Y1 float32
	// Line 是它所在的文本行：同一行取同一个值，复制时按行断开。
	Line int
	// Block 是它所在的段落（TextBlock），三击选段用。
	Block int
}

// Chars 取某页的字符与它们的框，用来做选中、复制、查找。
func (d *Doc) Chars(i int) ([]Char, error) {
	p, err := d.page(i)
	if err != nil {
		return nil, err
	}
	st, err := p.StructuredText()
	if err != nil && st == nil {
		return nil, err
	}
	if st == nil {
		return nil, nil
	}
	var out []Char
	line, blockNo := 0, 0
	for _, block := range st.Blocks {
		blockNo++
		for _, ln := range block.Lines {
			line++
			for _, ch := range ln.Chars {
				if ch.Rune == '\n' || ch.Rune == '\r' {
					continue
				}
				b := ch.Quad.Bounds()
				// StructuredText 已经按 72dpi 的页面空间给出坐标（左上原点），
				// 和渲染出来的位图是同一套，不用再翻 y。
				out = append(out, Char{
					R:     ch.Rune,
					X0:    b.X0,
					Y0:    b.Y0,
					X1:    b.X1,
					Y1:    b.Y1,
					Line:  line,
					Block: blockNo,
				})
			}
		}
	}
	return out, nil
}

// Close 关掉文档。
func (d *Doc) Close() {
	if d == nil || d.doc == nil {
		return
	}
	d.doc.Close()
	d.doc = nil
	d.pages = nil
}

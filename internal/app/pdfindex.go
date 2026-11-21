package app

import (
	"os"
	"strings"
	"time"

	"papero/internal/pdfview"
	"papero/internal/vault"
)

// PDF 内容进全局搜索：把每份 PDF 的每页文本缓存起来，快速搜索时一起匹配。

// pdfTextIndex 是一份 PDF 的文本缓存。
type pdfTextIndex struct {
	ModTime time.Time
	Size    int64
	Pages   []string
}

// pdfContentHit 是一条 PDF 内容命中。
type pdfContentHit struct {
	Rel     string
	Page    int
	Snippet string
}

// pdfIndexFor 取（或建立）某份 PDF 的文本缓存。
func (a *App) pdfIndexFor(rel string) *pdfTextIndex {
	if a.pdfIndex == nil {
		a.pdfIndex = map[string]*pdfTextIndex{}
	}
	abs, err := a.vault.Abs(rel)
	if err != nil {
		return nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil
	}
	if idx, ok := a.pdfIndex[rel]; ok && idx.ModTime.Equal(info.ModTime()) && idx.Size == info.Size() {
		return idx
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	doc, err := pdfview.Default.Open(data)
	if err != nil {
		return nil
	}
	defer doc.Close()
	n := doc.Pages()
	if n > 50 { // 太长的文档只索引前 50 页，够搜索用了
		n = 50
	}
	pages := make([]string, 0, n)
	for i := 0; i < n; i++ {
		text, err := doc.Text(i)
		if err != nil {
			text = ""
		}
		pages = append(pages, text)
	}
	idx := &pdfTextIndex{ModTime: info.ModTime(), Size: info.Size(), Pages: pages}
	a.pdfIndex[rel] = idx
	return idx
}

// pdfContentHits 在所有 PDF 里找查询词，返回命中的页与上下文片段。
func (a *App) pdfContentHits(query string, limit int) []pdfContentHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if len([]rune(q)) < 2 {
		return nil
	}
	var out []pdfContentHit
	for _, f := range a.allFiles() {
		if f.Kind != vault.KindPDF {
			continue
		}
		idx := a.pdfIndexFor(f.Rel)
		if idx == nil {
			continue
		}
		for p, text := range idx.Pages {
			lower := strings.ToLower(text)
			at := strings.Index(lower, q)
			if at < 0 {
				continue
			}
			out = append(out, pdfContentHit{
				Rel:     f.Rel,
				Page:    p,
				Snippet: snippetAround(text, at, len(q)),
			})
			break // 每份文档先给一条，免得一份长文档占满列表
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

// snippetAround 取命中处前后各一段，压成一行。
func snippetAround(text string, at, n int) string {
	const pad = 24
	runes := []rune(text)
	// at 是字节位置，换算成 rune 下标
	r := len([]rune(text[:at]))
	from := r - pad
	if from < 0 {
		from = 0
	}
	to := r + n + pad
	if to > len(runes) {
		to = len(runes)
	}
	s := string(runes[from:to])
	s = strings.Join(strings.Fields(s), " ")
	if from > 0 {
		s = "…" + s
	}
	if to < len(runes) {
		s += "…"
	}
	// 太长就再截一刀，面板里一行放得下
	if r := []rune(s); len(r) > 90 {
		s = string(r[:90]) + "…"
	}
	return s
}

// openPDFAt 打开一份 PDF 并滚到指定页。
func (a *App) openPDFAt(rel string, page int) {
	a.open(rel)
	if a.active < 0 || a.active >= len(a.tabs) {
		return
	}
	if tab := a.tabs[a.active]; tab.pdfView != nil {
		tab.pdfScrollToPage = page
	}
}

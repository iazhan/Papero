package app

import (
	"fmt"
	"os"
	"strings"
	"time"

	"papero/internal/i18n"
	"papero/internal/vault"
	"papero/internal/wikitext"
)

// 全文搜索：把文本文稿（Markdown / 纯文本）的正文索引起来，和 PDF 的页文本
// 放在同一个结果列表里。索引按 mtime + size 失效，第一次搜到才建。

// textIndex 是一份文本文稿的索引。
type textIndex struct {
	ModTime time.Time
	Size    int64
	Body    string // 原文
	lower   string // 小写副本，搜索时用
	Tags    []string
}

// contentHit 是一条全文搜索命中。
type contentHit struct {
	Rel     string
	Snippet string
	Line    int // 文本文稿的行号（1 基），PDF 为 0
	Page    int // PDF 页码（0 基），文本文稿为 -1
	PDF     bool
}

// where 返回「第 N 行」或「第 N 页」。
func (h contentHit) where() string {
	if h.PDF {
		return fmt.Sprintf(i18n.T("search.page"), h.Page+1)
	}
	return fmt.Sprintf(i18n.T("search.line"), h.Line)
}

// textIndexFor 取（或建立）某份文本文稿的索引。
func (a *App) textIndexFor(rel string) *textIndex {
	if a.textIndex == nil {
		a.textIndex = map[string]*textIndex{}
	}
	abs, err := a.vault.Abs(rel)
	if err != nil {
		return nil
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil
	}
	if idx, ok := a.textIndex[rel]; ok && idx.ModTime.Equal(info.ModTime()) && idx.Size == info.Size() {
		return idx
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil
	}
	body := string(data)
	tags := make([]string, 0, 4)
	for _, t := range wikitext.Tags(body) {
		tags = append(tags, t.Name)
	}
	idx := &textIndex{
		ModTime: info.ModTime(),
		Size:    info.Size(),
		Body:    body,
		lower:   strings.ToLower(body),
		Tags:    tags,
	}
	a.textIndex[rel] = idx
	return idx
}

// contentHits 在文本文稿与 PDF 里找查询词。
func (a *App) contentHits(query string, limit int) []contentHit {
	q := strings.ToLower(strings.TrimSpace(query))
	if len([]rune(q)) < 2 {
		return nil
	}
	var out []contentHit
	for _, f := range a.allFiles() {
		if len(out) >= limit {
			break
		}
		switch f.Kind {
		case vault.KindMarkdown, vault.KindText:
			idx := a.textIndexFor(f.Rel)
			if idx == nil {
				continue
			}
			at := strings.Index(idx.lower, q)
			if at < 0 {
				// 正文没有就再看看标签
				for _, t := range idx.Tags {
					if strings.Contains(strings.ToLower(t), q) {
						out = append(out, contentHit{
							Rel:     f.Rel,
							Snippet: i18n.T("search.tag_prefix") + t,
							Line:    1,
						})
						break
					}
				}
				continue
			}
			out = append(out, contentHit{
				Rel:     f.Rel,
				Snippet: snippetAround(idx.Body, at, len(q)),
				Line:    lineAt(idx.Body, at),
			})
		case vault.KindPDF:
			for _, h := range a.pdfContentHits(q, 1) {
				if h.Rel != f.Rel {
					continue
				}
				out = append(out, contentHit{
					Rel:     h.Rel,
					Snippet: h.Snippet,
					Page:    h.Page,
					PDF:     true,
				})
				break
			}
		}
	}
	return out
}

// openContentHit 打开命中的文稿，并尽量跳到命中处。
func (a *App) openContentHit(h contentHit) {
	if h.PDF {
		a.openPDFAt(h.Rel, h.Page)
		return
	}
	a.open(h.Rel)
	if a.active < 0 || a.active >= len(a.tabs) {
		return
	}
	tab := a.tabs[a.active]
	if tab.kind != vault.KindMarkdown {
		return
	}
	// 富文本模式才有编辑器能跳词；顺手切过去，跳完用户可以再切回源码
	tab.mode = "blocks"
	tab.pendingFind = strings.TrimSpace(a.paletteQuery)
}

// kindOf 按路径查文稿类型（找不到就当 Markdown）。
func (a *App) kindOf(rel string) vault.Kind {
	if a.tree != nil {
		if n := a.tree.Find(rel); n != nil {
			return n.Kind
		}
	}
	return vault.KindMarkdown
}

// lineAt 返回字节位置 at 所在的行号（1 基）。
func lineAt(s string, at int) int {
	if at > len(s) {
		at = len(s)
	}
	return strings.Count(s[:at], "\n") + 1
}

// itoa 是 strconv.Itoa 的短别名，免得为几处引入包。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

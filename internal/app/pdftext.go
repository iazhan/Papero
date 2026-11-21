package app

import (
	"strings"
	"unicode"

	"github.com/egoist/mygo/ui"

	"papero/internal/pdfview"
)

// PDF 的文字层：按字符框做选中、复制与查找。
//
// 页面本身是位图（渲染引擎画的），这一层不画字，只画**高亮块**：
// 每个字符在页面空间（点，左上原点）有一个框，乘上缩放比就是屏幕上的矩形。
//
// 选区是**文档级**的：从一个 (页, 字符) 到另一个 (页, 字符)，所以可以跨页选中。

// pdfPos 是文档里的一个位置：第几页的第几个字符。
type pdfPos struct {
	Page int
	Char int
}

// noPos 是「没有位置」。
var noPos = pdfPos{Page: -1, Char: -1}

func (p pdfPos) valid() bool { return p.Page >= 0 && p.Char >= 0 }

// before 报告 p 是否在 q 之前（先比页，再比字符）。
func (p pdfPos) before(q pdfPos) bool {
	if p.Page != q.Page {
		return p.Page < q.Page
	}
	return p.Char < q.Char
}

// pdfCharRect 把一个字符框换算成页面内的矩形（单位 DIP）。
func pdfCharRect(c pdfview.Char, scale float32) ui.Rect {
	return ui.Rect{
		X: c.X0 * scale,
		Y: c.Y0 * scale,
		W: (c.X1 - c.X0) * scale,
		H: (c.Y1 - c.Y0) * scale,
	}
}

// pdfCharsAt 取某一页的字符（按页缓存）。
func (a *App) pdfCharsAt(tab *tab, page int) []pdfview.Char {
	if tab.pdfView == nil || page < 0 || page >= tab.pdfView.Pages() {
		return nil
	}
	if tab.pdfCharCache == nil {
		tab.pdfCharCache = map[int][]pdfview.Char{}
	}
	if chars, ok := tab.pdfCharCache[page]; ok {
		return chars
	}
	chars, err := tab.pdfView.Chars(page)
	if err != nil {
		chars = nil
	}
	tab.pdfCharCache[page] = chars
	return chars
}

// pdfHitChar 找出指针落在哪个字符上：优先同一行，其次最近的。
func pdfHitChar(chars []pdfview.Char, x, y float32) int {
	best, bestD := -1, float32(1e18)
	for i, c := range chars {
		var dy, dx float32
		if y < c.Y0 {
			dy = c.Y0 - y
		} else if y > c.Y1 {
			dy = y - c.Y1
		}
		if x < c.X0 {
			dx = c.X0 - x
		} else if x > c.X1 {
			dx = x - c.X1
		}
		// 行差权重高：宁可横向跑远一点，也不要跳到相邻行
		d := dy*6 + dx
		if d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

// pdfSelectionRuns 把一段字符合并成若干矩形（同一行相邻的并成一条），
// 免得一页几千个字就画几千个方块。
func pdfSelectionRuns(chars []pdfview.Char, from, to int, scale float32) []ui.Rect {
	if from < 0 || to < 0 || len(chars) == 0 {
		return nil
	}
	if from > to {
		from, to = to, from
	}
	if to >= len(chars) {
		to = len(chars) - 1
	}
	var out []ui.Rect
	var cur ui.Rect
	have := false
	prevLine := -1
	for i := from; i <= to; i++ {
		r := pdfCharRect(chars[i], scale)
		if r.W <= 0 || r.H <= 0 {
			continue
		}
		if have && chars[i].Line == prevLine && r.X <= cur.X+cur.W+0.6 {
			if r.X+r.W > cur.X+cur.W {
				cur.W = r.X + r.W - cur.X
			}
			continue
		}
		if have {
			out = append(out, cur)
		}
		cur, have, prevLine = r, true, chars[i].Line
	}
	if have {
		out = append(out, cur)
	}
	return out
}

// pdfRangeOn 返回选区落在第 page 页上的字符区间（不在这一页就返回 false）。
func (a *App) pdfRangeOn(tab *tab, page int) (int, int, bool) {
	from, to := tab.pdfSelFrom, tab.pdfSelTo
	if !from.valid() || !to.valid() {
		return 0, 0, false
	}
	if to.before(from) {
		from, to = to, from
	}
	if page < from.Page || page > to.Page {
		return 0, 0, false
	}
	chars := a.pdfCharsAt(tab, page)
	if len(chars) == 0 {
		return 0, 0, false
	}
	start := 0
	if page == from.Page {
		start = from.Char
	}
	end := len(chars) - 1
	if page == to.Page {
		end = to.Char
	}
	if start < 0 {
		start = 0
	}
	if end >= len(chars) {
		end = len(chars) - 1
	}
	if start > end {
		return 0, 0, false
	}
	return start, end, true
}

// pdfSelectionText 取出选中的文字：页内按行拼，跨页之间补换行。
func (a *App) pdfSelectionText(tab *tab) string {
	from, to := tab.pdfSelFrom, tab.pdfSelTo
	if !from.valid() || !to.valid() {
		return ""
	}
	if to.before(from) {
		from, to = to, from
	}
	var parts []string
	for p := from.Page; p <= to.Page && p < tab.pdfView.Pages(); p++ {
		chars := a.pdfCharsAt(tab, p)
		if len(chars) == 0 {
			continue
		}
		start := 0
		if p == from.Page {
			start = from.Char
		}
		end := len(chars) - 1
		if p == to.Page {
			end = to.Char
		}
		if start < 0 {
			start = 0
		}
		if end >= len(chars) {
			end = len(chars) - 1
		}
		if start > end {
			continue
		}
		parts = append(parts, pdfPageText(chars, start, end))
	}
	return strings.Join(parts, "\n")
}

// pdfPageText 拼出一页里某段字符的文字（换行处补 \n）。
func pdfPageText(chars []pdfview.Char, from, to int) string {
	var sb strings.Builder
	prevLine := -1
	for i := from; i <= to && i < len(chars); i++ {
		if prevLine >= 0 && chars[i].Line != prevLine {
			sb.WriteByte('\n')
		}
		sb.WriteRune(chars[i].R)
		prevLine = chars[i].Line
	}
	return sb.String()
}

// pdfSearch 在整份文档里找字符串，记下每处的 (页, 起, 止)。
//
// 按**字符下标**搜，不按字节：中文一个字三个字节，用字节偏移去映射下标会错位。
func (a *App) pdfSearch(tab *tab, query string) {
	tab.pdfFindHits = nil
	tab.pdfFindAt = -1
	tab.pdfFind = query
	q := []rune(strings.ToLower(strings.TrimSpace(query)))
	if len(q) == 0 || tab.pdfView == nil {
		return
	}
	for p := 0; p < tab.pdfView.Pages(); p++ {
		chars := a.pdfCharsAt(tab, p)
		if len(chars) == 0 {
			continue
		}
		// 拼出这一页的文字，同时记下每个 rune 对应哪个字符
		runes := make([]rune, 0, len(chars)+16)
		owner := make([]int, 0, len(chars)+16)
		for i, ch := range chars {
			if i > 0 && chars[i-1].Line != ch.Line {
				runes = append(runes, '\n')
				owner = append(owner, -1)
			}
			runes = append(runes, unicode.ToLower(ch.R))
			owner = append(owner, i)
		}
		for i := 0; i+len(q) <= len(runes); i++ {
			match := true
			for k := range q {
				if runes[i+k] != q[k] {
					match = false
					break
				}
			}
			if !match {
				continue
			}
			from, to := owner[i], owner[i+len(q)-1]
			if from < 0 || to < 0 || from > to {
				continue
			}
			tab.pdfFindHits = append(tab.pdfFindHits, pdfHit{Page: p, From: from, To: to})
			if len(tab.pdfFindHits) >= 500 {
				break
			}
		}
		if len(tab.pdfFindHits) >= 500 {
			break
		}
	}
	if len(tab.pdfFindHits) > 0 {
		tab.pdfFindAt = 0
		h := tab.pdfFindHits[0]
		tab.pdfSelFrom = pdfPos{Page: h.Page, Char: h.From}
		tab.pdfSelTo = pdfPos{Page: h.Page, Char: h.To}
		tab.pdfScrollToSel = true
	}
}

// pdfFindNext 跳到下一处匹配。
func (a *App) pdfFindNext(tab *tab, delta int) {
	if len(tab.pdfFindHits) == 0 {
		return
	}
	tab.pdfFindAt = (tab.pdfFindAt + delta + len(tab.pdfFindHits)) % len(tab.pdfFindHits)
	h := tab.pdfFindHits[tab.pdfFindAt]
	tab.pdfSelFrom = pdfPos{Page: h.Page, Char: h.From}
	tab.pdfSelTo = pdfPos{Page: h.Page, Char: h.To}
	tab.pdfScrollToSel = true
}

// pdfSelectAll 选中整份文档。
func (a *App) pdfSelectAll(tab *tab) {
	if tab.pdfView == nil || tab.pdfView.Pages() == 0 {
		return
	}
	last := tab.pdfView.Pages() - 1
	chars := a.pdfCharsAt(tab, last)
	if len(chars) == 0 {
		return
	}
	tab.pdfSelFrom = pdfPos{Page: 0, Char: 0}
	tab.pdfSelTo = pdfPos{Page: last, Char: len(chars) - 1}
}

// pdfWordRange 返回 idx 所在的「词」：西文按字母数字连写，中文按连续的汉字串。
func pdfWordRange(chars []pdfview.Char, idx int) (int, int) {
	if idx < 0 || idx >= len(chars) {
		return -1, -1
	}
	kind := pdfCharKind(chars[idx].R)
	if kind == 0 {
		return idx, idx
	}
	from, to := idx, idx
	for from > 0 && chars[from-1].Line == chars[idx].Line && pdfCharKind(chars[from-1].R) == kind {
		from--
	}
	for to < len(chars)-1 && chars[to+1].Line == chars[idx].Line && pdfCharKind(chars[to+1].R) == kind {
		to++
	}
	return from, to
}

// pdfCharKind 把字符归类：1 汉字，2 字母数字，0 其它（标点、空白）。
func pdfCharKind(r rune) int {
	switch {
	case unicode.Is(unicode.Han, r) || unicode.Is(unicode.Hiragana, r) ||
		unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hangul, r):
		return 1
	case unicode.IsLetter(r) || unicode.IsDigit(r):
		return 2
	}
	return 0
}

// pdfLineRange 返回 idx 所在那一行的字符区间（双击选整行用）。
func pdfLineRange(chars []pdfview.Char, idx int) (int, int) {
	if idx < 0 || idx >= len(chars) {
		return -1, -1
	}
	line := chars[idx].Line
	from, to := idx, idx
	for from > 0 && chars[from-1].Line == line {
		from--
	}
	for to < len(chars)-1 && chars[to+1].Line == line {
		to++
	}
	return from, to
}

// pdfParagraphRange 返回 idx 所在段落（TextBlock）的字符区间，三击选段用。
func pdfParagraphRange(chars []pdfview.Char, idx int) (int, int) {
	if idx < 0 || idx >= len(chars) {
		return -1, -1
	}
	block := chars[idx].Block
	from, to := idx, idx
	for from > 0 && chars[from-1].Block == block {
		from--
	}
	for to < len(chars)-1 && chars[to+1].Block == block {
		to++
	}
	return from, to
}

// pdfHit 是一处查找结果：第几页、字符区间。
type pdfHit struct {
	Page     int
	From, To int
}

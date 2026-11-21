package app

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"papero/internal/i18n"
	"papero/internal/pdfview"
)

// PDF 的批注（高亮 + 备注）。
//
// 存在 PDF 旁边的「同名 .papero.json」里，不写进 PDF 本体：改 PDF 需要重写整个
// 文件结构（交叉引用表、流对象），风险大且各家阅读器兼容性不一；旁注文件也更好
// 同步、更好回退。

// pdfAnnot 是一条批注：某一页的一段文字，加颜色与备注。
type pdfAnnot struct {
	ID      string `json:"id"`
	Page    int    `json:"page"`
	From    int    `json:"from"`
	To      int    `json:"to"`
	Color   string `json:"color,omitempty"`
	Note    string `json:"note,omitempty"`
	Created string `json:"created,omitempty"`
}

// pdfAnnotFile 是旁注文件的内容。
type pdfAnnotFile struct {
	Version int        `json:"version"`
	Annots  []pdfAnnot `json:"annots"`
}

// pdfAnnotColors 是高亮可选的颜色，Key 会写进旁注文件，不能随便改。
var pdfAnnotColors = []struct {
	Key  string
	Fill ui.Color
}{
	{"yellow", ui.Hex("#facc15")},
	{"green", ui.Hex("#4ade80")},
	{"blue", ui.Hex("#60a5fa")},
	{"pink", ui.Hex("#f472b6")},
}

// pdfAnnotColorName 是颜料的显示名，key 由 Key 拼出来（pdf.color.<key>）。
// 名字不在表里存：包级变量里的取词只在 init 跑一次，语言一换就永远是旧值。
func pdfAnnotColorName(key string) string { return i18n.T("pdf.color." + key) }

// pdfAnnotFill 按颜色键取填充色。
func pdfAnnotFill(key string) ui.Color {
	for _, c := range pdfAnnotColors {
		if c.Key == key {
			return c.Fill
		}
	}
	return pdfAnnotColors[0].Fill
}

// annotPath 返回某份 PDF 的旁注文件路径。
func annotPath(rel string) string { return rel + ".papero.json" }

// loadPDFAnnots 读入旁注（文件不存在就是空的）。
func (a *App) loadPDFAnnots(tab *tab) {
	if tab.pdfAnnotsLoaded {
		return
	}
	tab.pdfAnnotsLoaded = true
	data, err := a.vault.Read(annotPath(tab.rel))
	if err != nil || len(data) == 0 {
		return
	}
	var f pdfAnnotFile
	if err := json.Unmarshal(data, &f); err != nil {
		return
	}
	tab.pdfAnnots = f.Annots
}

// savePDFAnnots 把旁注写回文件（没有批注就删掉文件）。
func (a *App) savePDFAnnots(tab *tab) {
	rel := annotPath(tab.rel)
	if len(tab.pdfAnnots) == 0 {
		_ = a.vault.Remove(rel)
		return
	}
	f := pdfAnnotFile{Version: 1, Annots: tab.pdfAnnots}
	data, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return
	}
	if err := a.vault.Write(rel, append(data, '\n')); err != nil {
		a.say(i18n.T("pdf.annotations.save_failed") + err.Error())
	}
}

// addPDFAnnot 用当前选区建一条高亮批注。
func (a *App) addPDFAnnot(tab *tab, color string) {
	if !tab.pdfSelFrom.valid() || !tab.pdfSelTo.valid() {
		a.say(i18n.T("pdf.annotations.select_first"))
		return
	}
	from, to := tab.pdfSelFrom, tab.pdfSelTo
	if to.before(from) {
		from, to = to, from
	}
	if from.Page != to.Page {
		a.say(i18n.T("pdf.annotations.one_page"))
		return
	}
	for _, an := range tab.pdfAnnots {
		if an.Page == from.Page && an.From == from.Char && an.To == to.Char {
			a.say(i18n.T("pdf.annotations.already"))
			return
		}
	}
	tab.pdfAnnots = append(tab.pdfAnnots, pdfAnnot{
		ID:      fmt.Sprintf("a%d", time.Now().UnixNano()),
		Page:    from.Page,
		From:    from.Char,
		To:      to.Char,
		Color:   color,
		Created: time.Now().Format(time.RFC3339),
	})
	tab.pdfAnnotSel = len(tab.pdfAnnots) - 1
	a.savePDFAnnots(tab)
	a.say(i18n.T("pdf.annotations.added"))
}

// deletePDFAnnot 删掉第 i 条批注。
func (a *App) deletePDFAnnot(tab *tab, i int) {
	if i < 0 || i >= len(tab.pdfAnnots) {
		return
	}
	tab.pdfAnnots = append(tab.pdfAnnots[:i], tab.pdfAnnots[i+1:]...)
	if tab.pdfAnnotSel >= len(tab.pdfAnnots) {
		tab.pdfAnnotSel = len(tab.pdfAnnots) - 1
	}
	a.savePDFAnnots(tab)
}

// setPDFAnnotNote 改批注的备注。
func (a *App) setPDFAnnotNote(tab *tab, i int, note string) {
	if i < 0 || i >= len(tab.pdfAnnots) {
		return
	}
	if tab.pdfAnnots[i].Note == note {
		return
	}
	tab.pdfAnnots[i].Note = note
	a.savePDFAnnots(tab)
}

// pdfAnnotAt 找出点中的批注（点高亮块时选中它）。
func (a *App) pdfAnnotAt(tab *tab, page int, chars []pdfview.Char, x, y float32) int {
	for i, an := range tab.pdfAnnots {
		if an.Page != page || an.From < 0 || an.To >= len(chars) {
			continue
		}
		for k := an.From; k <= an.To; k++ {
			c := chars[k]
			if x >= c.X0 && x <= c.X1 && y >= c.Y0 && y <= c.Y1 {
				return i
			}
		}
	}
	return -1
}

// annotSummary 返回一条批注的摘要文字（给面板显示）。
func (a *App) annotSummary(tab *tab, i int) string {
	if i < 0 || i >= len(tab.pdfAnnots) {
		return ""
	}
	an := tab.pdfAnnots[i]
	text := pdfPageText(a.pdfCharsAt(tab, an.Page), an.From, an.To)
	text = strings.Join(strings.Fields(text), " ")
	if len([]rune(text)) > 40 {
		text = string([]rune(text)[:40]) + "…"
	}
	return text
}

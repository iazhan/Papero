// Package docx 把 .docx（OOXML）读成 Markdown。
//
// .docx 本质是一个 zip：word/document.xml 是正文，word/styles.xml 定义样式，
// word/numbering.xml 定义编号。这里只用标准库解析，不依赖 Office，也不需要
// 系统里装 Word，因此它在任何平台上都能读 Word 文稿。
//
// 覆盖范围：标题、正文、粗体/斜体/删除线/行内代码、项目符号与编号列表、
// 表格、换行与分页。图片、批注、修订与域代码暂不转换，保留为文字说明。
package docx

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
)

// ErrNotDocx 表示数据不是 .docx（例如旧版 .doc 二进制格式）。
var ErrNotDocx = errors.New("docx：不是有效的 Word 文稿（.docx），旧版 .doc 暂不支持")

// ToMarkdown 把 .docx 的字节转成 Markdown。
func ToMarkdown(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", ErrNotDocx
	}
	files := map[string]*zip.File{}
	for _, f := range zr.File {
		files[f.Name] = f
	}
	doc, ok := files["word/document.xml"]
	if !ok {
		return "", ErrNotDocx
	}
	body, err := readAll(doc)
	if err != nil {
		return "", err
	}
	styleNames := map[string]string{}
	if f, ok := files["word/styles.xml"]; ok {
		if raw, err := readAll(f); err == nil {
			styleNames = parseStyles(raw)
		}
	}
	numbering := map[string]string{}
	if f, ok := files["word/numbering.xml"]; ok {
		if raw, err := readAll(f); err == nil {
			numbering = parseNumbering(raw)
		}
	}
	return convert(body, styleNames, numbering)
}

func readAll(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(io.LimitReader(rc, 128<<20))
}

// 正文结构，只声明用得到的元素。encoding/xml 不支持「a>b,attr」这种带属性的链式路径，
// 因此用嵌套结构体表达。
type (
	attrVal struct {
		Val string `xml:"val,attr"`
	}
	paraPr struct {
		Style attrVal `xml:"pStyle"`
		NumPr numPr   `xml:"numPr"`
	}
	numPr struct {
		NumID attrVal `xml:"numId"`
		Ilvl  attrVal `xml:"ilvl"`
	}
	para struct {
		PPr   paraPr `xml:"pPr"`
		Runs  []run  `xml:"r"`
		Links []run  `xml:"hyperlink>r"`
	}
	runPr struct {
		Bold   *struct{} `xml:"b"`
		Italic *struct{} `xml:"i"`
		Strike *struct{} `xml:"strike"`
	}
	run struct {
		RPr   runPr      `xml:"rPr"`
		Text  string     `xml:"t"`
		Tab   []struct{} `xml:"tab"`
		Break []struct{} `xml:"br"`
	}
	table struct {
		Rows []tableRow `xml:"tr"`
	}
	tableRow struct {
		Cells []tableCell `xml:"tc"`
	}
	tableCell struct {
		Paras []para `xml:"p"`
	}
	stylesXML struct {
		Styles []styleDef `xml:"style"`
	}
	styleDef struct {
		ID   string  `xml:"styleId,attr"`
		Type string  `xml:"type,attr"`
		Name attrVal `xml:"name"`
	}
	numberingXML struct {
		Nums []numDef `xml:"num"`
		Abs  []absDef `xml:"abstractNum"`
	}
	numDef struct {
		NumID string  `xml:"numId,attr"`
		AbsID attrVal `xml:"abstractNumId"`
	}
	absDef struct {
		AbsID string   `xml:"abstractNumId,attr"`
		Lvls  []lvlDef `xml:"lvl"`
	}
	lvlDef struct {
		Ilvl string  `xml:"ilvl,attr"`
		Fmt  attrVal `xml:"numFmt"`
	}
)

func parseStyles(raw []byte) map[string]string {
	var s stylesXML
	if err := xml.Unmarshal(raw, &s); err != nil {
		return nil
	}
	out := map[string]string{}
	for _, st := range s.Styles {
		if st.ID != "" && st.Name.Val != "" {
			out[st.ID] = strings.ToLower(st.Name.Val)
		}
	}
	return out
}

func parseNumbering(raw []byte) map[string]string {
	var n numberingXML
	if err := xml.Unmarshal(raw, &n); err != nil {
		return nil
	}
	abs := map[string]map[string]string{}
	for _, a := range n.Abs {
		lvls := map[string]string{}
		for _, l := range a.Lvls {
			lvls[l.Ilvl] = l.Fmt.Val
		}
		abs[a.AbsID] = lvls
	}
	out := map[string]string{}
	for _, num := range n.Nums {
		// 键是 "numId/ilvl"，值是 bullet 或 decimal。
		for ilvl, format := range abs[num.AbsID.Val] {
			out[num.NumID+"/"+ilvl] = format
		}
	}
	return out
}

var headingRe = regexp.MustCompile(`^heading\s*([1-9])$`)

// convert 把正文 XML 转成 Markdown。
func convert(body []byte, styleNames, numbering map[string]string) (string, error) {
	dec := xml.NewDecoder(bytes.NewReader(body))
	var out strings.Builder
	inBody := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "body":
				inBody = true
			case "p":
				if !inBody {
					continue
				}
				var p para
				if err := dec.DecodeElement(&p, &t); err != nil {
					return "", err
				}
				writePara(&out, p, styleNames, numbering)
			case "tbl":
				if !inBody {
					continue
				}
				var tb table
				if err := dec.DecodeElement(&tb, &t); err != nil {
					return "", err
				}
				writeTable(&out, tb, styleNames)
			}
		case xml.EndElement:
			if t.Name.Local == "body" {
				inBody = false
			}
		}
	}
	text := strings.TrimRight(out.String(), "\n") + "\n"
	// 压缩连续空行，最多保留一个空行。
	text = regexp.MustCompile(`\n{3,}`).ReplaceAllString(text, "\n\n")
	return text, nil
}

// writePara 把一段写成 Markdown。
func writePara(out *strings.Builder, p para, styleNames, numbering map[string]string) {
	text := inlineText(p)
	style := ""
	if p.PPr.Style.Val != "" {
		if name, ok := styleNames[p.PPr.Style.Val]; ok {
			style = name
		} else {
			style = strings.ToLower(p.PPr.Style.Val)
		}
	}

	if m := headingRe.FindStringSubmatch(style); m != nil {
		if text != "" {
			fmt.Fprintf(out, "%s %s\n\n", strings.Repeat("#", int(m[1][0]-'0')), text)
		}
		return
	}
	switch style {
	case "title":
		if text != "" {
			fmt.Fprintf(out, "# %s\n\n", text)
		}
		return
	case "subtitle":
		if text != "" {
			fmt.Fprintf(out, "**%s**\n\n", text)
		}
		return
	}

	if numID := p.PPr.NumPr.NumID.Val; numID != "" && numID != "0" {
		ilvl := 0
		if v := p.PPr.NumPr.Ilvl.Val; v != "" {
			ilvl = int(v[0] - '0')
		}
		marker := "- "
		if numbering[numID+"/"+fmt.Sprint(ilvl)] == "decimal" {
			marker = "1. "
		}
		indent := strings.Repeat("  ", ilvl)
		fmt.Fprintf(out, "%s%s%s\n", indent, marker, text)
		return
	}

	if text == "" {
		out.WriteString("\n")
		return
	}
	fmt.Fprintf(out, "%s\n\n", text)
}

// writeTable 把表格写成 GFM 表格。
func writeTable(out *strings.Builder, tb table, styleNames map[string]string) {
	if len(tb.Rows) == 0 {
		return
	}
	var rows [][]string
	cols := 0
	for _, r := range tb.Rows {
		var cells []string
		for _, c := range r.Cells {
			var parts []string
			for _, p := range c.Paras {
				if s := inlineText(p); s != "" {
					parts = append(parts, s)
				}
			}
			cells = append(cells, strings.Join(parts, " "))
		}
		cols = max(cols, len(cells))
		rows = append(rows, cells)
	}
	if cols == 0 {
		return
	}
	writeRow := func(cells []string) {
		out.WriteString("|")
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(cells) {
				cell = cells[i]
			}
			out.WriteString(" " + strings.ReplaceAll(cell, "|", `\|`) + " |")
		}
		out.WriteString("\n")
	}
	writeRow(rows[0])
	out.WriteString("|")
	for i := 0; i < cols; i++ {
		out.WriteString(" --- |")
	}
	out.WriteString("\n")
	for _, r := range rows[1:] {
		writeRow(r)
	}
	out.WriteString("\n")
}

// inlineText 把一段里的 run 拼成带 Markdown 标记的文字。
func inlineText(p para) string {
	var b strings.Builder
	writeRuns := func(runs []run) {
		for _, r := range runs {
			text := r.Text
			if len(r.Tab) > 0 {
				text = " " + text
			}
			if text == "" {
				if len(r.Break) > 0 {
					b.WriteString("  \n")
				}
				continue
			}
			text = escapeInline(text)
			if r.RPr.Bold != nil && r.RPr.Italic != nil {
				text = "***" + text + "***"
			} else if r.RPr.Bold != nil {
				text = "**" + text + "**"
			} else if r.RPr.Italic != nil {
				text = "*" + text + "*"
			}
			if r.RPr.Strike != nil {
				text = "~~" + text + "~~"
			}
			b.WriteString(text)
		}
	}
	writeRuns(p.Runs)
	writeRuns(p.Links)
	return strings.TrimSpace(b.String())
}

// escapeInline 转义会破坏行内 Markdown 的字符。
var inlineEscaper = strings.NewReplacer(
	`\`, `\\`,
	`*`, `\*`,
	`_`, `\_`,
	"`", "\\`",
	`[`, `\[`,
	`]`, `\]`,
	`<`, `\<`,
)

func escapeInline(s string) string {
	return inlineEscaper.Replace(s)
}

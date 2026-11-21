package docx

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"
)

// buildDocx 拼一个最小的 .docx，用来验证转换。
func buildDocx(t *testing.T, body string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	write := func(name, content string) {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	write("word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>`+body+`</w:body></w:document>`)
	write("word/styles.xml", `<?xml version="1.0" encoding="UTF-8"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/></w:style>
</w:styles>`)
	write("word/numbering.xml", `<?xml version="1.0" encoding="UTF-8"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="0">
  <w:lvl w:ilvl="0"><w:numFmt w:val="bullet"/></w:lvl>
  <w:lvl w:ilvl="1"><w:numFmt w:val="decimal"/></w:lvl>
</w:abstractNum>
<w:num w:numId="1"><w:abstractNumId w:val="0"/></w:num>
</w:numbering>`)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestToMarkdown(t *testing.T) {
	body := `
<w:p><w:pPr><w:pStyle w:val="Heading1"/></w:pPr><w:r><w:t>课程设计报告</w:t></w:r></w:p>
<w:p><w:r><w:t>这是</w:t></w:r><w:r><w:rPr><w:b/></w:rPr><w:t>加粗</w:t></w:r><w:r><w:t>与</w:t></w:r><w:r><w:rPr><w:i/></w:rPr><w:t>斜体</w:t></w:r><w:r><w:t>。</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="0"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>第一项</w:t></w:r></w:p>
<w:p><w:pPr><w:numPr><w:ilvl w:val="1"/><w:numId w:val="1"/></w:numPr></w:pPr><w:r><w:t>第二层</w:t></w:r></w:p>
<w:tbl>
  <w:tr><w:tc><w:p><w:r><w:t>列一</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>列二</w:t></w:r></w:p></w:tc></w:tr>
  <w:tr><w:tc><w:p><w:r><w:t>甲</w:t></w:r></w:p></w:tc><w:tc><w:p><w:r><w:t>乙</w:t></w:r></w:p></w:tc></w:tr>
</w:tbl>`
	md, err := ToMarkdown(buildDocx(t, body))
	if err != nil {
		t.Fatalf("ToMarkdown: %v", err)
	}
	for _, want := range []string{
		"# 课程设计报告",
		"**加粗**",
		"*斜体*",
		"- 第一项",
		"  1. 第二层",
		"| 列一 | 列二 |",
		"| 甲 | 乙 |",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("输出里缺少 %q\n完整输出：\n%s", want, md)
		}
	}
}

func TestToMarkdownRejectsGarbage(t *testing.T) {
	if _, err := ToMarkdown([]byte("not a zip")); err == nil {
		t.Fatal("非 zip 数据应当报错")
	}
}

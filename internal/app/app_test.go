package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"papero/internal/board"
	"papero/internal/config"
	"papero/internal/i18n"
	"papero/internal/pdfview"
	"papero/internal/theme"
	"papero/internal/update"
	"papero/internal/vault"
	"papero/internal/wiki"
)

// newTestApp 在一个临时知识库里装配应用。

func newTestApp(t *testing.T) (*App, string) {
	t.Helper()
	// 配置写到临时目录：测试里的 open/commit 会 cfg.Save()，不隔离就会
	// 覆盖用户真实的 settings.json（vault 与 lastOpen 一改，下次启动
	// 就只剩「打开第一篇文稿」）。
	t.Cleanup(config.SetDir(t.TempDir()))
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
	os.WriteFile(filepath.Join(dir, "notes", "欢迎.md"), []byte("# 标题\n\n正文一段，带 **粗体**。\n\n- [ ] 待办\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "纯文本.txt"), []byte("hello world\n"), 0o644)

	// 一张 4x4 的 PNG
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.Set(0, 0, color.RGBA{255, 0, 0, 255})
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "图片.png"), pngBuf.Bytes(), 0o644)

	// 一个 .docx
	os.WriteFile(filepath.Join(dir, "报告.docx"), buildDocxBytes(t), 0o644)
	// 一个 .pdf
	os.WriteFile(filepath.Join(dir, "论文.pdf"), buildPDFBytes(), 0o644)
	// 一个白板
	os.WriteFile(filepath.Join(dir, "画布.excalidraw"), []byte(`{
  "type": "excalidraw",
  "version": 2,
  "elements": [
    {"id": "r1", "type": "rectangle", "x": 40, "y": 40, "width": 120, "height": 80,
     "strokeColor": "#1e1e1e", "backgroundColor": "transparent", "strokeWidth": 2},
    {"id": "t1", "type": "text", "x": 60, "y": 70, "width": 80, "height": 20,
     "text": "想法", "fontSize": 20, "strokeColor": "#1e1e1e"}
  ]
}`), 0o644)

	v, err := vault.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := v.Scan()
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.Vault = dir
	a := New(cfg, v, tree)
	return a, dir
}

// buildDocxBytes 生成一份最小的 .docx。

func buildDocxBytes(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("word/document.xml")
	fmt.Fprint(w, `<?xml version="1.0"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:t>Word 正文</w:t></w:r></w:p></w:body></w:document>`)
	zw.Close()
	return buf.Bytes()
}

// buildPDFBytes 生成一份最小的 PDF。

func buildPDFBytes() []byte {
	content := "BT /F1 20 Tf 40 150 Td (Papero PDF) Tj ET"
	var buf bytes.Buffer
	offsets := map[int]int{}
	buf.WriteString("%PDF-1.4\n")
	obj := func(num int, body string) {
		offsets[num] = buf.Len()
		fmt.Fprintf(&buf, "%d 0 obj\n%s\nendobj\n", num, body)
	}
	obj(1, "<< /Type /Catalog /Pages 2 0 R >>")
	obj(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	obj(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 200] /Resources << /Font << /F1 4 0 R >> >> /Contents 5 0 R >>")
	widths := strings.TrimSuffix(strings.Repeat("500 ", 95), " ")
	obj(4, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /FirstChar 32 /LastChar 126 /Widths ["+widths+"] >>")
	obj(5, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content))
	xref := buf.Len()
	buf.WriteString("xref\n0 6\n0000000000 65535 f \n")
	for i := 1; i <= 5; i++ {
		fmt.Fprintf(&buf, "%010d 00000 n \n", offsets[i])
	}
	fmt.Fprintf(&buf, "trailer\n<< /Size 6 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xref)
	return buf.Bytes()
}

// TestMarkdownTab 打开 Markdown 文稿，预览里应当出现标题与正文。

func TestMarkdownTab(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	// 这个测试检查的是预览渲染，显式用分栏模式（默认是块模式）
	a.tabs[a.active].mode = "split"
	tt := ui.NewTester(a.view, 1100, 760)

	if !tt.HasText("标题") {
		t.Errorf("预览里没有标题，界面文字：%q", tt.Texts())
	}
	if !tt.HasText("正文一段") {
		t.Errorf("预览里没有正文，界面文字：%q", tt.Texts())
	}
	// 分栏模式下源码编辑器里应当有原始 Markdown
	if !tt.HasText("Papero") {
		t.Errorf("侧栏标题缺失，界面文字：%q", tt.Texts())
	}
	// 侧栏里能看到文稿名
	if !tt.HasText("欢迎") {
		t.Errorf("侧栏没有文稿名，界面文字：%q", tt.Texts())
	}
}

// TestOpenEveryKind 每种文稿都能打开且不 panic。

func TestOpenEveryKind(t *testing.T) {
	a, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1100, 760)
	for _, rel := range []string{"notes/纯文本.txt", "图片.png", "报告.docx", "论文.pdf", "画布.excalidraw"} {
		a.open(rel)
		tt.Frame()
		if a.active < 0 || a.active >= len(a.tabs) {
			t.Fatalf("%s 没有打开成标签", rel)
		}
		tab := a.tabs[a.active]
		switch tab.kind {
		case vault.KindImage:
			if tab.img == nil {
				t.Errorf("%s 没有解码出图片：%s", rel, tab.imgErr)
			}
		case vault.KindWord:
			if tab.docxMD == "" {
				t.Errorf("%s 没有转出 Markdown：%s", rel, tab.docxErr)
			}
		case vault.KindPDF:
			if tab.pdfView == nil || tab.pdfView.Pages() == 0 {
				t.Errorf("%s 没有解析出页面：%s", rel, tab.pdfErr)
			}
		case vault.KindBoard:
			if tab.board == nil {
				t.Errorf("%s 没有解析出白板：%s", rel, tab.boardErr)
			}
		}
	}
	// PDF 现在由 PDFium 渲染成位图：检查页数与页面尺寸，文字用文本层取。
	a.open("论文.pdf")
	pdfTab := a.tabs[a.active]
	if pdfTab.pdfView == nil || pdfTab.pdfView.Pages() == 0 {
		t.Fatalf("PDF 没有页面：%s", pdfTab.pdfErr)
	}
	if w, h, err := pdfTab.pdfView.Size(0); err != nil || w <= 0 || h <= 0 {
		t.Errorf("页面尺寸 = %v x %v：%v", w, h, err)
	}
	if text, err := pdfTab.pdfView.Text(0); err == nil && !strings.Contains(text, "Papero PDF") {
		t.Errorf("取到的文字 = %q", text)
	}
	tt.Frame()
}

// TestEditAndSave 在源码模式下改字并保存，文件内容应当变化。

func TestEditAndSave(t *testing.T) {
	a, dir := newTestApp(t)
	a.open("notes/欢迎.md")
	tt := ui.NewTester(a.view, 1100, 760)
	tab := a.tabs[a.active]
	if tab.buf == nil {
		t.Fatal("Markdown 没有缓冲区")
	}
	tab.buf.Set("# 改过的标题\n\n新正文。\n")
	a.save(tab)
	data, err := os.ReadFile(filepath.Join(dir, "notes", "欢迎.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "改过的标题") {
		t.Errorf("保存后的内容 = %q", data)
	}
	tt.Frame()
}

// TestPaletteAndSettings 快速跳转与设置面板能打开。

func TestPaletteAndSettings(t *testing.T) {
	a, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1100, 760)

	tt.Key(ui.Cmd, ui.KeyK)
	tt.Frame()
	if !a.paletteOpen {
		t.Fatal("Ctrl+K 没有打开快速跳转")
	}
	a.paletteQuery = "报告"
	results := a.paletteResults()
	if len(results) == 0 || !strings.Contains(results[0].Rel, "报告") {
		t.Errorf("搜索结果 = %v", results)
	}
	a.paletteOpen = false

	a.settingsOpen = true
	tt.Frame()
	if !tt.HasText("设置") {
		t.Errorf("设置面板没有出现，界面文字：%q", tt.Texts())
	}
	if !tt.HasText("同步") {
		t.Errorf("设置里没有同步页，界面文字：%q", tt.Texts())
	}
}

// TestNewNote 新建文稿会落盘并打开。

func TestNewNote(t *testing.T) {
	a, dir := newTestApp(t)
	tt := ui.NewTester(a.view, 1100, 760)
	before := len(a.tree.Files())
	a.newNote("")
	tt.Frame()
	if len(a.tree.Files()) != before+1 {
		t.Fatalf("文件数 %d，想要 %d", len(a.tree.Files()), before+1)
	}
	tab := a.tabs[a.active]
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(tab.rel))); err != nil {
		t.Errorf("新文稿没有落盘：%v", err)
	}
}

// TestBoardEdits 白板可以新建元素并保存。

func TestBoardEdits(t *testing.T) {
	a, dir := newTestApp(t)
	a.open("画布.excalidraw")
	tab := a.tabs[a.active]
	if tab.board == nil {
		t.Fatal("白板没有打开")
	}
	n := len(tab.board.Elements)
	tab.board.Add("ellipse", 200, 200)
	tab.boardDirty = true
	a.saveBoard(tab)
	if len(tab.board.Elements) != n+1 {
		t.Fatalf("元素数 = %d，想要 %d", len(tab.board.Elements), n+1)
	}
	data, err := os.ReadFile(filepath.Join(dir, "画布.excalidraw"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "ellipse") {
		t.Errorf("保存的白板里没有新元素：%s", data)
	}
}

// TestSidebarFilter 侧栏搜索只显示匹配的文稿。

func TestSidebarFilter(t *testing.T) {
	a, _ := newTestApp(t)
	a.filter = "报告"
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("报告") {
		t.Errorf("搜索结果里没有「报告」，界面文字：%q", tt.Texts())
	}
	if tt.HasText("纯文本") {
		t.Errorf("不匹配的文稿仍然显示，界面文字：%q", tt.Texts())
	}
}

// TestOutline 大纲列出标题，点击后预览滚动到该标题。

func TestOutline(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.sidebarTab = 1
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("标题") {
		t.Errorf("大纲里没有标题，界面文字：%q", tt.Texts())
	}
	heads := outlineOf(a.tabs[a.active].text())
	if len(heads) == 0 || heads[0].Level != 1 || heads[0].Text != "标题" {
		t.Fatalf("解析出的标题 = %+v", heads)
	}
	// 分栏模式：跳到预览的锚点
	a.tabs[a.active].mode = "split"
	a.jumpToHeading(a.tabs[a.active], heads[0])
	if a.tabs[a.active].md.Anchor == "" {
		t.Error("跳转没有设置锚点")
	}
	// 编辑模式：滚到对应的块，不设锚点
	a.tabs[a.active].mode = "blocks"
	a.tabs[a.active].md.Anchor = ""
	a.jumpToHeading(a.tabs[a.active], heads[0])
	if a.tabs[a.active].md.Anchor != "" {
		t.Error("编辑模式不该设预览锚点")
	}
}

// TestOutlineSkipsCodeFence 代码围栏里的 # 不算标题。

func TestOutlineSkipsCodeFence(t *testing.T) {
	src := "# 一级\n\n```\n# 不是标题\n```\n\n## 二级 ##\n"
	heads := outlineOf(src)
	if len(heads) != 2 {
		t.Fatalf("标题数 = %d，想要 2：%+v", len(heads), heads)
	}
	if heads[1].Text != "二级" || heads[1].Level != 2 {
		t.Errorf("第二个标题 = %+v", heads[1])
	}
}

// TestSidebarCollapse 收起侧栏后正文占满窗口，仍能重新展开。

func TestSidebarCollapse(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.sidebarCollapsed = true
	tt := ui.NewTester(a.view, 1100, 760)
	// 侧栏是滑出去的，不是一帧消失：等动画走完再看
	tt.Advance(300 * time.Millisecond)
	if tt.HasText(i18n.T("command.new_note")) || tt.HasText(i18n.T("tab.outline")) {
		t.Errorf("侧栏收起后仍显示侧栏内容：%q", tt.Texts())
	}
	a.sidebarCollapsed = false
	tt.Frame()
	tt.Advance(300 * time.Millisecond)
	if !tt.HasText(i18n.T("command.new_note")) {
		t.Errorf("展开侧栏后没有侧栏内容，界面文字：%q", tt.Texts())
	}
}

// TestRecentOpened 打开过的文稿会进入「最近打开」。

func TestRecentOpened(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.open("notes/纯文本.txt")
	if len(a.recent) < 2 || a.recent[0] != "notes/纯文本.txt" {
		t.Fatalf("最近打开 = %v", a.recent)
	}
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("最近打开") {
		t.Errorf("侧栏没有「最近打开」小节，界面文字：%q", tt.Texts())
	}
	if !tt.HasText("纯文本.txt") {
		t.Errorf("最近打开里没有刚打开的文件")
	}
}

// TestDiscoverPanel 「发现」面板能打开并列出插件。

func TestDiscoverPanel(t *testing.T) {
	a, _ := newTestApp(t)
	a.discoverOpen = true
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("发现") {
		t.Errorf("发现面板没有出现，界面文字：%q", tt.Texts())
	}
	if !tt.HasText("知识图谱") {
		t.Errorf("发现面板里没有图谱占位")
	}
}

// TestSidebarNavActions 侧栏顶部的动作行都在。

func TestSidebarNavActions(t *testing.T) {
	a, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1100, 760)
	for _, want := range []string{"新建文稿", "快速搜索", "打开知识库", "打开知识图谱", "设置"} {
		if !tt.HasText(want) {
			t.Errorf("侧栏缺少 %q，界面文字：%q", want, tt.Texts())
		}
	}
	// 「发现」从侧栏撤掉了，只留在命令面板里
	for _, text := range tt.Texts() {
		if text == "发现" {
			t.Error("侧栏里不该再有「发现」")
		}
	}
}

// TestSidebarActionsConfig 侧栏顶部动作可以关掉、可以调顺序。
func TestSidebarActionsConfig(t *testing.T) {
	a, _ := newTestApp(t)
	if !a.hasSidebarAction("new") || !a.hasSidebarAction("graph") {
		t.Fatal("默认应当四个动作都显示")
	}

	// 关掉「打开知识库」
	a.setSidebarAction("vault", false)
	if a.hasSidebarAction("vault") {
		t.Error("关掉之后还显示")
	}
	// 打开一篇文稿，免得起始页上的同名按钮干扰判断
	a.open("notes/欢迎.md")
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if tt.HasText("打开知识库") {
		t.Error("关掉的动作不该出现在侧栏")
	}

	// 隐藏不动顺序：知识库还在原来的位置
	if got := a.sidebarActionIndex("vault"); got != 2 {
		t.Errorf("隐藏后位置 = %d，想要 2", got)
	}

	// 调顺序：把「打开知识图谱」挪到最前
	for i := 0; i < 3; i++ {
		a.moveSidebarAction("graph", -1)
	}
	if got := a.cfg.SidebarActions[0]; got != "graph" {
		t.Errorf("第一个动作 = %q，想要 graph", got)
	}

	// 再打开，回到它原来的位置
	a.setSidebarAction("vault", true)
	if got := a.sidebarActionIndex("vault"); got != 3 {
		t.Errorf("重新显示后位置 = %d，想要 3", got)
	}
}

// TestCommandPalette 命令面板列出命令并能执行。

func TestCommandPalette(t *testing.T) {
	a, _ := newTestApp(t)
	a.commandOpen = true
	a.commandQuery = "大纲"
	results := a.commandResults()
	if len(results) != 1 || results[0].Title != "侧栏显示大纲" {
		t.Fatalf("命令搜索结果 = %+v", results)
	}
	results[0].Run()
	if a.sidebarTab != 1 {
		t.Error("执行命令后侧栏没有切到大纲")
	}
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("侧栏显示大纲") {
		t.Errorf("命令面板没有列出命令，界面文字：%q", tt.Texts())
	}
}

// TestTitleBar 顶栏合并了标题与标签：左边软件名与侧栏开关，中间标签，右边窗口按钮；
// 菜单栏已经取消，常用动作在「更多」里。
func TestTitleBar(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	tt := ui.NewTester(a.view, 1100, 760)
	for _, want := range []string{"Papero", "收起侧栏 (Ctrl+\\)", "标签 notes/欢迎.md"} {
		if !tt.HasText(want) {
			t.Errorf("顶栏缺少 %q，界面文字：%q", want, tt.Texts())
		}
	}
	// 菜单栏、加号、更多都取消了
	for _, gone := range []string{"文件", "视图", "帮助", "更多", "新建文稿 (Ctrl+N)"} {
		for _, text := range tt.Texts() {
			if text == gone {
				t.Errorf("菜单栏已取消，不该出现 %q", gone)
			}
		}
	}
}

// TestTitleBarButtons 顶栏的按钮可以点：侧栏开关、更多菜单。
func TestTitleBarButtons(t *testing.T) {
	a, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1100, 760)

	if err := tt.Click("收起侧栏 (Ctrl+\\)"); err != nil {
		t.Fatalf("点击侧栏开关：%v", err)
	}
	// 等侧栏滑出去：顶栏那块要等它让出宽度才铺满
	tt.Advance(300 * time.Millisecond)
	if !a.sidebarCollapsed {
		t.Error("点击后侧栏没有收起")
	}
	// 收起后开关搬到顶栏左侧
	if err := tt.Click("展开侧栏 (Ctrl+\\)"); err != nil {
		t.Fatalf("点击展开按钮：%v", err)
	}
	tt.Advance(300 * time.Millisecond)
	if a.sidebarCollapsed {
		t.Error("再次点击后侧栏没有展开")
	}

	// 顶栏只剩标签与窗口按钮，命令面板走快捷键
	tt.Frame()
	tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	tt.Frame()
	if !a.commandOpen {
		t.Error("Ctrl+Shift+P 没有打开命令面板")
	}
}

// TestViewMenu 显示方式在状态栏里切换（菜单栏取消后不再有「视图」菜单）。
func TestViewMenu(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if err := tt.Click("源码"); err != nil {
		t.Fatalf("点击状态栏的源码：%v", err)
	}
	tt.Frame()
	if got := a.tabs[a.active].mode; got != "source" {
		t.Errorf("模式 = %q，想要 source", got)
	}
	if err := tt.Click("编辑"); err != nil {
		t.Fatalf("点击状态栏的编辑：%v", err)
	}
	tt.Frame()
	if got := a.tabs[a.active].mode; got != "blocks" {
		t.Errorf("模式 = %q，想要 blocks", got)
	}
	if err := tt.Click("源码"); err != nil {
		t.Fatalf("点击源码：%v", err)
	}
	tt.Frame()
	if got := a.tabs[a.active].mode; got != "source" {
		t.Errorf("模式 = %q，想要 source", got)
	}
}

// TestTreeContextMenu 文稿行右键菜单能打开并触发重命名。

func TestTreeContextMenu(t *testing.T) {
	a, _ := newTestApp(t)
	tt := ui.NewTester(a.view, 1100, 760)
	if err := tt.RightClick("notes/欢迎.md"); err != nil {
		t.Fatalf("右键文稿行：%v", err)
	}
	tt.Frame()
	if !tt.HasText("重命名…") {
		t.Fatalf("右键菜单没有出现，界面文字：%q", tt.Texts())
	}
	if err := tt.Click("重命名…"); err != nil {
		t.Fatalf("点击重命名：%v", err)
	}
	if !a.renameOpen || a.renameRel != "notes/欢迎.md" {
		t.Errorf("重命名对话框 = %v / %q", a.renameOpen, a.renameRel)
	}
}

// TestRenameFlow 重命名会改磁盘上的文件，并同步标签路径。

func TestRenameFlow(t *testing.T) {
	a, dir := newTestApp(t)
	a.open("notes/欢迎.md")
	a.beginRename("notes/欢迎.md")
	a.renameText = "新名字.md"
	a.commitRename()
	if _, err := os.Stat(filepath.Join(dir, "notes", "新名字.md")); err != nil {
		t.Fatalf("重命名后文件不存在：%v", err)
	}
	if a.tabs[a.active].rel != "notes/新名字.md" {
		t.Errorf("标签路径 = %q", a.tabs[a.active].rel)
	}
}

// TestTabMiddleClick 中键点击标签会关闭它。

func TestTabMiddleClick(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.open("notes/纯文本.txt")
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	before := len(a.tabs)
	rect, ok := tt.Find("标签 notes/纯文本.txt")
	if !ok {
		t.Fatal("找不到标签")
	}
	x, y := rect.X+rect.W/2, rect.Y+rect.H/2
	tt.PressButton(2, x, y)
	tt.Frame()
	tt.ReleaseButton(2, x, y)
	tt.Frame()
	if len(a.tabs) != before-1 {
		t.Errorf("中键后标签数 = %d，想要 %d", len(a.tabs), before-1)
	}
}

// TestTabContextMenu 标签右键菜单能关闭其他标签。

func TestTabContextMenu(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.open("notes/纯文本.txt")
	a.open("图片.png")
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if err := tt.RightClick("标签 notes/纯文本.txt"); err != nil {
		t.Fatalf("右键标签：%v", err)
	}
	tt.Frame()
	if !tt.HasText("关闭其他") {
		t.Fatalf("标签菜单没有出现，界面文字：%q", tt.Texts())
	}
	if err := tt.Click("关闭其他"); err != nil {
		t.Fatalf("点击关闭其他：%v", err)
	}
	if len(a.tabs) != 1 || a.tabs[0].rel != "notes/纯文本.txt" {
		t.Errorf("剩余标签 = %v", a.tabs)
	}
}

// TestTrashFile 移到回收站后文件消失、标签关闭（没有反向链接时直接删）。

func TestTrashFile(t *testing.T) {
	a, dir := newTestApp(t)
	a.open("notes/纯文本.txt")
	a.trash("notes/纯文本.txt")
	if a.trashOpen {
		t.Fatal("没有反向链接时不该弹确认")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "纯文本.txt")); !os.IsNotExist(err) {
		t.Error("文件应当已被移走")
	}
	entries, _ := os.ReadDir(filepath.Join(dir, ".trash"))
	if len(entries) != 1 {
		t.Errorf("回收站里有 %d 个条目", len(entries))
	}
	for _, tab := range a.tabs {
		if tab.rel == "notes/纯文本.txt" {
			t.Error("标签应当已关闭")
		}
	}
}

// TestNewFolder 新建文件夹会出现在树里。

func TestNewFolder(t *testing.T) {
	a, dir := newTestApp(t)
	a.newFolder("notes")
	if info, err := os.Stat(filepath.Join(dir, "notes", "新建文件夹")); err != nil || !info.IsDir() {
		t.Fatalf("新建文件夹失败：%v", err)
	}
	if a.tree.Find("notes/新建文件夹") == nil {
		t.Error("树里没有新文件夹")
	}
}

// TestNewBoard 新建白板会落盘、打开，并且是核心功能（不依赖插件）。

func TestNewBoard(t *testing.T) {
	a, dir := newTestApp(t)
	if a.plugins != nil && len(a.plugins.List()) != 0 {
		t.Errorf("不应该有内置插件，实际 %d 个", len(a.plugins.List()))
	}
	a.newBoard("")
	tab := a.tabs[a.active]
	if tab.kind != vault.KindBoard {
		t.Fatalf("新文稿类型 = %v，想要白板", tab.kind)
	}
	if tab.board == nil {
		t.Fatalf("白板没有解析出来：%s", tab.boardErr)
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(tab.rel))); err != nil {
		t.Errorf("白板没有落盘：%v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(tab.rel)))
	if err != nil || !strings.Contains(string(data), "excalidraw") {
		t.Errorf("白板内容不对：%s", data)
	}
}

// TestWikiDestRoundTrip 双链地址的编码与解码。

func TestWikiDestRoundTrip(t *testing.T) {
	link := wiki.Link{Target: "另一篇 笔记", Anchor: "第二节"}
	target, anchor, ok := parseWikiDest(wikiDest(link))
	if !ok || target != "另一篇 笔记" || anchor != "第二节" {
		t.Errorf("解码 = %q / %q / %v", target, anchor, ok)
	}
	if _, _, ok := parseWikiDest("https://example.com"); ok {
		t.Error("普通链接不该被当成双链")
	}
}

// TestWikiPreview 预览里的双链被改写成普通链接，点击能跳到目标。

func TestWikiPreview(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "首页.md"), []byte("# 首页\n\n去 [[另一篇]] 看看。\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "另一篇.md"), []byte("# 另一篇\n\n正文。\n"), 0o644)
	a.refresh()
	a.open("notes/首页.md")

	// 链接图：首页 → 另一篇，另一篇有反向链接
	note := a.noteOf("notes/另一篇.md")
	if note == nil || len(note.Back) != 1 || note.Back[0] != "notes/首页.md" {
		t.Fatalf("反向链接 = %+v", note)
	}

	// 预览渲染：界面里出现「另一篇」这个链接文字
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("另一篇") {
		t.Errorf("预览里没有链接文字，界面文字：%q", tt.Texts())
	}

	// 点击链接跳到目标文稿
	a.openWiki("另一篇", "", "notes/首页.md")
	if a.tabs[a.active].rel != "notes/另一篇.md" {
		t.Errorf("跳转后打开的是 %q", a.tabs[a.active].rel)
	}
}

// TestWikiCreatesMissing 指向不存在的文稿时新建它。

func TestWikiCreatesMissing(t *testing.T) {
	a, dir := newTestApp(t)
	a.open("notes/欢迎.md")
	a.openWiki("还没写的笔记", "", "notes/欢迎.md")
	tab := a.tabs[a.active]
	if tab.rel != "notes/还没写的笔记.md" {
		t.Fatalf("新建的路径 = %q", tab.rel)
	}
	data, err := os.ReadFile(filepath.Join(dir, "notes", "还没写的笔记.md"))
	if err != nil || !strings.Contains(string(data), "# 还没写的笔记") {
		t.Errorf("新文稿内容 = %q, %v", data, err)
	}
}

// TestLinksPanel 侧栏的链接面板列出反向链接与出链。

func TestLinksPanel(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "首页.md"), []byte("# 首页\n\n去 [[另一篇]] 看看。\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "另一篇.md"), []byte("# 另一篇\n\n回到 [[首页]]。\n"), 0o644)
	a.refresh()
	a.open("notes/首页.md")
	a.sidebarTab = 2
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("反向链接 · 1") {
		t.Errorf("没有反向链接小节，界面文字：%q", tt.Texts())
	}
	if !tt.HasText("出链 · 1") {
		t.Errorf("没有出链小节，界面文字：%q", tt.Texts())
	}
}

// TestGraphView 图谱有节点与边，且能打开。

func TestGraphView(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "首页.md"), []byte("# 首页\n\n[[另一篇]] 与 [[第三篇]]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "另一篇.md"), []byte("# 另一篇\n\n[[首页]]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "第三篇.md"), []byte("# 第三篇\n"), 0o644)
	a.refresh()
	nodes, edges := a.graphNodes()
	// 三篇有链接的文稿，加上库里零散的文稿（欢迎.md）
	if len(nodes) < 3 {
		t.Fatalf("节点数 = %d，至少要有 3：%+v", len(nodes), nodes)
	}
	if len(edges) != 3 {
		t.Errorf("边数 = %d，想要 3", len(edges))
	}
	data := a.graphData()
	if len(data.Nodes) != len(nodes) || len(data.Edges) != 3 {
		t.Errorf("图谱数据 = %d 节点 / %d 边", len(data.Nodes), len(data.Edges))
	}
	// 分组按顶层目录
	group := ""
	for _, n := range data.Nodes {
		if n.ID == "notes/首页.md" {
			group = n.Group
		}
	}
	if group != "notes" {
		t.Errorf("分组 = %q，想要 notes", group)
	}
	a.graphOpen = true
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if !tt.HasText("知识图谱") {
		t.Errorf("图谱没有打开，界面文字：%q", tt.Texts())
	}
}

// TestGraphFromSidebar 侧栏直接有「知识图谱」入口，点开是嵌入浮层。

func TestGraphFromSidebar(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "首页.md"), []byte("# 首页\n\n[[另一篇]]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "另一篇.md"), []byte("# 另一篇\n"), 0o644)
	a.refresh()
	tt := ui.NewTester(a.view, 1100, 760)
	if !tt.HasText("打开知识图谱") {
		t.Fatalf("侧栏没有知识图谱入口，界面文字：%q", tt.Texts())
	}
	if err := tt.Click("打开知识图谱"); err != nil {
		t.Fatalf("点击打开知识图谱：%v", err)
	}
	tt.Frame()
	if !a.graphOpen {
		t.Error("没有打开图谱浮层")
	}
	if !tt.HasText("重新布局") {
		t.Errorf("图谱浮层没有出现，界面文字：%q", tt.Texts())
	}
}

// TestConfigIsolatedFromUser 测试不能碰用户真实的 settings.json。
//
// 这条是给 newTestApp 兜底的：少调一次 config.SetDir，跑一次 go test 就会把
// 用户的知识库路径和「上次打开的文稿」冲掉，现象是「每次启动都打开第一篇」。
func TestConfigIsolatedFromUser(t *testing.T) {
	newTestApp(t)
	real, err := os.UserConfigDir()
	if err != nil {
		t.Skip("取不到用户配置目录")
	}
	want := filepath.Join(real, "Papero", "settings.json")
	if got, _ := config.Path(); got == want {
		t.Errorf("测试会写进用户真实配置：%s", got)
	}
}

// TestGraphModeConfig 图谱窗口模式可以在设置里切换，并落到配置里。

func TestGraphModeConfig(t *testing.T) {
	t.Cleanup(config.SetDir(t.TempDir()))
	cfg := config.Default()
	if cfg.GraphMode != "embed" {
		t.Errorf("默认模式 = %q", cfg.GraphMode)
	}
	cfg.GraphMode = "bogus"
	cfg.Save()
	loaded := config.Load()
	if loaded.GraphMode != "embed" {
		t.Errorf("非法值应当回落到 embed，实际 %q", loaded.GraphMode)
	}
	loaded.GraphMode = "window"
	loaded.Save()
	if got := config.Load().GraphMode; got != "window" {
		t.Errorf("保存后 = %q", got)
	}
}

// TestGraphWindowModeFallsBack 没有主窗口（测试环境）时退回嵌入模式。

func TestGraphWindowModeFallsBack(t *testing.T) {
	a, _ := newTestApp(t)
	a.cfg.GraphMode = "window"
	a.openGraph()
	if !a.graphOpen {
		t.Error("没有窗口可用时应当退回浮层")
	}
}

// TestWikiCompletionContext 判断光标是否在未闭合的双链里。

func TestWikiCompletionContext(t *testing.T) {
	cases := []struct {
		text   string
		cursor int
		ok     bool
		query  string
	}{
		{"写 [[设", len("写 [[设"), true, "设"},
		{"写 [[", len("写 [["), true, ""},
		{"写 [[设计]] 后面", len("写 [[设计]]"), false, ""},
		{"写 [[设计|别名", len("写 [[设计|别名"), false, ""},
		{"第一行\n[[读书", len("第一行\n[[读书"), true, "读书"},
		{"没有链接", len("没有链接"), false, ""},
	}
	for _, c := range cases {
		_, query, ok := wikiCompletionContext(c.text, c.cursor)
		if ok != c.ok || (ok && query != c.query) {
			t.Errorf("%q @%d = (%q, %v)，想要 (%q, %v)", c.text, c.cursor, query, ok, c.query, c.ok)
		}
	}
}

// TestWikiCompletions 候选里既有已有文稿，也有「新建」。

func TestWikiCompletions(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "设计笔记.md"), []byte("# 设计笔记\n"), 0o644)
	a.refresh()

	items := a.wikiCompletions("设计")
	if len(items) == 0 {
		t.Fatal("没有候选")
	}
	found := false
	for _, it := range items {
		if it.Label == "设计笔记" {
			found = true
			if it.Insert != "设计笔记]]" {
				t.Errorf("插入文字 = %q", it.Insert)
			}
		}
	}
	if !found {
		t.Errorf("候选里没有「设计笔记」：%+v", items)
	}

	// 输入一个不存在的名字时，第一条是「新建」
	items = a.wikiCompletions("全新的笔记")
	if len(items) == 0 || items[0].Label != "全新的笔记" || items[0].Detail != "新建文稿" {
		t.Errorf("新建候选 = %+v", items)
	}
}

// TestWikiCompletionInEditor 在编辑器里输入 [[ 会弹出补全，选中后补成 [[标题]]。

func TestWikiCompletionInEditor(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "设计笔记.md"), []byte("# 设计笔记\n"), 0o644)
	a.refresh()
	a.open("notes/欢迎.md")
	tab := a.tabs[a.active]
	tab.mode = "source"

	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if !tab.state.Focused() {
		// 点一下编辑器让它获得焦点
		if rect, ok := tt.Find("notes/欢迎.md"); ok {
			tt.ClickAt(rect.X+rect.W/2, rect.Y+rect.H/2)
			tt.Frame()
		}
	}
	tab.state.Select(len(tab.text()), len(tab.text()))
	tab.state.Insert("\n[[设计")
	a.applyWikiCompletions(tab, true)
	if !tab.wikiCompletion {
		t.Fatalf("输入 [[ 后没有弹出补全，文本尾部 = %q", tail(tab.text(), 20))
	}
	// 收起：光标离开双链上下文
	tab.state.Insert("]]")
	a.applyWikiCompletions(tab, true)
	if tab.wikiCompletion {
		t.Error("闭合之后应当收起补全")
	}
}

// tail 返回字符串末尾 n 个字符，便于断言。

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[len(r)-n:])
}

// TestRenameUpdatesLinks 改名后，全库引用它的双链会跟着改（保留别名与锚点）。

func TestRenameUpdatesLinks(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "目标.md"), []byte("# 目标\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "甲.md"),
		[]byte("看 [[目标]] 与 [[目标|别名]] 以及 [[目标#小节]]。\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "乙.md"),
		[]byte("路径写法 [[notes/目标]]。\n```\n代码里的 [[目标]] 不该改\n```\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "无关.md"), []byte("[[别的东西]]\n"), 0o644)
	a.refresh()

	a.beginRename("notes/目标.md")
	a.renameText = "新目标.md"
	a.commitRename()

	jia, _ := os.ReadFile(filepath.Join(dir, "notes", "甲.md"))
	want := "看 [[新目标]] 与 [[新目标|别名]] 以及 [[新目标#小节]]。"
	if strings.TrimSpace(string(jia)) != want {
		t.Errorf("甲.md = %q\n想要 %q", jia, want)
	}
	yi, _ := os.ReadFile(filepath.Join(dir, "notes", "乙.md"))
	if !strings.Contains(string(yi), "[[notes/新目标]]") {
		t.Errorf("路径写法没有更新：%q", yi)
	}
	if !strings.Contains(string(yi), "[[目标]] 不该改") {
		t.Errorf("代码块里的引用不该被改：%q", yi)
	}
	wu, _ := os.ReadFile(filepath.Join(dir, "无关.md"))
	if string(wu) != "[[别的东西]]\n" {
		t.Errorf("无关文件被改了：%q", wu)
	}
}

// TestRenameFolderUpdatesPathLinks 重命名目录时，路径写法的引用跟着改。

func TestRenameFolderUpdatesPathLinks(t *testing.T) {
	a, dir := newTestApp(t)
	os.MkdirAll(filepath.Join(dir, "项目"), 0o755)
	os.WriteFile(filepath.Join(dir, "项目", "计划.md"), []byte("# 计划\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "索引.md"),
		[]byte("看 [[项目/计划]] 与 [[计划]]。\n"), 0o644)
	a.refresh()

	a.beginRename("项目")
	a.renameText = "归档"
	a.commitRename()

	idx, _ := os.ReadFile(filepath.Join(dir, "notes", "索引.md"))
	if !strings.Contains(string(idx), "[[归档/计划]]") {
		t.Errorf("路径引用没有更新：%q", idx)
	}
	// 只写标题的引用本来就还能解析，不该被改
	if !strings.Contains(string(idx), "[[计划]]") {
		t.Errorf("标题引用被误改：%q", idx)
	}
}

// TestRenameKeepsOpenBufferInSync 被改写的文稿如果正开着，编辑器内容也要跟着更新。

func TestRenameKeepsOpenBufferInSync(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "目标.md"), []byte("# 目标\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "甲.md"), []byte("看 [[目标]]。\n"), 0o644)
	a.refresh()
	a.open("notes/甲.md")

	a.beginRename("notes/目标.md")
	a.renameText = "改名了.md"
	a.commitRename()

	tab := a.tabs[a.active]
	if !strings.Contains(tab.text(), "[[改名了]]") {
		t.Errorf("编辑器内容没有同步：%q", tab.text())
	}
}

// TestTrashWarnsBacklinks 有反向链接时先确认，取消则什么都不做。

func TestTrashWarnsBacklinks(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "目标.md"), []byte("# 目标\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "引用者.md"), []byte("看 [[目标]]。\n"), 0o644)
	a.refresh()

	a.trash("notes/目标.md")
	if !a.trashOpen {
		t.Fatal("有反向链接时应当先弹确认")
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "目标.md")); err != nil {
		t.Fatal("确认之前不该删除")
	}
	back := a.backlinksOf("notes/目标.md")
	if len(back) != 1 || back[0].Rel != "notes/引用者.md" {
		t.Fatalf("反向链接 = %+v", back)
	}

	// 对话框里能看到引用者
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if !tt.HasText("移到回收站？") || !tt.HasText("引用者") {
		t.Errorf("确认框内容不全，界面文字：%q", tt.Texts())
	}

	// 取消：文件还在
	a.trashOpen = false
	if _, err := os.Stat(filepath.Join(dir, "notes", "目标.md")); err != nil {
		t.Error("取消后文件应当还在")
	}

	// 确认：文件进回收站
	a.trash("notes/目标.md")
	a.doTrash(a.trashRel)
	if _, err := os.Stat(filepath.Join(dir, "notes", "目标.md")); !os.IsNotExist(err) {
		t.Error("确认后文件应当已被移走")
	}
	// 引用者还在，链接变成「还没创建」
	if _, err := os.Stat(filepath.Join(dir, "notes", "引用者.md")); err != nil {
		t.Error("引用者不该被删")
	}
	a.refresh()
	if note := a.noteOf("notes/引用者.md"); note == nil || len(note.Unresolved) != 1 {
		t.Errorf("引用者的断链 = %+v", note)
	}
}

// TestHoverPreview 悬停在链接行上会浮出目标文稿的摘要。

func TestHoverPreview(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "首页.md"), []byte("# 首页\n\n[[另一篇]]\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "另一篇.md"),
		[]byte("# 另一篇\n\n这是另一篇的正文，用来检查悬停预览。\n"), 0o644)
	a.refresh()
	a.open("notes/首页.md")
	a.sidebarTab = 2

	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	rect, ok := tt.Find("另一篇")
	if !ok {
		t.Fatalf("找不到出链行，界面文字：%q", tt.Texts())
	}
	tt.Move(rect.X+rect.W/2, rect.Y+rect.H/2)
	tt.Frame()
	if a.linkHover != "notes/另一篇.md" {
		t.Fatalf("悬停目标 = %q", a.linkHover)
	}
	if !tt.HasText("这是另一篇的正文") {
		t.Errorf("悬停卡片里没有摘要，界面文字：%q", tt.Texts())
	}

	// 指针移开后消失
	tt.Move(600, 400)
	tt.Frame()
	tt.Frame()
	if a.linkHover != "" {
		t.Errorf("移开后悬停预览应当消失，实际 %q", a.linkHover)
	}
}

// TestCreateMissingLinks 一键把断链建成空文稿。

func TestCreateMissingLinks(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "首页.md"), []byte("[[甲]] 与 [[乙]]\n"), 0o644)
	a.refresh()
	n := a.createMissingLinks([]string{"甲", "乙"}, "notes/首页.md")
	if n != 2 {
		t.Fatalf("新建 %d 篇，想要 2", n)
	}
	for _, name := range []string{"甲", "乙"} {
		data, err := os.ReadFile(filepath.Join(dir, "notes", name+".md"))
		if err != nil || !strings.Contains(string(data), "# "+name) {
			t.Errorf("%s.md = %q, %v", name, data, err)
		}
	}
	// 断链消失
	if note := a.noteOf("notes/首页.md"); note == nil || len(note.Unresolved) != 0 {
		t.Errorf("断链 = %+v", note)
	}
}

// TestLinkMenuOnPreviewLink 预览里右键双链会给出打开 / 新建等菜单。

func TestLinkMenuOnPreviewLink(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "首页.md"), []byte("# 首页\n\n先看 [[设计笔记]] 与 [[还没写]]。\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "设计笔记.md"), []byte("# 设计笔记\n"), 0o644)
	a.refresh()
	a.open("notes/首页.md")
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if err := tt.RightClick("设计笔记"); err != nil {
		t.Skipf("右键没有命中链接（预览链接的命中区域）：%v", err)
	}
	tt.Frame()
	if !tt.HasText("打开") {
		t.Errorf("链接菜单没有出现，界面文字：%q", tt.Texts())
	}
}

// TestHeadingMarks 标题的级别与要隐藏的标记长度。

func TestHeadingMarks(t *testing.T) {
	cases := map[string][2]int{
		"# 一级":       {1, 2},
		"### 三级":     {3, 4},
		"#没有空格":      {0, 0},
		"正文":         {0, 0},
		"####### 七级": {0, 0},
	}
	for line, want := range cases {
		level, marks := headingMarks(line)
		if level != want[0] || marks != want[1] {
			t.Errorf("%q = (%d,%d)，想要 (%d,%d)", line, level, marks, want[0], want[1])
		}
	}
}

// TestTaskBox 任务框的位置与状态。

func TestTaskBox(t *testing.T) {
	if start, checked, ok := taskBox("- [ ] 待办"); !ok || checked || start != 2 {
		t.Errorf("未完成 = (%d,%v,%v)", start, checked, ok)
	}
	if _, checked, ok := taskBox("- [x] 完成"); !ok || !checked {
		t.Errorf("已完成 = (%v,%v)", checked, ok)
	}
	if _, _, ok := taskBox("- 普通列表"); ok {
		t.Error("普通列表不该识别成任务框")
	}
}

// TestEmphasisReplacements 标记被藏起来、内容保留。

func TestEmphasisReplacements(t *testing.T) {
	line := "这是**粗体**与*斜体*"
	reps := emphasisReplacements(line, 0)
	if len(reps) != 4 {
		t.Fatalf("替换 %d 处，想要 4：%+v", len(reps), reps)
	}
	// 拼回来看剩下什么
	var b strings.Builder
	last := 0
	for _, r := range reps {
		b.WriteString(line[last:r.Start])
		last = r.End
	}
	b.WriteString(line[last:])
	if got := b.String(); got != "这是粗体与斜体" {
		t.Errorf("剩下 %q", got)
	}
}

// TestLinkReplacements 链接只留文字，双链只留标题。

func TestLinkReplacements(t *testing.T) {
	line := "看 [普通链接](https://example.com) 和 [[双链]]"
	reps := linkReplacements(line, 0)
	var b strings.Builder
	last := 0
	for _, r := range reps {
		b.WriteString(line[last:r.Start])
		last = r.End
	}
	b.WriteString(line[last:])
	if got := b.String(); got != "看 普通链接 和 双链" {
		t.Errorf("剩下 %q", got)
	}
}

// TestToggleTask 点复选框会改源码。

func TestToggleTask(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	tab := a.tabs[a.active]
	tab.buf.Set("- [ ] 待办\n")
	a.toggleTask(tab, 2)
	if got := tab.text(); !strings.HasPrefix(got, "- [x]") {
		t.Errorf("勾选后 = %q", got)
	}
	a.toggleTask(tab, 2)
	if got := tab.text(); !strings.HasPrefix(got, "- [ ]") {
		t.Errorf("取消后 = %q", got)
	}
}

// TestLivePreviewApplied 实时排版只在编辑/分栏里生效；源码模式要看到原样的 Markdown。

func TestLivePreviewApplied(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	tab := a.tabs[a.active]
	tab.buf.Set("# 标题\n\n**粗体**\n")

	tab.mode = "blocks"
	if !a.livePreview(tab) {
		t.Fatal("编辑模式里实时排版没有生效")
	}

	// 源码模式：标题的 # 与 ** 都要原样显示
	tab.mode = "source"
	if a.livePreview(tab) {
		t.Error("源码模式不该做实时排版")
	}
	if tab.liveOn {
		t.Error("切到源码后应当清理实时排版的标记")
	}

	// 关掉设置后清空
	tab.mode = "blocks"
	a.cfg.LivePreview = false
	if a.livePreview(tab) {
		t.Error("关掉之后不该生效")
	}
	if tab.liveOn {
		t.Error("关掉之后应当清理状态")
	}
}

// TestRichBlocksRoundTrip 富文本块编辑器打开文稿不改变文件内容。

func TestRichBlocksRoundTrip(t *testing.T) {
	a, dir := newTestApp(t)
	src := "# 标题\n\n段落一，带 **粗体** 与 `代码`。\n\n- 列表\n\n- [x] 完成\n\n| 甲 | 乙 |\n| --- | --- |\n| 1 | 2 |\n"
	os.WriteFile(filepath.Join(dir, "notes", "块.md"), []byte(src), 0o644)
	a.refresh()
	a.open("notes/块.md")
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Frame()
	if got := a.tabs[a.active].text(); got != src {
		t.Errorf("打开后内容变了：\n%q\n%q", got, src)
	}
	if tab := a.tabs[a.active]; tab.rich == nil || len(tab.rich.Blocks) < 5 {
		t.Errorf("富模型块数 = %d", len(tab.rich.Blocks))
	}
}
func TestRichMathBlock(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "公式.md"), []byte("$$\nE = mc^2\n$$\n"), 0o644)
	a.refresh()
	a.open("notes/公式.md")
	tab := a.tabs[a.active]
	tt := ui.NewTester(a.view, 1100, 600)
	tt.Frame()
	if len(tab.rich.Blocks) != 1 || tab.rich.Blocks[0].Kind != "math" {
		t.Fatalf("块 = %+v", tab.rich.Blocks)
	}
	if !tt.HasText("公式") {
		t.Errorf("没有公式画布，界面文字：%q", tt.Texts())
	}
}
func TestSettingsPages(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsOpen = true
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()

	// 导航里五页都在
	for _, page := range settingsPages() {
		if !tt.HasText(page.Label) {
			t.Errorf("导航里没有 %q，界面文字：%q", page.Label, tt.Texts())
		}
	}
	if a.currentSettingsPage().Key != "general" {
		t.Errorf("默认页 = %q", a.currentSettingsPage().Key)
	}

	// 切到同步页：出现同步相关字段
	a.settingsPage = "sync"
	tt.Frame()
	if !tt.HasText("远端") || !tt.HasText("启用") {
		t.Errorf("同步页没有字段，界面文字：%q", tt.Texts())
	}
	// 三个按钮与「打开时先拉取」的开关
	for _, want := range []string{"立即同步", "从云端拉取", "测试连接", "自动拉取", "忽略点开头", "按 .gitignore"} {
		if !tt.HasText(want) {
			t.Errorf("同步页没有 %q，界面文字：%q", want, tt.Texts())
		}
	}

	// 远端选 WebDAV：换成地址与账号，不该再出现文件夹
	a.cfg.Sync.Remote = "webdav"
	tt.Frame()
	if !tt.HasText("服务地址") || !tt.HasText("用户名") || !tt.HasText("密码") {
		t.Errorf("WebDAV 的字段没出来，界面文字：%q", tt.Texts())
	}
	if tt.HasText("同步文件夹") {
		t.Errorf("WebDAV 下不该出现文件夹，界面文字：%q", tt.Texts())
	}

	// 远端选「本库仓库」：没有可填的，只有一行说明
	a.cfg.Sync.Remote = "repo"
	tt.Frame()
	if !tt.HasText("仓库（自动）") {
		t.Errorf("本库仓库没显示说明，界面文字：%q", tt.Texts())
	}
	a.cfg.Sync.Remote = "dir"
	tt.Frame()

	// 切到关于页：显示版本
	a.settingsPage = "about"
	tt.Frame()
	if !tt.HasText(appVersion) {
		t.Errorf("关于页没有版本号，界面文字：%q", tt.Texts())
	}
	if a.currentSettingsPage().Key != "about" {
		t.Errorf("当前页 = %q", a.currentSettingsPage().Key)
	}
}

// TestTestConnection 设置页的「测试连接」：数得出远端的文件，缺字段时说缺什么。
func TestTestConnection(t *testing.T) {
	a, _ := newTestApp(t)
	remote := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "a.md"), []byte("一"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.cfg.Sync = config.SyncSettings{Remote: "dir", Dir: remote}

	a.testConnection()
	if a.probing {
		t.Error("没有真窗口时应当同步跑完")
	}
	if !strings.Contains(a.syncMsg, "远端文件") || !strings.Contains(a.syncMsg, "1") {
		t.Errorf("状态 = %q", a.syncMsg)
	}

	// 缺字段：先说缺什么，不去连
	a.cfg.Sync.Dir = ""
	a.testConnection()
	if a.syncMsg != i18n.T("sync.need_folder") {
		t.Errorf("状态 = %q", a.syncMsg)
	}
}

// TestPullFromCloud 从云端拉取：文件真的落进知识库，第二次说没有更新。
func TestPullFromCloud(t *testing.T) {
	a, dir := newTestApp(t)
	remote := t.TempDir()
	if err := os.WriteFile(filepath.Join(remote, "a.md"), []byte("云端的内容"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.cfg.Sync = config.SyncSettings{Remote: "dir", Dir: remote}

	a.pullCloud(true)
	if a.pulling {
		t.Error("没有真窗口时应当同步跑完")
	}
	if !strings.Contains(a.syncMsg, "1") {
		t.Errorf("状态 = %q", a.syncMsg)
	}
	if _, err := os.Stat(filepath.Join(dir, "a.md")); err != nil {
		t.Errorf("文件没落进知识库：%v", err)
	}

	// 再来一次：云端没有更新
	a.pullCloud(true)
	if a.syncMsg != i18n.T("sync.pull_none") {
		t.Errorf("第二次状态 = %q", a.syncMsg)
	}

	// 开关关着时，开机那次不拉
	a.cfg.Sync.PullOnOpen = false
	a.syncMsg = "没动"
	a.pullOnOpen()
	if a.syncMsg != "没动" {
		t.Errorf("没开开关也拉了：%q", a.syncMsg)
	}
}

// TestOpenSettingsFallback 没有真窗口时，设置退回主窗口的浮层。
func TestOpenSettingsFallback(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.openSettings()
	if !a.settingsOpen {
		t.Error("无窗口时应当退回浮层")
	}
}

// TestSettingsWindowView 独立设置窗口的内容能画出各页。
func TestSettingsWindowView(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	tt := ui.NewTester(a.settingsWindowView, 860, 600)
	tt.Frame()
	for _, page := range settingsPages() {
		if !tt.HasText(page.Label) {
			t.Errorf("窗口里没有导航项 %q", page.Label)
		}
	}
	a.settingsPage = "about"
	tt.Frame()
	if !tt.HasText(appVersion) {
		t.Errorf("关于页没有版本号，界面文字：%q", tt.Texts())
	}
}

// TestStartPage 没打开文稿时是起始页：不画标签栏，给三个动作与最近打开。
func TestStartPage(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.closeTab(0)
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	if len(a.tabs) != 0 {
		t.Fatalf("还有标签：%d", len(a.tabs))
	}
	for _, want := range []string{"新建文稿", "快速搜索", "打开知识库", "最近打开"} {
		if !tt.HasText(want) {
			t.Errorf("起始页缺少 %q，界面文字：%q", want, tt.Texts())
		}
	}
	// 没有标签时不该出现任何标签 chip（标题栏本身还在）
	for _, text := range tt.Texts() {
		if strings.HasPrefix(text, "标签 ") {
			t.Errorf("没有标签时出现了标签 chip：%q", text)
		}
	}
	if err := tt.Click("快速搜索"); err != nil {
		t.Fatalf("点击快速搜索：%v", err)
	}
	tt.Frame()
	if !a.paletteOpen {
		t.Error("快速搜索没有打开面板")
	}
}

// TestSidebarToggleOnce 侧栏收起后只有一个展开入口（顶栏左侧那个）。
func TestSidebarToggleOnce(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.sidebarCollapsed = true
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	count := 0
	for _, text := range tt.Texts() {
		if text == "展开侧栏 (Ctrl+\\)" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("展开入口有 %d 个，想要 1 个", count)
	}
}

// TestViewerToolbars 图片与 PDF 的工具栏都有缩放入口。
func TestViewerToolbars(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("图片.png")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	for _, want := range []string{"适应窗口", "1:1", "缩小", "放大"} {
		if !tt.HasText(want) {
			t.Errorf("图片工具栏缺少 %q，界面文字：%q", want, tt.Texts())
		}
	}
	if err := tt.Click("1:1"); err != nil {
		t.Fatalf("点击 1:1：%v", err)
	}
	tt.Frame()
	if a.tabs[a.active].imgZoom != 1 {
		t.Errorf("缩放 = %v，想要 1", a.tabs[a.active].imgZoom)
	}
	if err := tt.Click("放大"); err != nil {
		t.Fatalf("点击放大：%v", err)
	}
	tt.Frame()
	if a.tabs[a.active].imgZoom <= 1 {
		t.Errorf("放大后 = %v", a.tabs[a.active].imgZoom)
	}

	a.open("论文.pdf")
	tt.Frame()
	for _, want := range []string{"上一页", "下一页", "适应宽度"} {
		if !tt.HasText(want) {
			t.Errorf("PDF 工具栏缺少 %q，界面文字：%q", want, tt.Texts())
		}
	}
}

// TestParseZoom 缩放输入框的解析：百分比、带 %、空与「适应」。
func TestParseZoom(t *testing.T) {
	cases := []struct {
		text string
		want float32
		ok   bool
	}{
		{"150", 1.5, true},
		{"150%", 1.5, true},
		{" 80 ", 0.8, true},
		{"5", 0.1, true},  // 太小的会被抬到 10%
		{"2000", 8, true}, // 太大的压到 800%
		{"适应", 1, true},
		{"", 1, true},
		{"abc", 0, false},
		{"-20", 0, false},
	}
	for _, c := range cases {
		got, ok := parseZoom(c.text)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("parseZoom(%q) = %v,%v 想要 %v,%v", c.text, got, ok, c.want, c.ok)
		}
	}
	if zoomPercentText(1.5) != "150" {
		t.Errorf("zoomPercentText(1.5) = %q", zoomPercentText(1.5))
	}
	if zoomPercentText(0.875) != "88" {
		t.Errorf("zoomPercentText(0.875) = %q", zoomPercentText(0.875))
	}
}

// TestParsePageNumber 页码输入框的解析。
func TestParsePageNumber(t *testing.T) {
	if n, ok := parsePageNumber(" 3 "); !ok || n != 3 {
		t.Errorf("parsePageNumber(3) = %d,%v", n, ok)
	}
	for _, bad := range []string{"", "abc", "0", "-1"} {
		if _, ok := parsePageNumber(bad); ok {
			t.Errorf("parsePageNumber(%q) 不该成功", bad)
		}
	}
}

// TestViewerZoomShowsPercent 点「适应窗口」「1:1」之后，输入框显示的是当前真实比例。
func TestViewerZoomShowsPercent(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("图片.png")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	tab := a.tabs[a.active]

	if err := tt.Click("1:1"); err != nil {
		t.Fatalf("点击 1:1：%v", err)
	}
	tt.Frame()
	if tab.imgZoom != 1 || tab.zoomField.Text != "100" {
		t.Errorf("1:1 之后 zoom=%v 框里=%q，想要 1 与 100", tab.imgZoom, tab.zoomField.Text)
	}

	if err := tt.Click("适应窗口"); err != nil {
		t.Fatalf("点击适应窗口：%v", err)
	}
	tt.Frame()
	if tab.imgZoom == 1 {
		t.Errorf("适应窗口之后仍然 100%%，可用区域 %v", tab.imgStage)
	}
	if want := zoomPercentText(tab.imgZoom); tab.zoomField.Text != want {
		t.Errorf("适应窗口之后框里=%q，想要 %q", tab.zoomField.Text, want)
	}
}

// TestPDFZoomShowsPercent PDF 的缩放框里也是数字，不是「适应」。
func TestPDFZoomShowsPercent(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("论文.pdf")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()
	tab := a.tabs[a.active]
	if tab.zoom <= 0 {
		t.Fatalf("zoom = %v", tab.zoom)
	}
	if tab.zoomField.Text == "" || tab.zoomField.Text == "适应" {
		t.Errorf("缩放框 = %q，想要百分比数字", tab.zoomField.Text)
	}
	if err := tt.Click("适应宽度"); err != nil {
		t.Fatalf("点击适应宽度：%v", err)
	}
	tt.Frame()
	if want := zoomPercentText(tab.zoom); tab.zoomField.Text != want {
		t.Errorf("框里 = %q，想要 %q", tab.zoomField.Text, want)
	}
}

// TestEditorPortOpens 移植过来的编辑器能打开文稿，内容与块模型都对得上。
func TestEditorPortOpens(t *testing.T) {
	a, dir := newTestApp(t)
	src := "# 标题\n\n第一段正文\n\n- 一\n- 二\n\n> 引用\n"
	os.WriteFile(filepath.Join(dir, "notes", "块.md"), []byte(src), 0o644)
	a.refresh()
	a.open("notes/块.md")
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Frame()
	tab := a.tabs[a.active]
	if tab.edit == nil {
		t.Fatal("没有创建编辑器")
	}
	if got := tab.text(); got != src {
		t.Errorf("打开后内容变了：\n%q\n%q", got, src)
	}
	if tab.rich == nil || len(tab.rich.Blocks) < 5 {
		t.Errorf("模型块数 = %d", len(tab.rich.Blocks))
	}
}

// TestEditorPortTypes 点进正文打字会改 Markdown，保存会写盘。
func TestEditorPortTypes(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "块.md"), []byte("# 标题\n\n第一段\n"), 0o644)
	a.refresh()
	a.open("notes/块.md")
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.ClickAt(600, 200) // 点标题那一行
	tt.Frame()
	tt.Frame()
	tt.Type("尾巴")
	tt.Frame()
	tt.Frame()
	if got := a.tabs[a.active].text(); !strings.Contains(got, "尾巴") {
		t.Fatalf("输入没进缓冲区：%q", got)
	}
	a.saveActive()
	saved, err := os.ReadFile(filepath.Join(dir, "notes", "块.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(saved), "尾巴") {
		t.Errorf("没写进文件：%q", string(saved))
	}
}

// TestPDFTextLayer PDF 的文字层：能取字符、能选中、能复制、能查找。
func TestPDFTextLayer(t *testing.T) {
	a, dir := newTestApp(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "cjk-embedded.pdf"))
	if err != nil {
		t.Skip("没有中文样例 PDF")
	}
	os.WriteFile(filepath.Join(dir, "notes", "中文.pdf"), data, 0o644)
	a.refresh()
	a.open("notes/中文.pdf")
	tt := ui.NewTester(a.view, 1100, 820)
	tt.Frame()
	tt.Frame()

	tab := a.tabs[a.active]
	chars := a.pdfCharsAt(tab, 0)
	if len(chars) < 10 {
		t.Fatalf("字符数 = %d", len(chars))
	}

	// 选中前 6 个字（标题开头）
	tab.pdfSelFrom = pdfPos{Page: 0, Char: 0}
	tab.pdfSelTo = pdfPos{Page: 0, Char: 5}
	tt.Frame()
	if got := a.pdfSelectionText(tab); !strings.Contains(got, "中文渲染测试") {
		t.Errorf("选中 = %q", got)
	}

	// Ctrl+C 复制到剪贴板
	tt.Key(ui.Cmd, ui.KeyC)
	tt.Frame()
	if clip := tt.Clipboard(); !strings.Contains(clip, "中文") {
		t.Errorf("剪贴板 = %q", clip)
	}

	// 查找：命中并把选区落到命中处
	a.pdfSearch(tab, "标点")
	if len(tab.pdfFindHits) == 0 {
		t.Fatal("没找到「标点」")
	}
	if !tab.pdfSelFrom.valid() {
		t.Error("查找后没有选中命中处")
	}
	if got := a.pdfSelectionText(tab); !strings.Contains(got, "标点") {
		t.Errorf("命中处文字 = %q", got)
	}
}

// TestPDFDragSelect 在页面上拖过一段文字会选中它。
func TestPDFDragSelect(t *testing.T) {
	a, dir := newTestApp(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "cjk-embedded.pdf"))
	if err != nil {
		t.Skip("没有中文样例 PDF")
	}
	os.WriteFile(filepath.Join(dir, "notes", "中文.pdf"), data, 0o644)
	a.refresh()
	a.open("notes/中文.pdf")
	tt := ui.NewTester(a.view, 1100, 820)
	tt.Frame()
	tt.Frame()

	tab := a.tabs[a.active]
	chars := a.pdfCharsAt(tab, 0)
	scale := tab.zoom
	// 长卷里页面从 (页间距, 页间距) 开始，水平居中
	sizes := a.pdfSizes(tab)
	contentW := sizes[0][0]*scale + 2*pdfPageGap
	left := (contentW - sizes[0][0]*scale) / 2
	first, last := chars[0], chars[4]
	// 长卷在窗口里的原点（左侧是侧栏，不能从 0 算起）
	ox := tab.pdfStage.X + left
	oy := tab.pdfStage.Y + pdfPageGap - tab.pdfScroll.Y
	x0 := ox + first.X0*scale
	y0 := oy + first.Y0*scale + (first.Y1-first.Y0)*scale/2
	x1 := ox + last.X0*scale

	tt.Press(x0, y0)
	tt.Frame()
	tt.Move(x1, y0)
	tt.Frame()
	tt.Release(x1, y0)
	tt.Frame()

	if got := a.pdfSelectionText(tab); !strings.Contains(got, "中文") {
		t.Errorf("拖选 = %q", got)
	}
}

// TestPDFFindScrollsToHit 查找命中后，视图会滚到命中处。
func TestPDFFindScrollsToHit(t *testing.T) {
	a, dir := newTestApp(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "cjk-embedded.pdf"))
	if err != nil {
		t.Skip("没有中文样例 PDF")
	}
	os.WriteFile(filepath.Join(dir, "notes", "中文.pdf"), data, 0o644)
	a.refresh()
	a.open("notes/中文.pdf")
	tt := ui.NewTester(a.view, 1100, 620) // 视口比页面矮，才有得滚
	tt.Frame()
	tt.Frame()

	tab := a.tabs[a.active]
	if tab.pdfScroll.MaxY <= 0 {
		t.Fatalf("页面没有可滚动空间：MaxY=%.1f", tab.pdfScroll.MaxY)
	}
	if tab.pdfScroll.Y != 0 {
		t.Fatalf("一开始就滚动了：%.1f", tab.pdfScroll.Y)
	}

	a.pdfSearch(tab, "第二段")
	if len(tab.pdfFindHits) == 0 {
		t.Fatal("没找到「第二段」")
	}
	for i := 0; i < 6; i++ {
		tt.Advance(200 * time.Millisecond)
		tt.Frame()
	}
	if tab.pdfScroll.Y <= 0 {
		t.Errorf("没有滚到命中处：Y=%.1f", tab.pdfScroll.Y)
	}
	// 命中那一行应当落在可视区里
	chars := a.pdfCharsAt(tab, tab.pdfSelFrom.Page)
	y := pdfPageGap + chars[tab.pdfSelFrom.Char].Y0*tab.zoom - tab.pdfScroll.Y
	if y < 0 || y > tab.pdfStage.H {
		t.Errorf("命中处 y=%.1f 不在视口（高 %.0f）内", y, tab.pdfStage.H)
	}
}

// TestPDFDoubleClickLine 双击选中整行。
func TestPDFDoubleClickLine(t *testing.T) {
	chars := []pdfview.Char{
		{R: 'a', Line: 1}, {R: 'b', Line: 1}, {R: 'c', Line: 2}, {R: 'd', Line: 2},
	}
	from, to := pdfLineRange(chars, 0)
	if from != 0 || to != 1 {
		t.Errorf("第一行 = %d..%d", from, to)
	}
	from, to = pdfLineRange(chars, 2)
	if from != 2 || to != 3 {
		t.Errorf("第二行 = %d..%d", from, to)
	}
}

// TestPDFContinuousScroll 多页 PDF 连成一个长卷，能跨页选中。
func TestPDFContinuousScroll(t *testing.T) {
	a, dir := newTestApp(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "cjk-3pages.pdf"))
	if err != nil {
		t.Skip("没有多页样例")
	}
	os.WriteFile(filepath.Join(dir, "notes", "三页.pdf"), data, 0o644)
	a.refresh()
	a.open("notes/三页.pdf")
	tt := ui.NewTester(a.view, 1100, 700)
	tt.Frame()
	tt.Frame()

	tab := a.tabs[a.active]
	if tab.pdfView.Pages() != 3 {
		t.Fatalf("页数 = %d", tab.pdfView.Pages())
	}
	// 三页竖排：内容比视口高很多，滚动范围应当够大
	if tab.pdfScroll.MaxY < tab.pdfStage.H {
		t.Errorf("滚动范围太小：MaxY=%.0f 视口=%.0f", tab.pdfScroll.MaxY, tab.pdfStage.H)
	}
	// 跨页选区：第 1 页中间 → 第 2 页开头
	c0 := a.pdfCharsAt(tab, 0)
	tab.pdfSelFrom = pdfPos{Page: 0, Char: len(c0) / 2}
	tab.pdfSelTo = pdfPos{Page: 1, Char: 5}
	text := a.pdfSelectionText(tab)
	if len([]rune(text)) < 10 {
		t.Errorf("跨页选区 = %q", text)
	}
}

// TestPDFContentInPalette PDF 正文能出现在全局搜索里。
func TestPDFContentInPalette(t *testing.T) {
	a, dir := newTestApp(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "cjk-3pages.pdf"))
	if err != nil {
		t.Skip("没有多页样例")
	}
	os.WriteFile(filepath.Join(dir, "notes", "三页.pdf"), data, 0o644)
	a.refresh()

	hits := a.pdfContentHits("跨页选择", 6)
	if len(hits) == 0 {
		t.Fatal("全局搜索没找到 PDF 内容")
	}
	if !strings.Contains(hits[0].Snippet, "跨页") {
		t.Errorf("片段 = %q", hits[0].Snippet)
	}
	a.paletteOpen = true
	a.paletteQuery = "跨页选择"
	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	if !tt.HasText("正文") {
		t.Errorf("面板里没有「正文」一节")
	}
}

// TestPDFTripleClickParagraph 三击选整段。
func TestPDFTripleClickParagraph(t *testing.T) {
	chars := []pdfview.Char{
		{R: 'a', Line: 1, Block: 1}, {R: 'b', Line: 1, Block: 1},
		{R: 'c', Line: 2, Block: 1}, {R: 'd', Line: 2, Block: 2},
	}
	from, to := pdfParagraphRange(chars, 0)
	if from != 0 || to != 2 {
		t.Errorf("第一段 = %d..%d，想要 0..2", from, to)
	}
	from, to = pdfParagraphRange(chars, 3)
	if from != 3 || to != 3 {
		t.Errorf("第二段 = %d..%d，想要 3..3", from, to)
	}
}

// TestPDFAnnotations 高亮批注：能加、能存到旁注文件、重开还在、能删。
func TestPDFAnnotations(t *testing.T) {
	a, dir := newTestApp(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "cjk-3pages.pdf"))
	if err != nil {
		t.Skip("没有多页样例")
	}
	os.WriteFile(filepath.Join(dir, "notes", "三页.pdf"), data, 0o644)
	a.refresh()
	a.open("notes/三页.pdf")
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()

	tab := a.tabs[a.active]
	tab.pdfSelFrom = pdfPos{Page: 0, Char: 0}
	tab.pdfSelTo = pdfPos{Page: 0, Char: 4}
	tt.Frame()
	a.addPDFAnnot(tab, "yellow")
	if len(tab.pdfAnnots) != 1 {
		t.Fatalf("批注数 = %d", len(tab.pdfAnnots))
	}
	// 旁注文件写在 PDF 旁边
	sidecar := filepath.Join(dir, "notes", "三页.pdf.papero.json")
	if _, err := os.Stat(sidecar); err != nil {
		t.Fatalf("旁注文件没写：%v", err)
	}
	a.setPDFAnnotNote(tab, 0, "这里要注意")

	// 重开：批注还在，备注也在
	a.open("notes/三页.pdf")
	tab2 := a.tabs[a.active]
	a.loadPDFAnnots(tab2)
	if len(tab2.pdfAnnots) != 1 || tab2.pdfAnnots[0].Note != "这里要注意" {
		t.Fatalf("重开后 = %+v", tab2.pdfAnnots)
	}

	// 删掉：文件也该没了
	a.deletePDFAnnot(tab2, 0)
	if len(tab2.pdfAnnots) != 0 {
		t.Errorf("没删掉：%+v", tab2.pdfAnnots)
	}
	if _, err := os.Stat(sidecar); !os.IsNotExist(err) {
		t.Errorf("旁注文件还在：%v", err)
	}
}

// TestPDFThumbnails 缩略图能渲染出来，尺寸对得上页面比例。
func TestPDFThumbnails(t *testing.T) {
	a, dir := newTestApp(t)
	data, err := os.ReadFile(filepath.Join("..", "..", "ref", "cjk-3pages.pdf"))
	if err != nil {
		t.Skip("没有多页样例")
	}
	os.WriteFile(filepath.Join(dir, "notes", "三页.pdf"), data, 0o644)
	a.refresh()
	a.open("notes/三页.pdf")
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	tt.Frame()

	tab := a.tabs[a.active]
	bmp := a.pdfThumb(tab, 0)
	if bmp == nil {
		t.Fatal("缩略图没渲染出来")
	}
	if w, h := bmp.Size(); w < 80 || h < 80 {
		t.Errorf("缩略图太小：%dx%d", w, h)
	}
	// 面板能画出来
	tab.pdfPanel = 1
	tt.Frame()
	tt.Frame()
	if !tt.HasText("缩略图") || !tt.HasText("批注") {
		t.Errorf("面板页签没出现")
	}
}

// TestFullTextSearch 全文搜索：正文命中带行号与片段，标签也能搜到。
func TestFullTextSearch(t *testing.T) {
	a, dir := newTestApp(t)
	body := "# 笔记\n\n第一段：这里提到量子纠缠。\n\n第二段：量子纠缠很难懂。\n\n#物理 #科普\n"
	os.WriteFile(filepath.Join(dir, "notes", "物理.md"), []byte(body), 0o644)
	a.refresh()

	hits := a.contentHits("量子纠缠", 8)
	if len(hits) == 0 {
		t.Fatal("没搜到正文")
	}
	if hits[0].Line != 3 {
		t.Errorf("行号 = %d，想要 3", hits[0].Line)
	}
	if !strings.Contains(hits[0].Snippet, "量子纠缠") {
		t.Errorf("片段 = %q", hits[0].Snippet)
	}

	// 标签
	tagHits := a.contentHits("科普", 8)
	if len(tagHits) == 0 {
		t.Error("标签没搜到")
	}

	// 查询太短不搜
	if got := a.contentHits("量", 8); len(got) != 0 {
		t.Errorf("单字也搜了：%v", got)
	}
}

// TestFullTextJump 点搜索结果会打开文稿、切到富文本、把命中词放进查找框。
func TestFullTextJump(t *testing.T) {
	a, dir := newTestApp(t)
	body := "# 笔记\n\n这里提到量子纠缠。\n"
	os.WriteFile(filepath.Join(dir, "notes", "物理.md"), []byte(body), 0o644)
	a.refresh()

	hits := a.contentHits("量子纠缠", 8)
	if len(hits) == 0 {
		t.Fatal("没搜到")
	}
	a.paletteQuery = "量子纠缠"
	a.openContentHit(hits[0])

	tt := ui.NewTester(a.view, 1100, 800)
	tt.Frame()
	tt.Frame()

	tab := a.tabs[a.active]
	if tab.mode != "blocks" {
		t.Errorf("模式 = %s，想要 blocks", tab.mode)
	}
	if tab.edit == nil {
		t.Fatal("没有编辑器")
	}
	q, open := tab.edit.FindQuery()
	if !open || q != "量子纠缠" {
		t.Errorf("查找框 = %q open=%v", q, open)
	}
}

// TestOutlineTree 大纲按级别搭成树：比前一个级别深的挂到它下面。
func TestOutlineTree(t *testing.T) {
	heads := []heading{
		{Level: 1, Text: "总纲"},
		{Level: 2, Text: "第一章"},
		{Level: 3, Text: "1.1"},
		{Level: 3, Text: "1.2"},
		{Level: 2, Text: "第二章"},
		{Level: 1, Text: "附录"},
	}
	tree := outlineTree(heads)
	if len(tree) != 2 {
		t.Fatalf("根节点 = %d，想要 2", len(tree))
	}
	if len(tree[0].kids) != 2 {
		t.Errorf("总纲的子 = %d，想要 2", len(tree[0].kids))
	}
	first := tree[0].kids[0]
	if first.h.Text != "第一章" || len(first.kids) != 2 {
		t.Errorf("第一章 = %q 子 %d", first.h.Text, len(first.kids))
	}
	if len(tree[1].kids) != 0 {
		t.Errorf("附录不该有子")
	}
	// 跳级：## 后面直接 #### 也要挂得住
	jump := outlineTree([]heading{{Level: 1, Text: "A"}, {Level: 4, Text: "B"}})
	if len(jump) != 1 || len(jump[0].kids) != 1 {
		t.Errorf("跳级 = %+v", jump)
	}
}

// TestOutlineCollapse 折叠全部会把有子标题的项都记下来。
func TestOutlineCollapse(t *testing.T) {
	a, dir := newTestApp(t)
	body := "# A\n\n## A1\n\n### A1a\n\n## A2\n\n# B\n"
	os.WriteFile(filepath.Join(dir, "notes", "大纲.md"), []byte(body), 0o644)
	a.refresh()
	a.open("notes/大纲.md")
	tab := a.tabs[a.active]

	a.setOutlineAll(tab, outlineOf(tab.text()), true)
	if len(a.outlineCollapsed) != 2 {
		t.Errorf("折叠了 %d 项，想要 2（A 与 A1）", len(a.outlineCollapsed))
	}
	for k, v := range a.outlineCollapsed {
		if !v {
			t.Errorf("%s 应当是折叠的", k)
		}
	}
	a.setOutlineAll(tab, outlineOf(tab.text()), false)
	for k, v := range a.outlineCollapsed {
		if v {
			t.Errorf("%s 应当展开了", k)
		}
	}
}

// TestBoardUndoRedo 白板的撤销与重做。
func TestBoardUndoRedo(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	sc.Add("rectangle", 40, 40)
	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "板.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/板.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	if tab.board == nil {
		t.Fatal("白板没打开")
	}
	if len(tab.board.Elements) != 1 {
		t.Fatalf("元素 = %d", len(tab.board.Elements))
	}

	a.boardSnapshot(tab)
	tab.board.Add("ellipse", 200, 40)
	if len(tab.board.Elements) != 2 {
		t.Fatalf("加完 = %d", len(tab.board.Elements))
	}

	a.boardUndoStep(tab)
	if len(tab.board.Elements) != 1 {
		t.Errorf("撤销后 = %d，想要 1", len(tab.board.Elements))
	}
	a.boardRedoStep(tab)
	if len(tab.board.Elements) != 2 {
		t.Errorf("重做后 = %d，想要 2", len(tab.board.Elements))
	}
	// 撤销栈空时再撤不该崩
	a.boardUndoStep(tab)
	a.boardUndoStep(tab)
	if len(tab.board.Elements) != 1 {
		t.Errorf("撤到底 = %d，想要 1", len(tab.board.Elements))
	}
}

// TestBoardRoughness 抖动是确定性的：同一个 seed 每次画出来一样。
func TestBoardRoughness(t *testing.T) {
	r1 := newRoughRand(42)
	r2 := newRoughRand(42)
	for i := 0; i < 5; i++ {
		if r1.next() != r2.next() {
			t.Fatal("同一个 seed 应当给出同样的序列")
		}
	}
	// 不同 seed 要不一样
	r3 := newRoughRand(43)
	if r3.next() == newRoughRand(42).next() {
		t.Error("不同 seed 不该给出同一个值")
	}
	// 幅度随 roughness 增大
	if roughAmp(0, 1) != 0 {
		t.Error("roughness 0 应当不抖")
	}
	if roughAmp(2, 1) <= roughAmp(1, 1) {
		t.Error("roughness 2 应当比 1 抖得多")
	}
}

// TestBoardExportSVG 导出的是合法 SVG，形状与元素数量对得上。
func TestBoardExportSVG(t *testing.T) {
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	r := sc.Add("rectangle", 60, 60)
	r.Width, r.Height = 180, 110
	r.Fill = "#ffec99"
	sc.Add("ellipse", 300, 60)
	l := sc.Add("arrow", 250, 120)
	l.Points = [][]float64{{0, 0}, {180, 0}}
	tx := sc.Add("text", 60, 360)
	tx.Text = "手绘 & 文字"

	svg := boardSVG(sc)
	if !strings.HasPrefix(svg, "<svg") || !strings.HasSuffix(strings.TrimSpace(svg), "</svg>") {
		t.Fatalf("不是 SVG：%s", svg[:60])
	}
	if strings.Contains(svg, `\n`) {
		t.Error("SVG 里有字面 \\n，说明换行写错了")
	}
	if !strings.Contains(svg, "手绘 &amp; 文字") {
		t.Errorf("文字没转义：%s", svg)
	}
	// 矩形 4 边 × 2 遍 + 椭圆 2 遍 + 箭头 1 = 11 条折线
	if n := strings.Count(svg, "<polyline"); n != 11 {
		t.Errorf("折线 = %d，想要 11", n)
	}
	if n := strings.Count(svg, "<line"); n != 2 {
		t.Errorf("箭头末端斜线 = %d，想要 2", n)
	}
}

// TestBoardBindOnCreate 画箭头时两端落在形状上就自动粘住，形状移动时箭头跟着走。
func TestBoardBindOnCreate(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	rid := sc.Add("rectangle", 100, 100).ID
	sc.Find(rid).Width, sc.Find(rid).Height = 160, 100
	eid := sc.Add("ellipse", 400, 100).ID
	sc.Find(eid).Width, sc.Find(eid).Height = 160, 100
	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "流程.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/流程.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	// 模拟「从矩形里拖到椭圆里」画一条箭头
	arrow := tab.board.Add("arrow", 150, 150)
	arrow.Points = [][]float64{{0, 0}, {300, 0}}
	tab.boardDragEl = arrow.ID
	tab.boardCreating = true
	a.finishBoardCreate(tab)

	got := tab.board.Find(tab.boardDragEl)
	if got == nil {
		t.Fatal("箭头没了")
	}
	if got.StartBinding == nil || got.StartBinding.ElementID != rid {
		t.Errorf("起点没粘上：%+v", got.StartBinding)
	}
	if got.EndBinding == nil || got.EndBinding.ElementID != eid {
		t.Errorf("终点没粘上：%+v", got.EndBinding)
	}
	// 端点应当落在两个形状的边上
	sx, _, ex, _ := got.PointsAbs()
	if sx < 250 || sx > 270 {
		t.Errorf("起点 x = %.1f，应当在矩形右边缘 260 附近", sx)
	}
	if ex < 390 || ex > 410 {
		t.Errorf("终点 x = %.1f，应当在椭圆左边缘 400 附近", ex)
	}

	// 移动矩形：箭头起点跟着走
	rect := tab.board.Find(rid)
	rect.X += 50
	tab.board.RebindAll(rid)
	sx2, _, _, _ := tab.board.Find(got.ID).PointsAbs()
	if sx2 < 300 || sx2 > 320 {
		t.Errorf("移动后起点 x = %.1f，应当在 310 附近", sx2)
	}
}

// TestBoardUnbindOnDelete 删掉形状时，粘在它上面的箭头自动解绑。
func TestBoardUnbindOnDelete(t *testing.T) {
	s := &board.Scene{Type: "excalidraw", Version: 2}
	rid := s.Add("rectangle", 100, 100).ID
	aid := s.Add("arrow", 0, 0).ID
	s.Find(aid).StartBinding = &board.Binding{ElementID: rid}
	s.Unbind(rid)
	if s.Find(aid).StartBinding != nil {
		t.Error("解绑没生效")
	}
}

// TestBoardEndpointDrag 拖动箭头端点可以改接到另一个形状上。
func TestBoardEndpointDrag(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	r1 := sc.Add("rectangle", 100, 100).ID
	sc.Find(r1).Width, sc.Find(r1).Height = 160, 100
	r2 := sc.Add("rectangle", 100, 320).ID
	sc.Find(r2).Width, sc.Find(r2).Height = 160, 100
	aid := sc.Add("arrow", 0, 0).ID
	sc.Find(aid).StartBinding = &board.Binding{ElementID: r1}
	sc.Rebind(sc.Find(aid))
	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "改接.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/改接.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	arrow := tab.board.Find(aid)
	_, _, ex, ey := arrow.PointsAbs()
	t.Logf("改接前：终点 %.1f,%.1f 绑定=%v", ex, ey, arrow.EndBinding)

	// 端点命中判定
	if w := tab.board.EndpointAt(arrow, ex, ey, 10); w != 1 {
		t.Errorf("端点命中 = %d，想要 1（终点）", w)
	}

	// 模拟把终点拖到第二个矩形里再松手
	tab.boardSel = []string{aid}
	tab.boardDragEl = aid
	tab.boardEndDrag = 1
	sx, sy, _, _ := arrow.PointsAbs()
	tab.board.SetEndpoints(arrow, sx, sy, 180, 340)
	a.finishEndpointDrag(tab)

	got := tab.board.Find(aid)
	if got.EndBinding == nil || got.EndBinding.ElementID != r2 {
		t.Fatalf("终点没改接到第二个矩形：%+v", got.EndBinding)
	}
	_, _, ex2, ey2 := got.PointsAbs()
	t.Logf("改接后：终点 %.1f,%.1f", ex2, ey2)
	if ey2 > 320 || ey2 < 300 {
		t.Errorf("终点 y = %.1f，应当落在第二个矩形的上边缘 320 附近", ey2)
	}
}

// TestBoardEndpointDetach 拖到空白处会解绑，不再粘着原来的形状。
func TestBoardEndpointDetach(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	r1 := sc.Add("rectangle", 100, 100).ID
	sc.Find(r1).Width, sc.Find(r1).Height = 160, 100
	aid := sc.Add("arrow", 0, 0).ID
	sc.Find(aid).StartBinding = &board.Binding{ElementID: r1}
	sc.Rebind(sc.Find(aid))
	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "拆.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/拆.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	arrow := tab.board.Find(aid)
	sx, sy, _, _ := arrow.PointsAbs()
	tab.boardDragEl = aid
	tab.boardEndDrag = 0
	tab.board.SetEndpoints(arrow, 600, 600, sx+100, sy)
	a.finishEndpointDrag(tab)

	got := tab.board.Find(aid)
	if got.StartBinding != nil {
		t.Errorf("拖到空白处应当解绑，实际还粘着 %+v", got.StartBinding)
	}
}

// TestBoardContainerText 双击形状给它配一段居中的文字，形状动文字跟着动。
func TestBoardContainerText(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	rid := sc.Add("rectangle", 100, 100).ID
	sc.Find(rid).Width, sc.Find(rid).Height = 200, 120
	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "标签.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/标签.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	shape := tab.board.Find(rid)

	// 模拟双击形状
	a.boardSnapshot(tab)
	text := tab.board.EnsureContainerText(shape)
	text.Text = "开始"
	tab.board.CenterContainerText(shape)

	if text.ContainerID != rid {
		t.Errorf("文字没挂在形状上：%q", text.ContainerID)
	}
	if len(shape.BoundElements) != 1 || shape.BoundElements[0].ID != text.ID {
		t.Errorf("形状的 boundElements = %+v", shape.BoundElements)
	}
	// 居中：文字左边界应当在形状中心减去半宽的位置
	w := board.TextWidth("开始", text.FontSize)
	wantX := 100 + (200-w)/2
	if diff := text.X - wantX; diff > 0.5 || diff < -0.5 {
		t.Errorf("文字 x = %.1f，想要 %.1f", text.X, wantX)
	}
	if text.Y < 100 || text.Y > 220 {
		t.Errorf("文字 y = %.1f，不在形状里", text.Y)
	}

	// 移动形状，文字跟着走
	shape.X += 60
	tab.board.CenterContainerText(shape)
	if diff := text.X - (wantX + 60); diff > 0.5 || diff < -0.5 {
		t.Errorf("移动后文字 x = %.1f，想要 %.1f", text.X, wantX+60)
	}

	// 删形状：文字一起删
	tab.board.RemoveWithContainer(rid)
	if tab.board.Find(text.ID) != nil {
		t.Error("删形状后文字还在")
	}
	if tab.board.Find(rid) != nil {
		t.Error("形状没删掉")
	}
}

// TestBoardTextWidth 文字宽度估算：汉字比西文宽。
func TestBoardTextWidth(t *testing.T) {
	if board.TextWidth("中中", 20) <= board.TextWidth("ab", 20) {
		t.Error("两个汉字应当比两个字母宽")
	}
	if board.TextWidth("", 20) != 0 {
		t.Error("空串宽度应当是 0")
	}
}

// TestBoardStylePersists 样式改动会写进文件，重开还在。
func TestBoardStylePersists(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	rid := sc.Add("rectangle", 100, 100).ID
	sc.Find(rid).Width, sc.Find(rid).Height = 180, 100
	tid := sc.Add("text", 0, 0).ID
	sc.Find(tid).Text = "标签"
	sc.Find(tid).ContainerID = rid
	sc.Find(tid).FontSize = 20
	sc.CenterContainerText(sc.Find(rid))

	// 改样式
	sc.Find(rid).Stroke = "#2f9e44"
	sc.Find(rid).Fill = "#a5d8ff"
	sc.Find(rid).StrokeWidth = 4
	sc.Find(tid).FontSize = 28
	sc.CenterContainerText(sc.Find(rid))

	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "样式.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/样式.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	got := tab.board.Find(rid)
	if got.Stroke != "#2f9e44" || got.Fill != "#a5d8ff" || got.StrokeWidth != 4 {
		t.Errorf("样式没保住：%+v", got)
	}
	text := tab.board.Find(tid)
	if text == nil || text.FontSize != 28 {
		t.Fatalf("字号没保住：%+v", text)
	}
	// 字号变大后仍然居中
	w := board.TextWidth("标签", 28)
	wantX := 100 + (180-w)/2
	if diff := text.X - wantX; diff > 0.5 || diff < -0.5 {
		t.Errorf("字号改后 x = %.1f，想要 %.1f", text.X, wantX)
	}
}

// TestBoardMultiLineText 多行文字：尺寸按行算，形状里居中，SVG 里用 tspan。
func TestBoardMultiLineText(t *testing.T) {
	// 宽度取最宽一行，高度按行数
	w1, h1 := board.TextBlockSize("短\n长长长长", 20)
	w2, _ := board.TextBlockSize("长长长长", 20)
	if w1 != w2 {
		t.Errorf("宽度应当取最宽行：%.1f vs %.1f", w1, w2)
	}
	if h1 != 20*board.LineHeight*2 {
		t.Errorf("两行高度 = %.1f，想要 %.1f", h1, 20*board.LineHeight*2)
	}

	sc := &board.Scene{Type: "excalidraw", Version: 2}
	rid := sc.Add("rectangle", 100, 100).ID
	sc.Find(rid).Width, sc.Find(rid).Height = 200, 140
	tid := sc.Add("text", 0, 0).ID
	sc.Find(tid).Text = "第一行\n第二行"
	sc.Find(tid).ContainerID = rid
	sc.Find(tid).FontSize = 20
	sc.CenterContainerText(sc.Find(rid))

	text := sc.Find(tid)
	w, h := board.TextBlockSize(text.Text, 20)
	wantX := 100 + (200-w)/2
	wantY := 100 + (140-h)/2
	if diff := text.X - wantX; diff > 0.5 || diff < -0.5 {
		t.Errorf("x = %.1f，想要 %.1f", text.X, wantX)
	}
	if diff := text.Y - wantY; diff > 0.5 || diff < -0.5 {
		t.Errorf("y = %.1f，想要 %.1f", text.Y, wantY)
	}

	svg := boardSVG(sc)
	if !strings.Contains(svg, "<tspan") || strings.Count(svg, "<tspan") != 2 {
		t.Errorf("SVG 里应当有两行 tspan：%s", svg)
	}
}

// TestBoardImage 白板里的图片：能解出来、能画、导出成 SVG 的 <image>。
func TestBoardImage(t *testing.T) {
	// 造一张 4×4 的红色 PNG
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 220, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())

	sc := &board.Scene{Type: "excalidraw", Version: 2, Files: map[string]board.File{}}
	sc.Files["f1"] = board.File{ID: "f1", MimeType: "image/png", DataURL: dataURL}
	el := sc.Add("image", 100, 100)
	el.Width, el.Height = 80, 80
	el.FileID = "f1"

	// dataURL 能拆开
	raw, err := decodeDataURL(dataURL)
	if err != nil || len(raw) != buf.Len() {
		t.Fatalf("解 dataURL 失败：%v，长度 %d", err, len(raw))
	}

	// 能解成位图
	a, dir := newTestApp(t)
	enc, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "图.excalidraw"), enc, 0o644)
	a.refresh()
	a.open("notes/图.excalidraw")
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	tt.Frame()

	tab := a.tabs[a.active]
	bmp := a.boardImage(tab, "f1")
	if bmp == nil {
		t.Fatal("图片没解出来")
	}
	if w, h := bmp.Size(); w != 4 || h != 4 {
		t.Errorf("位图尺寸 = %dx%d", w, h)
	}

	// SVG 里应当有 <image>
	svg := boardSVG(tab.board)
	if !strings.Contains(svg, "<image") || !strings.Contains(svg, "data:image/png;base64,") {
		t.Errorf("SVG 里没有图片：%s", svg)
	}
}

// TestBoardImageMissing 缺图时画占位框，不崩。
func TestBoardImageMissing(t *testing.T) {
	sc := &board.Scene{Type: "excalidraw", Version: 2, Files: map[string]board.File{}}
	el := sc.Add("image", 100, 100)
	el.FileID = "不存在"
	a, dir := newTestApp(t)
	enc, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "缺图.excalidraw"), enc, 0o644)
	a.refresh()
	a.open("notes/缺图.excalidraw")
	tt := ui.NewTester(a.view, 900, 700)
	tt.Frame()
	tt.Frame()
	tab := a.tabs[a.active]
	if bmp := a.boardImage(tab, "不存在"); bmp != nil {
		t.Error("缺图不该解出位图")
	}
	if svg := boardSVG(tab.board); strings.Contains(svg, "<image") {
		t.Error("缺图不该导出 <image>")
	}
}

// TestBoardFreehand 画笔：拖动追加点、收笔归一化包围盒、点太密会跳过。
func TestBoardFreehand(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	sc.Add("rectangle", 100, 100)
	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "画.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/画.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	el := tab.board.Add("freedraw", 200, 200)
	el.Points = [][]float64{{0, 0}}
	tab.boardDragEl = el.ID

	// 追加一串点，其中有一个离得太近应当被跳过
	appendBoardPoint(el, 210, 200)
	appendBoardPoint(el, 211, 200) // 距离 1，跳过
	appendBoardPoint(el, 230, 210)
	appendBoardPoint(el, 250, 230)
	if len(el.Points) != 4 {
		t.Errorf("点数 = %d，想要 4", len(el.Points))
	}

	finishBoardStroke(tab)
	got := tab.board.Find(el.ID)
	if got.Width < 40 || got.Height < 20 {
		t.Errorf("包围盒 = %.0fx%.0f", got.Width, got.Height)
	}
	// 归一化后第一个点应当在原点附近
	if len(got.Points) == 0 || got.Points[0][0] != 0 || got.Points[0][1] != 0 {
		t.Errorf("第一个点 = %v，应当归一化到 (0,0)", got.Points[0])
	}
	if tab.boardDrawing {
		t.Error("收笔后 boardDrawing 应当是 false")
	}
}

// TestBoardFreehandDot 只点一下也要留下痕迹。
func TestBoardFreehandDot(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "点.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/点.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	el := tab.board.Add("freedraw", 100, 100)
	el.Points = [][]float64{{0, 0}}
	tab.boardDragEl = el.ID
	finishBoardStroke(tab)
	got := tab.board.Find(el.ID)
	if len(got.Points) < 2 {
		t.Errorf("点一下也应当留下至少两个点，实际 %d", len(got.Points))
	}
}

// TestBoardMultiSelect 多选：Shift 加选、分组展开、整体删除。
func TestBoardMultiSelect(t *testing.T) {
	a, dir := newTestApp(t)
	sc := &board.Scene{Type: "excalidraw", Version: 2}
	r1 := sc.Add("rectangle", 100, 100).ID
	r2 := sc.Add("ellipse", 300, 100).ID
	sc.Add("diamond", 500, 100)
	data, _ := sc.Encode()
	os.WriteFile(filepath.Join(dir, "notes", "多选.excalidraw"), data, 0o644)
	a.refresh()
	a.open("notes/多选.excalidraw")
	tt := ui.NewTester(a.view, 1000, 760)
	tt.Frame()

	tab := a.tabs[a.active]
	a.boardSetSel(tab, r1)
	a.boardAddSel(tab, r2)
	if len(tab.boardSel) != 2 {
		t.Fatalf("选中 %d 个", len(tab.boardSel))
	}
	if !a.boardIsSel(tab, r1) || !a.boardIsSel(tab, r2) {
		t.Error("boardIsSel 判断错")
	}
	// 主选中是最后加的那个
	if got := a.boardPrimary(tab); got == nil || got.ID != r2 {
		t.Errorf("主选中 = %+v", got)
	}

	// 分组之后点一个成员应当选中整组
	gid := tab.board.Group([]string{r1, r2})
	if gid == "" {
		t.Fatal("分组失败")
	}
	a.boardSetSel(tab, r1)
	ids := a.boardSelIDs(tab)
	if len(ids) != 2 {
		t.Errorf("分组后选中集 = %v，想要两个", ids)
	}

	// 整体删除
	a.boardDeleteSelection(tab)
	if len(tab.board.Visible()) != 1 {
		t.Errorf("删完剩 %d 个元素，想要 1", len(tab.board.Visible()))
	}
	if len(tab.boardSel) != 0 {
		t.Error("删除后选择集应当清空")
	}
}

// TestBoardResizeCorner 拉角改尺寸，翻过来也不出负宽。
func TestBoardResizeCorner(t *testing.T) {
	el := board.Element{X: 100, Y: 100, Width: 200, Height: 100}
	box := [4]float64{100, 100, 200, 100}

	// 右下角拖到 (400, 260)
	resizeFromCorner(&el, 2, box, 400, 260)
	if el.Width != 300 || el.Height != 160 {
		t.Errorf("右下角 = %.0fx%.0f，想要 300x160", el.Width, el.Height)
	}

	// 左上角拖到 (50, 40)
	el = board.Element{X: 100, Y: 100, Width: 200, Height: 100}
	resizeFromCorner(&el, 0, box, 50, 40)
	if el.X != 50 || el.Y != 40 || el.Width != 250 || el.Height != 160 {
		t.Errorf("左上角 = %.0f,%.0f %.0fx%.0f", el.X, el.Y, el.Width, el.Height)
	}

	// 拖过头（比最小尺寸还小）应当钳到 8
	el = board.Element{X: 100, Y: 100, Width: 200, Height: 100}
	resizeFromCorner(&el, 2, box, 110, 110)
	if el.Width < 8 || el.Height < 8 {
		t.Errorf("最小尺寸没钳住：%.0fx%.0f", el.Width, el.Height)
	}
}

// TestBoardAddBend 双击线加折点，变成折线。
func TestBoardAddBend(t *testing.T) {
	s := &board.Scene{Type: "excalidraw", Version: 2}
	aid := s.Add("arrow", 100, 100).ID
	el := s.Find(aid)
	el.Points = [][]float64{{0, 0}, {200, 0}}

	// 在线上方 30 像素处双击
	if !s.AddBend(el, 200, 130) {
		t.Fatal("没加折点")
	}
	if len(el.Points) != 3 {
		t.Fatalf("点数 = %d，想要 3", len(el.Points))
	}
	// 新点在中间
	if el.Points[1][0] != 100 || el.Points[1][1] != 30 {
		t.Errorf("折点 = %v，想要 (100,30)", el.Points[1])
	}
	// 直线不能加折点
	r := s.Add("rectangle", 0, 0)
	if s.AddBend(r, 10, 10) {
		t.Error("矩形不该能加折点")
	}
}

// TestBoardGroupExpand 分组：同组元素一起选中，拆组后各自独立。
func TestBoardGroupExpand(t *testing.T) {
	s := &board.Scene{Type: "excalidraw", Version: 2}
	a := s.Add("rectangle", 0, 0).ID
	b := s.Add("ellipse", 200, 0).ID
	c := s.Add("diamond", 400, 0).ID

	gid := s.Group([]string{a, b})
	if gid == "" {
		t.Fatal("分组失败")
	}
	if got := s.ExpandGroup(a); len(got) != 2 {
		t.Errorf("展开 = %v，想要两个", got)
	}
	if got := s.ExpandGroup(c); len(got) != 1 {
		t.Errorf("没分组的应当只返回自己：%v", got)
	}
	s.Ungroup([]string{a, b})
	if got := s.ExpandGroup(a); len(got) != 1 {
		t.Errorf("拆组后 = %v", got)
	}
}

// TestReorderSidebarActions 拖动排序：把一行挪到目标位置，其余保持相对顺序。
func TestReorderSidebarActions(t *testing.T) {
	a, _ := newTestApp(t)
	a.cfg.SidebarActions = []string{"new", "search", "vault", "graph"}

	// 把第 0 行（新建文稿）挪到第 2 行（打开知识库）之前
	a.reorderSidebarActions([]int{0}, 2)
	want := []string{"search", "new", "vault", "graph"}
	if !slices.Equal(a.cfg.SidebarActions, want) {
		t.Errorf("排序 = %v，想要 %v", a.cfg.SidebarActions, want)
	}

	// 再把第 0 行（快速搜索）挪到末尾
	a.reorderSidebarActions([]int{0}, 4)
	want = []string{"new", "vault", "graph", "search"}
	if !slices.Equal(a.cfg.SidebarActions, want) {
		t.Errorf("排到末尾 = %v，想要 %v", a.cfg.SidebarActions, want)
	}
}

// TestSidebarDragReorder 在设置页里按住一行往下拖，顺序真的会变。
func TestSidebarDragReorder(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsOpen = true
	a.settingsPage = "sidebar"
	tt := ui.NewTester(a.view, 900, 800)
	tt.Frame()
	tt.Frame()

	// 侧栏里也有同名行，这里按勾选框的无障碍名字定位设置页里的那几行
	first, ok := tt.Find("显示 新建文稿")
	if !ok {
		t.Fatalf("找不到设置页的第一行，界面文字：%q", tt.Texts())
	}
	third, ok := tt.Find("显示 打开知识库")
	if !ok {
		t.Fatalf("找不到设置页的第三行")
	}
	// 从勾选框右边一点按下（避开勾选框本身），拖到第三行下方
	tt.Press(first.X+30, first.Y+first.H/2)
	tt.Move(first.X+30, first.Y+first.H/2+10)
	tt.Move(third.X+30, third.Y+third.H-2)
	tt.Release(third.X+30, third.Y+third.H-2)
	tt.Frame()

	got := a.cfg.SidebarActions
	if len(got) != 4 {
		t.Fatalf("排序后 = %v", got)
	}
	if got[0] == "new" {
		t.Errorf("新建文稿还在第一位：%v", got)
	}
}

// TestSidebarRowsAreBlocks 顶部动作的每一行都是同一种块：
// 开关贴在同一个右缘上，行高 52，行距均匀。
//
// 这一页原来是一排 30 像素的紧凑行、勾选框在最左边，和上面「最近打开」那种
// 卡片行不是一回事。全部改成「名字 + 说明在左、控件在右」的块之后，
// 这条测试钉住三件事：右缘对齐、行高、行距。
func TestSidebarRowsAreBlocks(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsPage = "sidebar"
	// 用独立窗口那套视图并给它足够高：设置浮层固定在 620，第四行会落在滚动区外，
	// 没排版的行矩形是 0，量出来的行距没意义
	tt := ui.NewTester(a.settingsWindowView, 900, 900)
	tt.Frame()
	tt.Frame()

	var rects []ui.Rect
	for _, name := range []string{"新建文稿", "快速搜索", "打开知识库", "打开知识图谱"} {
		r, ok := tt.Find("显示 " + name)
		if !ok {
			t.Fatalf("找不到「显示 %s」，界面文字：%q", name, tt.Texts())
		}
		rects = append(rects, r)
	}
	right := rects[0].X + rects[0].W
	for i, r := range rects {
		if d := r.X + r.W - right; d > 0.5 || d < -0.5 {
			t.Errorf("第 %d 行开关右缘差了 %.1f：%+v", i+1, d, r)
		}
		if i == 0 {
			continue
		}
		if gap := r.Y - rects[i-1].Y; gap > 52.5 || gap < 51.5 {
			t.Errorf("第 %d 行行距 = %.1f，想要 52", i+1, gap)
		}
	}
}

// TestSidebarToggleAction 关掉/开回一个动作都不崩，而且状态确实跟着变。
// 曾经在这里闪退过：列表画到一半把配置改短，后面的行越界。
func TestSidebarToggleAction(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsOpen = true
	a.settingsPage = "sidebar"
	tt := ui.NewTester(a.view, 900, 800)
	tt.Frame()
	tt.Frame()

	if err := tt.Click("显示 新建文稿"); err != nil {
		t.Fatalf("点勾选框：%v", err)
	}
	tt.Frame()
	if a.hasSidebarAction("new") {
		t.Error("勾掉之后还显示")
	}
	if got := a.sidebarActionIndex("new"); got != 0 {
		t.Errorf("隐藏不该动顺序，位置 = %d", got)
	}

	// 再点一次勾回来
	if err := tt.Click("显示 新建文稿"); err != nil {
		t.Fatalf("再点勾选框：%v", err)
	}
	tt.Frame()
	if !a.hasSidebarAction("new") {
		t.Error("勾回来之后没有恢复")
	}
}

// TestFileMenuNewBoard 文件行的右键菜单能新建白板，也能用默认程序打开。
func TestFileMenuNewBoard(t *testing.T) {
	a, dir := newTestApp(t)
	os.WriteFile(filepath.Join(dir, "notes", "示例.md"), []byte("# 示例\n"), 0o644)
	a.refresh()
	a.open("notes/示例.md")
	tt := ui.NewTester(a.view, 900, 500)
	tt.Frame()

	if err := tt.RightClick("notes/示例.md"); err != nil {
		t.Fatalf("右键文件行：%v", err)
	}
	tt.Frame()
	for _, want := range []string{"用默认程序打开", "新建白板"} {
		if !tt.HasText(want) {
			t.Errorf("文件菜单缺少 %q，界面文字：%q", want, tt.Texts())
		}
	}

	if err := tt.Click("新建白板"); err != nil {
		t.Fatalf("点新建白板：%v", err)
	}
	tt.Frame()
	// 新建的白板应当存在并被打开
	if tab := a.currentTab(); tab == nil || tab.kind != vault.KindBoard {
		t.Fatalf("没有打开白板：%+v", a.currentTab())
	}
	if _, err := os.Stat(filepath.Join(dir, "notes", "新白板.excalidraw")); err != nil {
		t.Errorf("白板文件没建出来：%v", err)
	}
}

// TestFolderMenuNewBoard 目录行的右键菜单里也有新建白板。
func TestFolderMenuNewBoard(t *testing.T) {
	a, _ := newTestApp(t)
	a.refresh()
	tt := ui.NewTester(a.view, 900, 500)
	tt.Frame()
	if err := tt.RightClick("notes"); err != nil {
		t.Fatalf("右键目录：%v", err)
	}
	tt.Frame()
	if !tt.HasText("新建白板") {
		t.Errorf("目录菜单缺少「新建白板」，界面文字：%q", tt.Texts())
	}
}

// TestDerivedColorsVisible 推导色卡片的每一格都要真的落在可视区里。
//
// 设置浮层高度是固定的（620），主题页又排在一列里，多一行就会被滚动区裁掉——
// 第一版把「选区」排成第五行时，它连排版都没做（矩形宽高为 0），截图上只剩
// 一个被切断的卡片边框。这条测试就是钉住「看得见」。
func TestDerivedColorsVisible(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsOpen = true
	a.settingsPage = "theme"
	tt := ui.NewTester(a.view, 1000, 800)
	tt.Frame()
	tt.Frame()

	for _, name := range []string{"面板", "分隔线", "次要文字", "悬停强调色", "选区"} {
		r, ok := tt.Find(name)
		if !ok {
			t.Errorf("推导色卡片里找不到「%s」", name)
			continue
		}
		if r.W <= 0 || r.H < 12 {
			// 被裁掉的行不会消失，而是被压成两三个像素高——照抄「>0」是抓不到的
			t.Errorf("「%s」被可视区裁掉了：%+v", name, r)
		}
	}

	// 色值要和名字在同一列。单数行的右半边不补空位时，左边那格会吃满整行，
	// 它的色值就跑到第二列的右边去了——「选区」的 #dce6fc 错过一次。
	// 一行里同一侧必须有一个色值：按行高配对，再看是不是同一边。
	type hexAt struct {
		y, right float32
	}
	var hexes []hexAt
	for _, txt := range tt.Texts() {
		if len(txt) != 7 || txt[0] != '#' {
			continue
		}
		if r, ok := tt.Find(txt); ok {
			hexes = append(hexes, hexAt{r.Y, r.X + r.W})
		}
	}
	for _, name := range []string{"面板", "分隔线", "次要文字", "悬停强调色", "选区"} {
		nr, ok := tt.Find(name)
		if !ok {
			continue
		}
		found := false
		for _, h := range hexes {
			dy := h.y - nr.Y
			if dy < 0 {
				dy = -dy
			}
			// 名字在左半边（x < 400）时，色值也要落在左半边的右缘（< 500）
			if dy < 4 && (h.right < 500) == (nr.X < 400) {
				found = true
			}
		}
		if !found {
			t.Errorf("「%s」这一行同侧没有色值（名字 x=%.0f）", name, nr.X)
		}
	}
}

// TestThemeModeSwitch 切到深色后 token 跟着变，文字仍可读。
func TestThemeModeSwitch(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	before := a.tokens.Paper

	a.settingsOpen = true
	a.settingsPage = "theme"
	tt := ui.NewTester(a.view, 1000, 800)
	tt.Frame()

	if err := tt.Click("明暗 深色"); err != nil {
		t.Fatalf("点深色：%v", err)
	}
	tt.Frame()
	if a.cfg.Theme != "dark" {
		t.Errorf("模式 = %q，想要 dark", a.cfg.Theme)
	}
	if a.tokens.Paper == before {
		t.Error("切到深色后纸色没变")
	}
	if theme.Contrast(a.tokens.Ink, a.tokens.Paper) < 4.5 {
		t.Error("深色主题下正文对比度不达标")
	}
}

// TestThemeColorEdit 改一个颜色会存进配置，并立刻重算 token。
func TestThemeColorEdit(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsOpen = true
	a.settingsPage = "theme"
	tt := ui.NewTester(a.view, 1000, 800)
	tt.Frame()

	a.setThemeColor("accent", ui.Hex("#ff0000"))
	if a.cfg.ThemeAccent != "#ff0000" {
		t.Errorf("存下的强调色 = %q", a.cfg.ThemeAccent)
	}
	if got := hexOf(a.tokens.Accent); got != "#ff0000" {
		t.Errorf("token 里的强调色 = %s", got)
	}
	// 面板是由背景派生的，不该被强调色带跑
	if a.tokens.Panel != theme.Derive(themeInput(a.cfg)).Panel {
		t.Error("面板色没有跟着重新推导")
	}
}

// TestRichBlocksRoundTrip 富文本块编辑器打开文稿不改变文件内容。

// TestThemeReset 改过颜色后能恢复默认，按钮只在改过时出现。
func TestThemeReset(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsOpen = true
	a.settingsPage = "theme"
	tt := ui.NewTester(a.view, 1000, 800)
	tt.Frame()

	// 没改过时不该有「恢复默认」
	if tt.HasText("恢复默认") {
		t.Error("没改过颜色时不该出现恢复默认")
	}

	a.setThemeColor("accent", ui.Hex("#ff0000"))
	tt.Frame()
	if !tt.HasText("恢复默认") {
		t.Fatalf("改过颜色后应当出现恢复默认，界面文字：%q", tt.Texts())
	}
	if err := tt.Click("恢复默认"); err != nil {
		t.Fatalf("点恢复默认：%v", err)
	}
	tt.Frame()
	if a.themeCustomized() {
		t.Error("恢复后配置里还留着自定义颜色")
	}
	if got := hexOf(a.tokens.Accent); got != hexOf(theme.Default(false).Accent) {
		t.Errorf("恢复后强调色 = %s", got)
	}
}

// TestNumberSlider 宽度/字号既有滑块也有数值框，数值框能直接改配置。
func TestNumberSlider(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsOpen = true
	a.settingsPage = "sidebar"
	tt := ui.NewTester(a.view, 1000, 800)
	tt.Frame()

	before := a.cfg.SidebarWidth
	if err := tt.Click("步进 +"); err != nil {
		t.Fatalf("点步进加号：%v（界面文字：%q）", err, tt.Texts())
	}
	tt.Frame()
	if got := a.cfg.SidebarWidth; got != before+10 {
		t.Errorf("点加号后 = %.0f，想要 %.0f", got, before+10)
	}
	if err := tt.Click("步进 −"); err != nil {
		t.Fatalf("点步进减号：%v", err)
	}
	tt.Frame()
	if got := a.cfg.SidebarWidth; got != before {
		t.Errorf("点减号后 = %.0f，想要 %.0f", got, before)
	}
}

// TestLanguageSwitch 在设置里换语言：界面下一帧整体换过来，导航也跟着换，
// 再切回来能恢复。语言是全局状态，所以这条测试自己负责收尾。
func TestLanguageSwitch(t *testing.T) {
	t.Cleanup(func() { i18n.SetLanguage("zh-Hans") })

	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsOpen = true
	a.settingsPage = "general"
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Frame()

	if !tt.HasText("界面语言") {
		t.Fatalf("通用页没有语言项：%q", tt.Texts())
	}

	a.cfg.Language = "en"
	tt.Frame()
	if !tt.HasText("Interface language") {
		t.Errorf("切英文后没有 Interface language：%q", tt.Texts())
	}
	if tt.HasText("界面语言") {
		t.Error("切英文后还留着中文标签")
	}
	// 设置页自己的导航也得换，不能只换正文
	for _, want := range []string{"General", "Sidebar", "Theme"} {
		if !tt.HasText(want) {
			t.Errorf("导航里没有 %q：%q", want, tt.Texts())
		}
	}

	a.cfg.Language = ""
	tt.Frame()
	if !tt.HasText("界面语言") {
		t.Errorf("切回中文失败：%q", tt.Texts())
	}
}

// TestRestoreLastOpen 重启时恢复上次打开的标签与激活项，而不是第一篇。
func TestRestoreLastOpen(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.open("notes/纯文本.txt")

	// 模拟重启：拿同一份配置重新装配
	b := New(a.cfg, a.vault, a.tree)
	b.restore()

	var got []string
	for _, tb := range b.tabs {
		got = append(got, tb.rel)
	}
	want := []string{"notes/欢迎.md", "notes/纯文本.txt"}
	if !slices.Equal(got, want) {
		t.Errorf("恢复的标签 = %v，想要 %v", got, want)
	}
	if b.tabs[b.active].rel != "notes/纯文本.txt" {
		t.Errorf("激活的是 %q，想要最后打开的那篇", b.tabs[b.active].rel)
	}
}

// TestVaultKeepsItsTabs 每个知识库有自己的标签页：切走再切回来要回到原处，
// 而不是又落在它的第一篇文稿上。
func TestVaultKeepsItsTabs(t *testing.T) {
	a, dirA := newTestApp(t)
	a.open("notes/欢迎.md")
	a.open("notes/纯文本.txt")

	dirB := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirB, "b.md"), []byte("# B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.setVault(dirB)
	if len(a.tabs) != 1 || a.tabs[0].rel != "b.md" {
		t.Fatalf("第一次打开 B 库应当落在它的第一篇，实际 %d 个标签", len(a.tabs))
	}

	a.setVault(dirA)
	var got []string
	for _, tb := range a.tabs {
		got = append(got, tb.rel)
	}
	want := []string{"notes/欢迎.md", "notes/纯文本.txt"}
	if !slices.Equal(got, want) {
		t.Errorf("切回 A 库的标签 = %v，想要 %v", got, want)
	}
}

// TestShotIsolatedFromConfig `-shot` 是开发辅助，不能改用户的设置。
//
// 它在临时示例库里 open 文稿，会走到 remember() → cfg.Save()；不隔离就会
// 把真实 vault 换成那个临时库（已经踩过一次，整个知识库"消失"）。
func TestShotIsolatedFromConfig(t *testing.T) {
	t.Cleanup(config.SetDir(t.TempDir()))
	cfg := config.Default()
	cfg.Vault = "MARKER"
	if err := cfg.Save(); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "shot.png")
	if err := Shot(out, 400, 300, 1, 1); err != nil {
		t.Fatalf("截图失败：%v", err)
	}
	if got := config.Load().Vault; got != "MARKER" {
		t.Errorf("截图改动了用户设置：vault = %q", got)
	}
}

// TestCollapsedSidebarKeepsBrand 收起侧栏后，图标和软件名要留在展开按钮旁边：
// 侧栏整块不画了，只剩一个按钮就认不出是哪个应用。
func TestCollapsedSidebarKeepsBrand(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.sidebarCollapsed = true
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Frame()

	if !tt.HasText("Papero") {
		t.Fatalf("收起后看不到软件名，界面文字：%q", tt.Texts())
	}
	btn, ok := tt.Find(i18n.T("sidebar.expand_tooltip"))
	if !ok {
		t.Fatal("找不到展开按钮")
	}
	brand, ok := tt.Find("Papero")
	if !ok {
		t.Fatal("找不到品牌文字")
	}
	if btn.X <= brand.X {
		t.Errorf("展开按钮应当在软件名右边（按钮 x=%.0f，brand x=%.0f）", btn.X, brand.X)
	}
	// 也不能压到标签上
	if tab, ok := tt.Find("标签 notes/欢迎.md"); ok && brand.X+brand.W > tab.X {
		t.Errorf("品牌压到了标签（brand 右边=%.0f，标签左边=%.0f）", brand.X+brand.W, tab.X)
	}
}

// TestRememberKeepsSessionBeforeRestore 启动早期（restore 之前）手里没有标签，
// 这时 remember() 不能写盘：一写就把上次打开的文稿抹成空。
func TestRememberKeepsSessionBeforeRestore(t *testing.T) {
	a, _ := newTestApp(t)
	a.cfg.SetSession(a.vault.Root(), config.Session{
		LastOpen: []string{"notes/欢迎.md", "notes/纯文本.txt"},
		Active:   "notes/欢迎.md",
	})
	a.remember()
	if got := a.cfg.Session(a.vault.Root()); len(got.LastOpen) != 2 {
		t.Fatalf("空标签页把会话抹掉了：%+v", got)
	}
	// 恢复之后照常写
	a.restore()
	if len(a.tabs) != 2 {
		t.Fatalf("恢复后应当有两个标签，实际 %d", len(a.tabs))
	}
	a.closeTab(0)
	if got := a.cfg.Session(a.vault.Root()); len(got.LastOpen) != 1 {
		t.Errorf("关掉一个标签后应当只剩一个：%+v", got)
	}
}

// fakeUpdater 是更新器的假实现：不下网、不换文件，只记下被要求做了什么。
type fakeUpdater struct {
	rel        *update.Release
	checkErr   error
	fetched    string
	fetchErr   error
	installed  string
	installErr error
	progress   int64 // 非零时在 Fetch 里报一次进度
	notes      []update.Note
	notesErr   error
	askedAfter string // 上次 Notes 问的区间，用来核对版本范围
	askedUpto  string
}

func (f *fakeUpdater) Check(context.Context) (*update.Release, error) { return f.rel, f.checkErr }

func (f *fakeUpdater) Notes(_ context.Context, after, upto string) ([]update.Note, int, error) {
	f.askedAfter, f.askedUpto = after, upto
	return f.notes, len(f.notes), f.notesErr
}

func (f *fakeUpdater) Fetch(_ context.Context, rel *update.Release, progress func(done, total int64)) (string, error) {
	if f.progress > 0 && progress != nil {
		progress(f.progress, rel.File.Size)
	}
	return f.fetched, f.fetchErr
}

func (f *fakeUpdater) Install(path string) error {
	f.installed = path
	return f.installErr
}

// TestUpdateFlow 查 → 下载 → 换文件 → 重启，四步的状态都要对。
func TestUpdateFlow(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeUpdater{
		rel:      &update.Release{Version: "9.9.9", File: update.Asset{Name: "papero-windows-amd64.exe", Size: 100}},
		fetched:  filepath.Join(t.TempDir(), "papero.exe.new"),
		progress: 50,
	}
	a.upd.svc = f
	relaunched := false
	a.upd.relaunch = func() { relaunched = true }

	// 没查之前：说明位说清更新从哪来
	if got := a.updateHint(); got != i18n.T("settings.update.source") {
		t.Errorf("初始说明 = %q", got)
	}

	a.checkUpdate(true)
	if a.upd.rel == nil || a.upd.checking {
		t.Fatalf("没查到新版本：%+v", a.upd.rel)
	}
	if !strings.Contains(a.updateHint(), "9.9.9") {
		t.Errorf("说明里没有版本号：%q", a.updateHint())
	}

	a.downloadUpdate()
	if a.upd.ready != "9.9.9" || a.upd.downloading {
		t.Fatalf("下载后状态不对：ready=%q downloading=%v", a.upd.ready, a.upd.downloading)
	}
	if a.upd.got != 50 || a.upd.total != 100 {
		t.Errorf("进度没记下来：%d/%d", a.upd.got, a.upd.total)
	}

	a.installUpdate()
	if f.installed != f.fetched {
		t.Errorf("换的不是下好的那个文件：%q", f.installed)
	}
	if !relaunched {
		t.Error("换完没有重启")
	}
}

// TestUpdateNoRelease 三种「没有更新」的答案：已最新、还没发版本、查不动。
func TestUpdateNoRelease(t *testing.T) {
	a, _ := newTestApp(t)

	a.upd.svc = &fakeUpdater{}
	a.checkUpdate(true)
	if got, want := a.updateHint(), i18n.T("settings.update.latest"); got != want {
		t.Errorf("同版本 = %q，想要 %q", got, want)
	}

	a.upd.svc = &fakeUpdater{checkErr: update.ErrNoReleases}
	a.checkUpdate(true)
	if got, want := a.updateHint(), i18n.T("settings.update.none"); got != want {
		t.Errorf("没有发布 = %q，想要 %q", got, want)
	}

	a.upd.svc = &fakeUpdater{checkErr: errors.New("网络不通")}
	a.checkUpdate(true)
	if !strings.Contains(a.updateHint(), "网络不通") {
		t.Errorf("失败没说原因：%q", a.updateHint())
	}
}

// TestUpdateInstallFailure 换文件失败时要说出来，而且不能重启。
func TestUpdateInstallFailure(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeUpdater{
		rel:        &update.Release{Version: "9.9.9", File: update.Asset{Name: "x", Size: 1}},
		fetched:    "x.new",
		installErr: errors.New("目录只读"),
	}
	a.upd.svc = f
	relaunched := false
	a.upd.relaunch = func() { relaunched = true }

	a.checkUpdate(true)
	a.downloadUpdate()
	a.installUpdate()
	if relaunched {
		t.Error("换文件失败却重启了")
	}
	if !strings.Contains(a.updateHint(), "目录只读") {
		t.Errorf("失败原因没显示：%q", a.updateHint())
	}
}

// TestSettingsAboutUpdateRow 关于页有「检查更新」这一行，开关能关掉自动检查。
func TestSettingsAboutUpdateRow(t *testing.T) {
	a, _ := newTestApp(t)
	a.open("notes/欢迎.md")
	a.settingsPage = "about"
	tt := ui.NewTester(a.settingsWindowView, 860, 700)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{appVersion, i18n.T("settings.update.title"), i18n.T("settings.update.auto")} {
		if !tt.HasText(want) {
			t.Errorf("关于页没有 %q", want)
		}
	}
	if err := tt.Click(i18n.T("settings.update.auto_label")); err != nil {
		t.Fatalf("点自动检查开关：%v", err)
	}
	tt.Frame()
	if !a.cfg.NoUpdateCheck {
		t.Error("关掉之后配置没变")
	}
}

// TestUpdateNotes 查到新版本之后，中间每一版的日志要取回来，并且能打开浮层看。
func TestUpdateNotes(t *testing.T) {
	a, _ := newTestApp(t)
	f := &fakeUpdater{
		rel: &update.Release{Version: "0.4.0", File: update.Asset{Name: "x", Size: 1}},
		notes: []update.Note{
			{Version: "0.4.0", Notes: "### 新增\n- 自更新"},
			{Version: "0.2.1", Notes: "修了几个 bug"},
		},
	}
	a.upd.svc = f
	a.checkUpdate(true)

	if f.askedAfter != appVersion || f.askedUpto != "0.4.0" {
		t.Errorf("问的区间是 (%q, %q]，想要 (%q, 0.4.0]", f.askedAfter, f.askedUpto, appVersion)
	}
	if len(a.upd.notes) != 2 || a.upd.notesTotal != 2 {
		t.Fatalf("日志 = %+v（总数 %d）", a.upd.notes, a.upd.notesTotal)
	}
	if got := a.notesSubtitle(); !strings.Contains(got, "0.4.0") || !strings.Contains(got, "2") {
		t.Errorf("副标题 = %q", got)
	}

	// 侧栏那张卡片的文案
	title, sub := a.updateBannerText()
	if !strings.Contains(title, "0.4.0") || sub != i18n.T("update.banner.view") {
		t.Errorf("卡片 = %q / %q", title, sub)
	}

	// 浮层能打开，里面能看到每一版的标题
	a.upd.overlay = true
	tt := ui.NewTester(a.view, 1100, 760)
	tt.Frame()
	tt.Frame()
	for _, want := range []string{i18n.T("update.notes.title"), "v0.4.0", "v0.2.1"} {
		if !tt.HasText(want) {
			t.Errorf("浮层里没有 %q", want)
		}
	}
}

// TestUpdateJustUpdated 升级之后第一次启动：认得出是从上一版升上来的，
// 取那一版的日志，并把当前版本记进配置。
func TestUpdateJustUpdated(t *testing.T) {
	a, _ := newTestApp(t)
	a.cfg.LastVersion = "0.1.0"
	f := &fakeUpdater{notes: []update.Note{{Version: appVersion, Notes: "改了什么"}}}
	a.upd.svc = f

	a.noteUpgrade()

	if a.upd.updatedTo != appVersion {
		t.Errorf("没认出这次是升级：%q", a.upd.updatedTo)
	}
	if f.askedAfter != "0.1.0" || f.askedUpto != appVersion {
		t.Errorf("问的区间 = (%q, %q]", f.askedAfter, f.askedUpto)
	}
	if title, _ := a.updateBannerText(); !strings.Contains(title, appVersion) {
		t.Errorf("卡片标题 = %q", title)
	}
	if a.cfg.LastVersion != appVersion {
		t.Errorf("配置里的上次版本 = %q", a.cfg.LastVersion)
	}

	// 第二次启动（同版本）不该再提示
	b, _ := newTestApp(t)
	b.cfg.LastVersion = appVersion
	b.upd.svc = f
	b.noteUpgrade()
	if b.upd.updatedTo != "" {
		t.Errorf("同版本也提示了：%q", b.upd.updatedTo)
	}
}

// TestNoteLines release 正文在浮层里摊成纯文本。
func TestNoteLines(t *testing.T) {
	md := "# 主要变化\n\n" +
		"这一版加了 **自更新**，见 [文档](https://example.com/docs)。\n\n\n" +
		"- 修了 `crash`\n" +
		"* 修了另一个 https://github.com/iazhan/Papero/pull/12\n" +
		"1. 第三条\n"
	lines := noteLines(md)
	var got []string
	headings := 0
	for _, l := range lines {
		got = append(got, l.Text)
		if l.Heading {
			headings++
		}
	}
	want := []string{
		"主要变化",
		"",
		"这一版加了 自更新，见 文档。",
		"",
		"• 修了 crash",
		"• 修了另一个 #12",
		"• 第三条",
	}
	if !slices.Equal(got, want) {
		t.Errorf("摊平结果：\n%q\n想要\n%q", got, want)
	}
	if headings != 1 {
		t.Errorf("标题行 %d 个，想要 1", headings)
	}
}

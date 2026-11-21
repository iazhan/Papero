// Package app 装配 Papero 的原生窗口：侧栏目录树、标签页、各类文稿的查看与编辑。
// 界面完全由 MyGo 的 ui 包在 GPU 上绘制，没有 WebView，也不加载网页。
package app

import (
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"github.com/oneo-me/onevra/source/components/graph"

	rte "papero/internal/editor"

	"papero/internal/board"
	"papero/internal/config"
	"papero/internal/docx"
	"papero/internal/i18n"
	"papero/internal/pdfview"
	"papero/internal/plugin"
	"papero/internal/richdoc"
	"papero/internal/theme"
	"papero/internal/vault"

	"papero/internal/wiki"
)

// autosaveDelay 是停止输入后写盘的时间。
const autosaveDelay = 800 * time.Millisecond

// tab 是一个打开着的文稿。Markdown 与文本用代码编辑器编辑，
// 其余类型由各自的查看器绘制。
type tab struct {
	rel  string
	kind vault.Kind
	// 文本类文稿（Markdown、纯文本）的缓冲区与编辑器状态。
	buf   *editor.Buffer
	state *editor.State
	// md 是 Markdown 预览的渲染状态，跨帧保留排版结果。
	md markdown.Options
	// mode 是 Markdown 的显示方式：split、preview、source。
	mode string
	// version 与 lastEdit 用于自动保存。
	version  uint64
	lastEdit time.Time

	// 图片查看器。
	img *ui.Bitmap
	// size 是文稿的字节数，查看器的工具栏上要显示。
	size     int64
	imgErr   string
	imgZoom  float32 // 当前倍率，永远是真实值（按下「适应窗口」时就写进来）
	imgStage ui.Rect // 图片可视区域的实测大小，供「适应窗口」计算
	// zoomField / pageField 是查看器工具栏上可输入的比例与页码。
	zoomField numField
	pageField numField
	// pdfStage 是 PDF 页面的可视区域，供「适应宽度」计算。
	pdfStage ui.Rect

	// Word 查看器：docx 转成的 Markdown。
	docxMD  string
	docxErr string

	// liveOn 表示实时排版当前是否生效（关掉时要清一次）。
	liveOn bool
	// 富文本块编辑器的状态：富模型、生成它时的源文本、每块的编辑器与聚焦项。
	rich *richdoc.Document
	// edit 是富文本编辑器（ArcMD 的 internal/editor），整篇一个缓冲区。
	edit    *rte.Editor
	editRev uint64
	// imageCache 缓存移植编辑器里画过的图片。
	imageCache map[string]rte.ImageResource

	// 双链自动补全的状态：是否正在补全、上一次的前缀（用来避免每帧重置）。
	wikiCompletion    bool
	wikiCompletionKey string

	// PDF 查看器（PDFium 渲染成位图）。
	pdfView *pdfview.Doc
	pdfErr  string
	// pdfCache 缓存渲染过的页（键：页码 + DPI）。
	pdfCache map[string]*ui.Bitmap
	// 文字层：按页缓存的字符、文档级选区、查找结果。
	pdfCharCache map[int][]pdfview.Char
	pdfSizeCache [][2]float32
	pdfSelFrom   pdfPos
	pdfSelTo     pdfPos
	pdfDragging  bool
	// 连击检测（双击选词、三击选段）。
	pdfClickAt    time.Time
	pdfClickPos   ui.Point
	pdfClickCount int
	pdfFind       string
	pdfFindHits   []pdfHit
	pdfFindAt     int
	pdfFindOpen   bool
	pdfFindField  numField
	// pendingFind 是打开文稿后要高亮的词（全文搜索跳转用）。
	pendingFind string
	// pdfManual 为假时按窗口宽度自适应（打开面板、改窗口大小都跟着变）。
	pdfManual bool
	// 缩略图与批注。
	pdfPanel        int // 0 关，1 缩略图，2 批注
	pdfThumbs       map[int]*ui.Bitmap
	pdfAnnots       []pdfAnnot
	pdfAnnotsLoaded bool
	pdfAnnotSel     int
	pdfAnnotNote    string
	pdfAnnotColor   string
	// pdfScroll 记住长卷滚到哪儿；跳转时用它。
	pdfScroll ui.ScrollState
	// pdfScrollToSel 置位后，下一帧把视图滚到当前选区。
	pdfScrollToSel bool
	// pdfScrollToPage 置位后，下一帧滚到这一页（-1 表示不动）。
	pdfScrollToPage int
	page            int
	zoom            float32 // 0 表示适应宽度

	// 白板（内置插件 dev.papero.board）。
	board *board.Scene
	// 白板的撤销栈：每步存一份元素快照。
	boardUndo [][]board.Element
	boardRedo [][]board.Element
	// boardTextFocus 表示刚双击了文字元素，要把工具栏的输入框聚焦。
	boardTextFocus bool
	boardErr       string
	boardDirty     bool
	boardSel       []string
	boardTool      string
	boardZoom      float32
	boardPanX      float32
	boardPanY      float32

	// 白板交互的临时状态。
	boardDragging bool
	boardCreating bool
	// boardResize 是正在拉的那个角（-1 表示没在拉），boardResizeBox 是拉之前的包围盒。
	boardResize    int
	boardResizeBox [4]float64
	// boardDragOrig 记下拖动开始时各元素的位置，多选整体平移用。
	boardDragOrig map[string][2]float64
	// boardDrawing 表示正在用画笔画线。
	boardDrawing bool
	// boardImages 缓存白板里解出来的图片（键：fileId）。
	boardImages map[string]*ui.Bitmap
	// boardEndDrag 记录正在拖的是箭头的哪一端：0 起点、1 终点、-1 没在拖。
	boardEndDrag    int
	boardCreateX    float64
	boardCreateY    float64
	boardPanning    bool
	boardDragEl     string
	boardDragStartX float64
	boardDragStartY float64
	boardDragOffX   float64
	boardDragOffY   float64
}

// currentTab 返回当前标签，没有则返回 nil。
func (a *App) currentTab() *tab {
	if a.active < 0 || a.active >= len(a.tabs) {
		return nil
	}
	return a.tabs[a.active]
}

// dirty 报告文本类文稿是否有未保存的改动。
func (t *tab) dirty() bool {
	return t.buf != nil && t.buf.Modified()
}

// text 返回编辑器中的当前文本。
func (t *tab) text() string {
	if t.buf == nil {
		return ""
	}
	return t.buf.String()
}

// App 是窗口的全部状态。View 只在主线程读写；后台结果经 Window.Update 写回。
type App struct {
	// outlineCollapsed 记住大纲里哪些标题被折叠（键：文稿路径 + 标题路径）。
	outlineCollapsed map[string]bool
	// pdfIndex / textIndex 缓存正文，供全文搜索使用（应用级，跨标签复用）。
	pdfIndex  map[string]*pdfTextIndex
	textIndex map[string]*textIndex
	cfg       *config.Settings
	vault     *vault.Vault
	tree      *vault.Node
	stamp     string

	tabs   []*tab
	active int
	// tabScroll 记住标签栏的横向滚动位置；lastTab 用来发现「切换了标签」。
	tabScroll ui.ScrollState
	lastTab   int
	// tabRects 是上一帧各标签的位置，用来把切换到的标签滚进视野。
	tabRects []ui.Rect
	// tabScrollPending 表示切换了标签，等下一帧拿到位置后再滚。
	tabScrollPending bool

	// tokens 是当前主题派生出的全部语义色。
	tokens   theme.Tokens
	sidebarW float32
	// sidebarList 是设置页里侧栏动作的列表状态（拖动排序用）。
	sidebarList ui.ListState
	// groupRows 数着当前卡片里已经有几行，用来决定下一行要不要画分隔线。
	groupRows int
	// themePick* 是主题页里取色浮层的状态：给哪个颜色取、取到的是什么。
	themePickOpen  bool
	themePickKey   string
	themePickColor ui.Color
	collapsed      map[string]bool
	filter         string
	// sidebarTab 是侧栏视图：0 文档，1 大纲。
	sidebarTab int
	// sidebarCollapsed 表示侧栏已收起。
	sidebarCollapsed bool
	// sidebarP 是侧栏开合的当前进度：0 收起、1 展开，由 view 每帧算出来。
	// 收起后侧栏整块不画，动画状态不能挂在它身上，只能挂在根行，再记到这里。
	sidebarP float32
	// restored 表示启动时「恢复上次标签页」已经跑过。在那之前 remember()
	// 一律不写：一个还没恢复的空窗口要是先存了盘，上次打开的文稿就被抹掉了。
	restored bool

	paletteOpen  bool
	paletteQuery string
	paletteSel   int

	// 链接图（双链解析结果）与图谱视图。
	graph      *wiki.Graph
	graphStamp string
	graphOpen  bool
	// settingsWin 是独立的设置窗口。
	settingsWin *mygo.Window
	// graphWin 是独立图谱窗口，嵌在主窗口时为空。
	graphWin *mygo.Window
	// graphState 保留图谱的布局、镜头与悬停状态。
	graphState graph.State

	// 悬停预览：指针停在链接行上时，侧栏底部显示目标文稿的开头。
	linkHover string

	// 移到回收站前的确认（有反向链接时才弹）。
	trashOpen bool
	trashRel  string

	// 重命名对话框。
	renameOpen bool
	renameRel  string
	renameText string

	// 命令面板（Ctrl+Shift+P）。
	commandOpen  bool
	commandQuery string
	commandSel   int

	settingsOpen bool
	settingsPage string
	// discoverOpen 打开「发现」面板（未来放知识图谱与插件市场）。
	discoverOpen bool
	// recent 是最近打开过的文稿，vaults 是最近用过的知识库。
	recent []string
	vaults []string

	toast   string
	toastAt time.Time

	syncMsg string
	syncing bool
	// probing 是「测试连接」正在进行：和同步一样只允许一件，免得两个请求
	// 抢同一条状态。
	probing bool
	// pulling 是「从云端拉取」正在进行：同样和同步互斥。
	pulling bool

	// upd 是自更新的状态（查版本、下载、换 exe）。
	upd updateState

	// splitPct 是分栏模式下源码所占的百分比。
	splitPct float32
	// plugins 是插件管理器。
	plugins *plugin.Manager

	win *mygo.Window
}

// Run 启动 Papero。
func Run() {
	cfg := config.Load()
	// 先定语言再干活：下面 seed 出来的第一篇文档要用它决定写中文还是英文。
	i18n.SetLanguage(cfg.Language)
	// 上次更新让位出来的旧 exe 只能现在删：更新那一刻进程还开着它。
	cleanupUpdate()
	root := cfg.Vault
	if root == "" {
		root = defaultVault()
		cfg.Vault = root
		seed(root)
	}
	v, err := vault.Open(root)
	if err != nil {
		log.Fatalf(i18n.T("error.vault_open_format"), err)
	}
	tree, err := v.Scan()
	if err != nil {
		log.Fatalf(i18n.T("error.scan_format"), err)
	}

	a := New(cfg, v, tree)
	// 这次启动是不是刚从旧版本升上来的：是就把更新日志先取好
	a.noteUpgrade()

	mygo.App.OnOpenFile(func(p string) {
		// 双击关联文件或“打开方式”启动时，把文件所在目录当作知识库打开。
		a.openExternal(p)
	})

	mygo.App.WhenReady(func() {
		opts := mygo.WindowOptions{
			Title:    "Papero",
			Width:    1180,
			Height:   820,
			MinWidth: 760, MinHeight: 520,
			StateKey: "papero",
			Content:  ui.View(a.view),
			// 隐藏标题栏，让侧栏铺满窗口高度；红绿灯浮在侧栏上方。
			TitleBarStyle: mygo.TitleBarHidden,
			// 和系统画的最小化 / 最大化 / 关闭同高（Windows 的标题栏按钮是 32）
			TitleBarHeight: 32,
		}
		if runtime.GOOS == "darwin" {
			// 红绿灯居中在侧栏顶部那一行里。
			opts.TrafficLightPosition = &mygo.Point{X: 18, Y: 16}
		} else {
			// 其他平台自己画最小化 / 最大化 / 关闭，样式照 Windows 的标题栏按钮。
			opts.HideWindowButtons = true
		}
		a.win = mygo.NewWindow(opts)
		a.win.OnFileDrop(func(e *mygo.FileDropEvent) {
			for _, p := range e.Paths {
				a.openExternal(p)
			}
		})
		a.restore()
		// 多台设备编同一个库时，先拿到别人的改动再动手
		a.pullOnOpen()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

// New 按设置装配应用状态。窗口由 Run 创建；测试里可以只装配状态，
// 用 ui.NewTester 驱动视图。
func New(cfg *config.Settings, v *vault.Vault, tree *vault.Node) *App {
	if cfg == nil {
		cfg = config.Default()
	}
	a := &App{
		cfg:       cfg,
		tokens:    theme.Derive(themeInput(cfg)),
		vault:     v,
		tree:      tree,
		sidebarW:  float32(cfg.SidebarWidth),
		sidebarP:  1,
		collapsed: map[string]bool{},
		splitPct:  50,
		lastTab:   -1,
	}
	if v != nil {
		a.stamp = v.Stamp()
	}
	a.recent = append([]string(nil), cfg.Recent...)
	a.vaults = append([]string(nil), cfg.Vaults...)
	if v != nil {
		a.rememberVault(v.Root())
	}
	if dir, err := config.Dir(); err == nil {
		a.plugins = plugin.NewManager(filepath.Join(dir, "plugins"))
		if _, err := a.plugins.Load(); err != nil {
			log.Printf(i18n.T("error.plugins"), err)
		}
	}
	return a
}

// defaultVault 是首次启动时的知识库位置：文档目录下的 Papero。
func defaultVault() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "Papero")
	}
	docs := filepath.Join(home, "Documents")
	if _, err := os.Stat(docs); err != nil {
		return filepath.Join(home, "Papero")
	}
	return filepath.Join(docs, "Papero")
}

// seed 在新建的知识库里放一篇欢迎文稿，说明 Papero 的用法。
func seed(root string) {
	if err := os.MkdirAll(filepath.Join(root, "notes"), 0o755); err != nil {
		return
	}
	name := filepath.Join(root, "notes", i18n.T("welcome.title")+".md")
	if _, err := os.Stat(name); err == nil {
		return
	}
	os.WriteFile(name, []byte(welcomeText()), 0o644)
}

const welcome = `# 欢迎使用 Papero

Papero 是「纸」的意思（世界语 *papero*）。它是一个以 Markdown 为主、
但不止 Markdown 的个人知识库。

## 它能打开什么

- **Markdown**：默认分栏显示，左边源码、右边排版结果，随时切换
- **图片**：PNG、JPEG、GIF、WebP、BMP、SVG
- **PDF**：原生解析重排，可以选中文字
- **Word**：.docx 直接读成 Markdown 排版
- **纯文本**：代码、日志、CSV 都行

## 文稿就是文件

所有文稿都是这个文件夹里普通的 Markdown 文件，
随时可以和 Obsidian、VS Code、Git 一起用。

- [ ] 在左侧新建一篇文稿
- [ ] 按 Ctrl+K 快速跳转
- [ ] 在设置里接上同步（文件夹 / GitHub / WebDAV）

## 双链

在正文里写 [[另一篇文稿]]，两篇就互相连起来了：
点一下跳过去，侧栏「链接」页里能看到谁链接到这里，
「发现 › 知识图谱」里能看到整库的关系图。
`

const welcomeEN = `# Welcome to Papero

Papero means “paper” (Esperanto *papero*). It's a personal knowledge base built
around Markdown — but not only Markdown.

## What it opens

- **Markdown**: split view by default, source on the left and the page on the right, switch any time
- **Images**: PNG, JPEG, GIF, WebP, BMP, SVG
- **PDF**: own renderer, text is selectable
- **Word**: .docx read straight into Markdown layout
- **Plain text**: code, logs, CSV

## Documents are files

Every document is a plain Markdown file in this folder,
ready to share with Obsidian, VS Code or Git.

- [ ] Create a document from the sidebar
- [ ] Press Ctrl+K to jump anywhere
- [ ] Set up sync in Settings (folder / GitHub / WebDAV)

## Wiki links

Write [[another document]] in the body and the two link up:
click to jump, see who links here under Links in the sidebar,
and the whole vault's relationships under Discover › Knowledge Graph.
`

// welcomeText 是建库时写进去的第一篇文档。内容跟界面语言走——
// 英文用户打开的第一篇不该是中文。
func welcomeText() string {
	if i18n.English() {
		return welcomeEN
	}
	return welcome
}

// restore 恢复这个知识库上次打开的标签页；第一次打开它就落在第一篇文稿上。
func (a *App) restore() {
	a.restored = true
	sess := a.cfg.Session(a.vault.Root())
	opened := 0
	for _, rel := range sess.LastOpen {
		if n := a.tree.Find(rel); n != nil && !n.Dir {
			a.open(rel)
			opened++
		}
	}
	if sess.Active != "" {
		for i, t := range a.tabs {
			if t.rel == sess.Active {
				a.active = i
			}
		}
	}
	if opened == 0 {
		a.openFirstNote()
	}
}

// openFirstNote 打开知识库里的第一篇 Markdown，避免空窗口。
func (a *App) openFirstNote() {
	for _, f := range a.tree.Files() {
		if f.Kind == vault.KindMarkdown {
			a.open(f.Rel)
			return
		}
	}
}

// open 打开一篇文稿；已打开时切换到对应标签。
func (a *App) open(rel string) {
	rel = strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "/")
	for i, t := range a.tabs {
		if t.rel == rel {
			a.active = i
			return
		}
	}
	n := a.tree.Find(rel)
	if n == nil || n.Dir {
		a.say(i18n.T("error.file_not_found") + rel)
		return
	}
	t := &tab{
		rel: rel, kind: n.Kind, mode: a.cfg.Split,
		pdfSelFrom: noPos, pdfSelTo: noPos,
		pdfAnnotSel: -1, pdfScrollToPage: -1,
	}
	if t.mode == "" {
		t.mode = "split"
	}
	a.load(t)
	a.tabs = append(a.tabs, t)
	a.active = len(a.tabs) - 1
	a.remember()
	a.addRecent(rel)
}

// addRecent 把一篇文稿放到「最近打开」的最前面，最多留 8 条。
func (a *App) addRecent(rel string) {
	out := []string{rel}
	for _, r := range a.recent {
		if r != rel && len(out) < 8 {
			out = append(out, r)
		}
	}
	a.recent = out
	a.cfg.Recent = out
	a.cfg.Save()
}

// rememberVault 把知识库放到「工作区间」的最前面，最多留 6 个。
func (a *App) rememberVault(root string) {
	if root == "" {
		return
	}
	// 按归一后的路径比较：同一个库会以 D:\Note\Note 和 D:/Note/Note 两种
	// 写法出现（设置里选目录、双击文件带进来的路径写法不同），按原字符串
	// 比就会在工作区间里列两遍。
	key := config.SessionKey(root)
	out := []string{root}
	for _, v := range a.vaults {
		if config.SessionKey(v) != key && len(out) < 6 {
			out = append(out, v)
		}
	}
	a.vaults = out
	a.cfg.Vaults = out
	a.cfg.Save()
}

// load 按类型读取文稿内容。
func (a *App) load(t *tab) {
	data, err := a.vault.Read(t.rel)
	if err != nil {
		t.imgErr = err.Error()
		return
	}
	dir := path.Dir(t.rel)
	switch t.kind {
	case vault.KindMarkdown, vault.KindText:
		text := stripBOM(string(data))
		t.buf = editor.NewBuffer(text)
		if l := editor.LanguageFor(t.rel); l != nil {
			t.buf.SetLanguage(l)
		}
		t.state = &editor.State{
			Buffer:     t.buf,
			SoftWrap:   true,
			LineHeight: 1.6,
			Font:       ui.Font{Family: fontMono, Size: 13},
		}
		t.version = t.buf.Version()
		if t.kind == vault.KindMarkdown {
			t.md = markdown.Options{
				LoadImage: markdown.FileImages(dir),
				OnTask:    a.taskHandler(t),
			}
		}
	case vault.KindImage:
		t.size = int64(len(data))
		bmp, err := ui.DecodeBitmap(data)
		if err != nil {
			t.imgErr = i18n.T("error.image_decode") + err.Error()
			return
		}
		t.img = bmp
	case vault.KindWord:
		md, err := docx.ToMarkdown(data)
		if err != nil {
			t.docxErr = err.Error()
			return
		}
		t.docxMD = md
	case vault.KindPDF:
		doc, err := pdfview.Default.Open(data)
		if err != nil {
			t.pdfErr = i18n.T("error.pdf_open") + err.Error()
			return
		}
		t.pdfView = doc
	case vault.KindBoard:
		scene, err := board.Parse(data)
		if err != nil {
			t.boardErr = err.Error()
			return
		}
		t.board = scene
		t.boardZoom = 1
		t.boardPanX, t.boardPanY = 40, 40
	}
}

// linkHandler 处理 Markdown 预览里的链接：站内 Markdown 直接打开，
// 锚点滚动到对应标题，其余交给系统浏览器。
func (a *App) linkHandler(c *ui.Context, t *tab) func(string) {
	return func(url string) {
		if target, anchor, ok := parseWikiDest(url); ok {
			a.openWiki(target, anchor, t.rel)
			return
		}
		switch {
		case strings.HasPrefix(url, "#"):
			t.md.Anchor = strings.TrimPrefix(url, "#")
			t.md.AnchorVersion++
		case strings.HasPrefix(url, "http://"), strings.HasPrefix(url, "https://"), strings.HasPrefix(url, "mailto:"):
			c.Services().OpenURL(url)
		default:
			target := path.Clean(path.Join(path.Dir(t.rel), url))
			if n := a.tree.Find(target); n != nil && !n.Dir {
				a.open(target)
			} else if n := a.tree.Find(target + ".md"); n != nil {
				a.open(target + ".md")
			} else {
				a.say(i18n.T("error.link_target") + url)
			}
		}
	}
}

// taskHandler 让预览里的任务框可点，改动写回 Markdown 原文。
func (a *App) taskHandler(t *tab) func(line int, checked bool) {
	return func(line int, checked bool) {
		if t.buf == nil || line < 1 || line > t.buf.LineCount() {
			return
		}
		old := t.buf.Line(line - 1)
		next := toggleTask(old, checked)
		if next == old {
			return
		}
		start := 0
		for i := 0; i < line-1; i++ {
			start += len(t.buf.Line(i)) + 1
		}
		t.buf.Edit(editor.Edit{Start: start, End: start + len(old), Text: next})
		a.save(t)
	}
}

// toggleTask 把一行任务列表的勾选状态改写成 checked。
func toggleTask(line string, checked bool) string {
	lower := strings.ToLower(line)
	idx := strings.Index(lower, "[ ]")
	box := "[ ]"
	if idx < 0 {
		idx = strings.Index(lower, "[x]")
		box = "[x]"
	}
	if idx < 0 {
		return line
	}
	want := "[ ]"
	if checked {
		want = "[x]"
	}
	if box == want {
		return line
	}
	return line[:idx] + want + line[idx+len(box):]
}

// save 把文本类文稿写回磁盘。
func (a *App) save(t *tab) {
	if t == nil || t.buf == nil {
		return
	}
	if !t.buf.Modified() {
		return
	}
	text := t.buf.String()
	if err := a.vault.Write(t.rel, []byte(text)); err != nil {
		a.say(i18n.T("error.save") + err.Error())
		return
	}
	t.buf.MarkSaved()
	t.version = t.buf.Version()
}

// saveActive 保存当前标签。
func (a *App) saveActive() {
	if a.active >= 0 && a.active < len(a.tabs) {
		a.save(a.tabs[a.active])
	}
}

// saveAll 保存所有标签，退出前调用。
func (a *App) saveAll() {
	for _, t := range a.tabs {
		a.save(t)
	}
}

// closeTab 关闭一个标签，未保存的改动先写盘。
func (a *App) closeTab(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.save(a.tabs[i])
	a.tabs = append(a.tabs[:i], a.tabs[i+1:]...)
	if a.active >= len(a.tabs) {
		a.active = len(a.tabs) - 1
	}
	if a.active < 0 {
		a.active = 0
	}
	a.remember()
}

// remember 记住这个知识库打开着的文稿，重启或切回来时恢复。
//
// 按知识库分开记：每个库有自己的标签页，切到别的库再切回来不该回到第一篇。
func (a *App) remember() {
	// 恢复还没跑过、手里又没有标签：这多半是启动早期的某一帧，
	// 存下去等于把上次打开的文稿清空。等 restore() 跑完再记。
	if len(a.tabs) == 0 && !a.restored {
		return
	}
	var sess config.Session
	for _, t := range a.tabs {
		sess.LastOpen = append(sess.LastOpen, t.rel)
	}
	if a.active >= 0 && a.active < len(a.tabs) {
		sess.Active = a.tabs[a.active].rel
	}
	a.cfg.SetSession(a.vault.Root(), sess)
	a.cfg.Save()
}

// refresh 重新扫描知识库。
// commit 存盘并让所有窗口重画。
//
// 设置窗口和主窗口是两个独立的窗口，改配置只会让设置窗口自己重画；
// 主窗口要等下一次输入（比如关掉设置窗口）才重绘，看起来就像
// 「改了深色要关掉设置才生效」。所以配置一改就主动 invalidate。
func (a *App) commit() {
	a.cfg.Save()
	for _, w := range []*mygo.Window{a.win, a.settingsWin, a.graphWin} {
		if w != nil {
			w.Invalidate()
		}
	}
}

func (a *App) refresh() {
	tree, err := a.vault.Scan()
	if err != nil {
		a.say(i18n.T("error.scan") + err.Error())
		return
	}
	a.tree = tree
	a.stamp = a.vault.Stamp()
	if a.graphWin != nil {
		a.graphWin.Invalidate()
	}
}

// newNote 在 dir 下新建一篇 Markdown 文稿并打开。
func (a *App) newNote(dir string) {
	rel := a.vault.UniqueName(dir, i18n.T("new.untitled"), ".md")
	body := "# \n\n"
	if err := a.vault.Create(rel, []byte(body)); err != nil {
		a.say(i18n.T("error.create") + err.Error())
		return
	}
	a.refresh()
	a.open(rel)
}

// newBoard 在 dir 下新建一个白板并打开。
func (a *App) newBoard(dir string) {
	rel := a.vault.UniqueName(dir, i18n.T("new.untitled_board"), ".excalidraw")
	scene := &board.Scene{}
	data, err := scene.Encode()
	if err != nil {
		a.say(i18n.T("error.board_create") + err.Error())
		return
	}
	if err := a.vault.Create(rel, data); err != nil {
		a.say(i18n.T("error.board_create") + err.Error())
		return
	}
	a.refresh()
	a.open(rel)
}

// openExternal 打开知识库外的文件：把它的目录当成知识库。
func (a *App) openExternal(p string) {
	abs, err := filepath.Abs(p)
	if err != nil {
		return
	}
	dir := filepath.Dir(abs)
	if rel, err := filepath.Rel(a.vault.Root(), abs); err == nil && !strings.HasPrefix(rel, "..") {
		a.refresh()
		a.open(filepath.ToSlash(rel))
		return
	}
	a.setVault(dir)
	a.open(filepath.Base(abs))
}

// setVault 切换知识库目录。
func (a *App) setVault(dir string) {
	v, err := vault.Open(dir)
	if err != nil {
		a.say(i18n.T("error.vault_open") + err.Error())
		return
	}
	a.saveAll()
	a.remember() // 先记下旧库停在哪，切回来时能回到原处
	a.vault = v
	a.cfg.Vault = v.Root()
	a.cfg.Save()
	a.tabs = nil
	a.active = 0
	a.collapsed = map[string]bool{}
	a.refresh()
	a.rememberVault(v.Root())
	// 这个库上次停在哪就回到哪；第一次打开它才落在第一篇文稿上
	a.restore()
}

// openWithDefault 用系统的默认程序打开一个文件（不经过 Papero 自己的查看器）。
func (a *App) openWithDefault(rel string) {
	abs, err := a.vault.Abs(rel)
	if err != nil {
		a.say(i18n.T("error.file_not_found_open") + err.Error())
		return
	}
	if err := mygo.Shell.OpenPath(abs); err != nil {
		a.say(i18n.T("error.open_default") + err.Error())
	}
}

// reveal 在系统的文件管理器里显示一个文件。
func (a *App) reveal(rel string) {
	abs, err := a.vault.Abs(rel)
	if err != nil {
		return
	}
	mygo.Shell.ShowItemInFolder(abs)
}

// trash 把文稿移到知识库的回收站（.trash），并关掉它的标签。
// 如果还有别的文稿链接到它，先确认——删掉之后那些链接会变成断链。
func (a *App) trash(rel string) {
	if back := a.backlinksOf(rel); len(back) > 0 {
		a.trashRel = rel
		a.trashOpen = true
		return
	}
	a.doTrash(rel)
}

// doTrash 真正执行删除。
func (a *App) doTrash(rel string) {
	if err := a.vault.Remove(rel); err != nil {
		a.say(i18n.T("error.trash") + err.Error())
		return
	}
	a.closeTabsUnder(rel)
	a.trashOpen = false
	a.refresh()
	a.say(i18n.T("trash.moved") + rel)
}

// backlinksOf 返回链接到某篇文稿的其它文稿。
func (a *App) backlinksOf(rel string) []*wiki.Note {
	g := a.ensureGraph()
	note := g.Notes[rel]
	if note == nil {
		return nil
	}
	out := make([]*wiki.Note, 0, len(note.Back))
	for _, r := range note.Back {
		if n := g.Notes[r]; n != nil {
			out = append(out, n)
		}
	}
	return out
}

// closeTabsUnder 关闭某个文件（或它所在目录）下的所有标签。
func (a *App) closeTabsUnder(rel string) {
	out := a.tabs[:0]
	for _, t := range a.tabs {
		if t.rel == rel || strings.HasPrefix(t.rel, rel+"/") {
			continue
		}
		out = append(out, t)
	}
	a.tabs = out
	if a.active >= len(a.tabs) {
		a.active = len(a.tabs) - 1
	}
	if a.active < 0 {
		a.active = 0
	}
	a.remember()
}

// closeOthers 关闭除第 i 个以外的标签。
func (a *App) closeOthers(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.saveAll()
	keep := a.tabs[i]
	a.tabs = []*tab{keep}
	a.active = 0
	a.remember()
}

// closeToRight 关闭第 i 个标签右侧的所有标签。
func (a *App) closeToRight(i int) {
	if i < 0 || i >= len(a.tabs) {
		return
	}
	a.saveAll()
	a.tabs = a.tabs[:i+1]
	if a.active >= len(a.tabs) {
		a.active = len(a.tabs) - 1
	}
	a.remember()
}

// beginRename 打开重命名对话框。
func (a *App) beginRename(rel string) {
	a.renameRel = rel
	a.renameText = path.Base(rel)
	a.renameOpen = true
}

// commitRename 执行重命名，并同步更新打开着的标签。
func (a *App) commitRename() {
	old := a.renameRel
	name := strings.TrimSpace(a.renameText)
	a.renameOpen = false
	if old == "" || name == "" {
		return
	}
	dir := path.Dir(old)
	if dir == "." {
		dir = ""
	}
	next := path.Join(dir, name)
	if next == old {
		return
	}
	// 改名之前先记下链接图，改完才知道哪些引用要跟着改
	oldGraph := a.ensureGraph()
	if err := a.vault.Rename(old, next); err != nil {
		a.say(i18n.T("error.rename") + err.Error())
		return
	}
	for _, t := range a.tabs {
		switch {
		case t.rel == old:
			t.rel = next
		case strings.HasPrefix(t.rel, old+"/"):
			t.rel = next + strings.TrimPrefix(t.rel, old)
		}
	}
	a.refresh()
	a.remember()
	fixed := a.rewriteLinksAfterRename(oldGraph, old, next)
	if len(fixed) > 0 {
		a.refresh()
		a.say(fmt.Sprintf(i18n.T("rename.done_with_refs"), name, len(fixed)))
		return
	}
	a.say(i18n.T("rename.done") + name)
}

// newFolder 在 dir 下新建一个文件夹。
func (a *App) newFolder(dir string) {
	name := i18n.T("common.new_folder")
	for i := 2; ; i++ {
		rel := path.Join(dir, name)
		if abs, err := a.vault.Abs(rel); err == nil {
			if _, err := os.Stat(abs); os.IsNotExist(err) {
				if err := a.vault.Mkdir(rel); err != nil {
					a.say(i18n.T("error.folder_create") + err.Error())
					return
				}
				break
			}
		}
		name = fmt.Sprintf(i18n.T("folder.new_numbered"), i)
	}
	a.refresh()
	a.say(i18n.T("folder.created"))
}

// say 在状态栏上方弹一条短提示。
func (a *App) say(msg string) {
	a.toast = msg
	a.toastAt = time.Now()
}

// autosave 在停止输入一小会儿后写盘。
func (a *App) autosave() {
	if a.active < 0 || a.active >= len(a.tabs) {
		return
	}
	t := a.tabs[a.active]
	if t.buf == nil {
		return
	}
	if v := t.buf.Version(); v != t.version {
		t.version = v
		t.lastEdit = time.Now()
	}
	if t.buf.Modified() && !t.lastEdit.IsZero() && time.Since(t.lastEdit) > autosaveDelay {
		a.save(t)
	}
}

// wordCount 统计一段 Markdown 的字数：汉字逐字计，其他语言按词计。
func wordCount(s string) int {
	count := 0
	inWord := false
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r), unicode.Is(unicode.Katakana, r):
			count++
			inWord = false
		case unicode.IsLetter(r), unicode.IsDigit(r):
			if !inWord {
				count++
				inWord = true
			}
		default:
			inWord = false
		}
	}
	return count
}

// stripBOM 去掉 UTF-8 BOM，避免编辑器把它当成正文显示。
func stripBOM(s string) string {
	return strings.TrimPrefix(s, "\ufeff")
}

// fuzzyMatch 报告 query 的各字符是否按顺序出现在 text 中（忽略大小写）。
// 按 rune 比较，中文与 emoji 也能正常匹配。
func fuzzyMatch(text, query string) bool {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return true
	}
	text = strings.ToLower(text)
	needle := []rune(query)
	i := 0
	for _, r := range text {
		if i < len(needle) && r == needle[i] {
			i++
		}
		if i == len(needle) {
			return true
		}
	}
	return false
}

// allFiles 返回按名字排序的全部文件，供快速跳转使用。
func (a *App) allFiles() []*vault.Node {
	files := a.tree.Files()
	sort.SliceStable(files, func(i, j int) bool {
		return strings.ToLower(files[i].Rel) < strings.ToLower(files[j].Rel)
	})
	return files
}

// paletteResults 返回匹配查询的文稿：先列名字里直接包含查询的，再列模糊匹配的。
func (a *App) paletteResults() []*vault.Node {
	query := strings.ToLower(strings.TrimSpace(a.paletteQuery))
	files := a.allFiles()
	var exact, fuzzy []*vault.Node
	for _, f := range files {
		lower := strings.ToLower(f.Rel)
		switch {
		case query == "":
			exact = append(exact, f)
		case strings.Contains(lower, query), strings.Contains(strings.ToLower(f.Title()), query):
			exact = append(exact, f)
		case fuzzyMatch(f.Rel, query):
			fuzzy = append(fuzzy, f)
		}
	}
	out := append(exact, fuzzy...)
	if len(out) > 40 {
		out = out[:40]
	}
	return out
}

// kindIcon 返回文稿类型对应的图标。
func kindIcon(k vault.Kind) *ui.SVG {
	switch k {
	case vault.KindMarkdown:
		return iconFile
	case vault.KindText:
		return iconCode
	case vault.KindImage:
		return iconImage
	case vault.KindPDF:
		return iconFilePDF
	case vault.KindWord:
		return iconFileWord
	case vault.KindBoard:
		return iconBoard
	}
	return iconFile
}

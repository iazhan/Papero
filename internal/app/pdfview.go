package app

import (
	"fmt"
	"math"
	"time"

	"github.com/egoist/mygo/ui"

	"papero/internal/i18n"
)

// pdfTab 画出 PDF：所有页竖着排成一个连续的长卷，按需渲染可见的那几页。
//
// 为什么整页渲染成位图：PDF 的版面（字体、矢量、图片、透明）只有渲染引擎才算得准。
// 文字交互靠 pdftext.go 里那一层字符框，不靠位图。
func (a *App) pdfTab(c *ui.Context, pal chrome, tab *tab) {
	t := c.Theme()
	if tab.pdfErr != "" {
		ui.Column(c).Fill().Center().Padding(40).Children(func() {
			ui.Text(c, tab.pdfErr).FontSize(13).TextColor(t.Danger)
		})
		return
	}
	if tab.pdfView == nil || tab.pdfView.Pages() == 0 {
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, i18n.T("pdf.empty_pages")).FontSize(13).TextColor(t.TextMuted)
		})
		return
	}
	pages := tab.pdfView.Pages()
	sizes := a.pdfSizes(tab)
	a.loadPDFAnnots(tab)
	if tab.pdfAnnotColor == "" {
		tab.pdfAnnotColor = pdfAnnotColors[0].Key
	}

	// 适应宽度：按最宽的那一页算
	stageW := tab.pdfStage.W
	if stageW <= 0 {
		winW, _ := c.Size()
		stageW = winW - a.sidebarW*a.sidebarP - 110
	}
	panelW := float32(0)
	if tab.pdfPanel != 0 {
		panelW = pdfPanelWidth
	}
	maxW := float32(0)
	for _, s := range sizes {
		if s[0] > maxW {
			maxW = s[0]
		}
	}
	fitScale := fitWidthZoom(maxW, stageW-panelW-48)
	scale := tab.zoom
	if !tab.pdfManual || scale <= 0 {
		scale = fitScale
	}
	tab.zoom = scale

	// 布局：每页的 y 偏移（含页间距）
	tops := make([]float32, pages)
	y := float32(pdfPageGap)
	for i, sz := range sizes {
		tops[i] = y
		y += sz[1]*scale + pdfPageGap
	}
	contentW := maxW*scale + 2*pdfPageGap

	// 可见范围（多算一页做缓冲）
	first, last := a.pdfVisiblePages(sizes, tops, scale, tab.pdfScroll.Y, tab.pdfStage.H)
	tab.page = a.pdfPageAt(sizes, tops, scale, tab.pdfScroll.Y+tab.pdfStage.H/2)

	// 快捷键
	if c.Shortcut(ui.Cmd, ui.KeyC) && tab.pdfSelFrom.valid() {
		if text := a.pdfSelectionText(tab); text != "" {
			c.WriteClipboard(text)
			a.say(i18n.Tn(len([]rune(text)), "pdf.copied_chars.one", "pdf.copied_chars.other", len([]rune(text))))
		}
	}
	if c.Shortcut(ui.Cmd, ui.KeyA) {
		a.pdfSelectAll(tab)
	}
	if c.KeyDown(ui.KeyEscape) {
		tab.pdfSelFrom, tab.pdfSelTo = noPos, noPos
	}

	ui.Column(c).Fill().Children(func() {
		a.pdfToolbar(c, pal, tab, pages, scale)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			if tab.pdfPanel != 0 {
				a.pdfPanelView(c, pal, tab, sizes)
			}
			a.pdfScroller(c, pal, tab, sizes, tops, scale, contentW, first, last)
		})
	})
}

// pdfScroller 是右侧的连续滚动区。
func (a *App) pdfScroller(c *ui.Context, pal chrome, tab *tab, sizes [][2]float32,
	tops []float32, scale, contentW float32, first, last int) {
	t := c.Theme()
	pages := len(sizes)
	// 外面套一个盒子量视口：ScrollBoth.Bounds() 给的是**内容框**（滚到哪儿、
	// 内容多高），拿它算可见范围会把整篇都当成可见。
	wrap := ui.Box(c).Grow(1).Clip()
	wrap.Children(func() {
		ui.ScrollBoth(c).Fill().Background(t.Background).
			TrackScroll(&tab.pdfScroll).Children(func() {
			ui.Column(c).Width(contentW).Padding(pdfPageGap, 0).Gap(pdfPageGap).Children(func() {
				for i := 0; i < pages; i++ {
					// 视口外的页只留占位，保持高度与偏移不变
					visible := i >= first && i <= last
					a.pdfPage(c, pal, tab, i, sizes[i], scale, visible)
				}
				a.pdfInput(c, tab, sizes, tops, scale, contentW)
			})
		})
	})
	if r := wrap.Bounds(); r.W > 0 {
		tab.pdfStage = r
	}
	// 查找跳转 / 翻页跳转：把目标滚进视野
	if tab.pdfScrollToSel {
		a.pdfScrollToSelection(tab, sizes, tops, scale)
	} else if tab.pdfScrollToPage >= 0 {
		page := tab.pdfScrollToPage
		tab.pdfScrollToPage = -1
		if page >= 0 && page < len(tops) {
			target := tops[page] - pdfPageGap
			if target < 0 {
				target = 0
			}
			tab.pdfScroll.ScrollTo(0, target, ui.Smooth)
		}
	}
}

// pdfPageGap 是页与页之间的空隙，也是页面左右的内边距。
const pdfPageGap = 20

// pdfPage 画一页：位图 + 查找高亮 + 选区高亮。visible 为假时只留白占位。
func (a *App) pdfPage(c *ui.Context, pal chrome, tab *tab, i int, size [2]float32,
	scale float32, visible bool) {
	t := c.Theme()
	w, h := size[0]*scale, size[1]*scale

	ui.Row(c).Justify(ui.Center).Children(func() {
		page := ui.Box(c).Width(w).Height(h).
			Background(a.tokens.Page).Radius(3).
			Shadow(0, 2, 12, 0, a.tokens.PageShadow)
		if !visible {
			return
		}
		page.Children(func() {
			if img := a.pdfPageImage(tab, i, pdfDPI(scale)); img != nil {
				ui.Image(c, img).Size(w, h).Fit(ui.FillBox)
			}
			// 查找高亮
			for _, hit := range tab.pdfFindHits {
				if hit.Page != i {
					continue
				}
				for _, r := range pdfSelectionRuns(a.pdfCharsAt(tab, i), hit.From, hit.To, scale) {
					ui.Box(c).Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).
						Background(ui.Hex("#f59e0b").Alpha(0.35)).Radius(1).IgnoreInput(true)
				}
			}
			// 批注高亮
			for idx, an := range tab.pdfAnnots {
				if an.Page != i {
					continue
				}
				fill := pdfAnnotFill(an.Color)
				alpha := float32(0.32)
				if idx == tab.pdfAnnotSel {
					alpha = 0.5
				}
				for _, r := range pdfSelectionRuns(a.pdfCharsAt(tab, i), an.From, an.To, scale) {
					ui.Box(c).Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).
						Background(fill.Alpha(alpha)).Radius(1).IgnoreInput(true)
				}
			}
			// 选区高亮（跨页时每页只画落在自己身上的那一段）
			if from, to, ok := a.pdfRangeOn(tab, i); ok {
				for _, r := range pdfSelectionRuns(a.pdfCharsAt(tab, i), from, to, scale) {
					ui.Box(c).Absolute().Left(r.X).Top(r.Y).Width(r.W).Height(r.H).
						Background(t.Accent.Alpha(0.30)).Radius(1).IgnoreInput(true)
				}
			}
		})
	})
}

// pdfInput 是盖在整个长卷上的透明输入层：所有按下、拖动都在这里处理，
// 这样跨页拖动时「按钮还按着」的状态不会因为换页而丢失。
func (a *App) pdfInput(c *ui.Context, tab *tab, sizes [][2]float32, tops []float32,
	scale, contentW float32) {
	layer := ui.Box(c).Absolute().Fill().Cursor(ui.CursorText)
	x, y, ok := layer.PointerPosition()
	if !ok {
		if !layer.Pressed() {
			tab.pdfDragging = false
		}
		return
	}
	page := a.pdfPageAtY(sizes, tops, scale, y)
	if page < 0 || page >= len(sizes) {
		return
	}
	chars := a.pdfCharsAt(tab, page)
	if len(chars) == 0 {
		return
	}
	w := sizes[page][0] * scale
	px := (x - (contentW-w)/2) / scale
	py := (y - tops[page]) / scale
	idx := pdfHitChar(chars, px, py)
	pos := pdfPos{Page: page, Char: idx}

	switch {
	case layer.PressedInside():
		// 点在已有批注上：选中它，不进文字选择
		if idx := a.pdfAnnotAt(tab, page, chars, px, py); idx >= 0 {
			tab.pdfAnnotSel = idx
			tab.pdfPanel = 2
			tab.pdfAnnotNote = tab.pdfAnnots[idx].Note
			tab.pdfDragging = false
			break
		}
		// 连击检测：同一位置 400ms 内再按一次算连击
		now := c.Now()
		near := math.Hypot(float64(x-tab.pdfClickPos.X), float64(y-tab.pdfClickPos.Y)) < 6
		if now.Sub(tab.pdfClickAt) < 400*time.Millisecond && near {
			tab.pdfClickCount++
		} else {
			tab.pdfClickCount = 1
		}
		tab.pdfClickAt, tab.pdfClickPos = now, ui.Point{X: x, Y: y}

		switch {
		case tab.pdfClickCount >= 3:
			// 三击：选整段
			if from, to := pdfParagraphRange(chars, idx); from >= 0 {
				tab.pdfSelFrom = pdfPos{Page: page, Char: from}
				tab.pdfSelTo = pdfPos{Page: page, Char: to}
			}
			tab.pdfDragging = false
		case tab.pdfClickCount == 2:
			// 双击：选词
			if from, to := pdfWordRange(chars, idx); from >= 0 {
				tab.pdfSelFrom = pdfPos{Page: page, Char: from}
				tab.pdfSelTo = pdfPos{Page: page, Char: to}
			}
			tab.pdfDragging = false
		default:
			// 单击：定锚点，接着可以拖
			tab.pdfSelFrom, tab.pdfSelTo = pos, pos
			tab.pdfDragging = true
		}
	case tab.pdfDragging && layer.Pressed():
		tab.pdfSelTo = pos
	case !layer.Pressed():
		tab.pdfDragging = false
	}
}

// pdfToolbar 是 PDF 的工具栏：翻页、缩放、查找。
func (a *App) pdfToolbar(c *ui.Context, pal chrome, tab *tab, pages int, scale float32) {
	t := c.Theme()
	toolbarBar(c, pal, func() {
		toolButton(c, pal, iconChevronLeft, i18n.T("pdf.tool.prev_page"), i18n.T("pdf.tool.prev_page"), false, func() {
			if tab.page > 0 {
				tab.pdfScrollToPage = tab.page - 1
			}
		})
		tab.pageField.sync(fmt.Sprintf("%d", tab.page+1))
		tab.pageField.view(c, pal, i18n.T("pdf.tool.page_number"), i18n.T("pdf.tool.page_number_hint"), func(text string) {
			if n, ok := parsePageNumber(text); ok {
				if n > pages {
					n = pages
				}
				tab.pdfScrollToPage = n - 1
			}
		})
		ui.Text(c, fmt.Sprintf("/ %d", pages)).FontSize(11.5).TextColor(t.TextMuted)
		toolButton(c, pal, iconChevronRight, i18n.T("pdf.tool.next_page"), i18n.T("pdf.tool.next_page"), false, func() {
			if tab.page < pages-1 {
				tab.pdfScrollToPage = tab.page + 1
			}
		})
		toolSep(c, pal)
		maxW := float32(0)
		for _, s := range a.pdfSizes(tab) {
			if s[0] > maxW {
				maxW = s[0]
			}
		}
		fitScale := fitWidthZoom(maxW, tab.pdfStage.W-48)
		fit := !tab.pdfManual
		toolButton(c, pal, iconFitWidth, i18n.T("pdf.tool.fit_width"), i18n.T("pdf.tool.fit_width_tooltip"), fit, func() {
			tab.pdfManual = false
			tab.zoom = fitScale
		})
		toolSep(c, pal)
		tab.zoomField.sync(zoomPercentText(scale))
		zoomStepper(c, pal, &tab.zoomField,
			func() { tab.pdfManual = true; tab.zoom = stepZoomValue(scale, -1) },
			func() { tab.pdfManual = true; tab.zoom = stepZoomValue(scale, 1) },
			func(text string) {
				if z, ok := parseZoom(text); ok {
					tab.pdfManual = true
					tab.zoom = z
				}
			})
		toolSep(c, pal)
		toolButton(c, pal, iconSearch, i18n.T("pdf.tool.find"), i18n.T("pdf.tool.find_tooltip"), tab.pdfFindOpen, func() {
			tab.pdfFindOpen = !tab.pdfFindOpen
		})
		if tab.pdfFindOpen {
			tab.pdfFindField.sync(tab.pdfFind)
			tab.pdfFindField.view(c, pal, i18n.T("pdf.tool.find"), i18n.T("pdf.tool.find_hint"), func(text string) {
				a.pdfSearch(tab, text)
			})
			if len(tab.pdfFindHits) > 0 {
				ui.Text(c, fmt.Sprintf("%d/%d", tab.pdfFindAt+1, len(tab.pdfFindHits))).
					FontSize(11).TextColor(t.TextMuted)
				toolButton(c, pal, iconChevronLeft, i18n.T("pdf.find.prev"), i18n.T("pdf.find.prev"), false,
					func() { a.pdfFindNext(tab, -1) })
				toolButton(c, pal, iconChevronRight, i18n.T("pdf.find.next"), i18n.T("pdf.find.next"), false,
					func() { a.pdfFindNext(tab, 1) })
			}
		}
		toolSep(c, pal)
		toolButton(c, pal, iconSidebar, i18n.T("pdf.panel.thumbnails"), i18n.T("pdf.panel.toggle_tooltip"), tab.pdfPanel != 0, func() {
			if tab.pdfPanel != 0 {
				tab.pdfPanel = 0
			} else {
				tab.pdfPanel = 1
			}
		})
		// 有选区就能加高亮
		hasSel := tab.pdfSelFrom.valid() && tab.pdfSelTo.valid()
		if hasSel {
			toolButton(c, pal, iconCheck, i18n.T("pdf.tool.highlight"), i18n.T("pdf.tool.highlight_tooltip"), false, func() {
				a.addPDFAnnot(tab, tab.pdfAnnotColor)
				tab.pdfPanel = 2
			})
		}
		ui.Box(c).Grow(1)
		pw, ph, _ := tab.pdfView.Size(tab.page)
		toolInfo(c, pal, fmt.Sprintf("%.0f × %.0f pt", pw, ph), 200)
	})
}

// pdfSizes 取所有页的尺寸（点），按文档缓存。
func (a *App) pdfSizes(tab *tab) [][2]float32 {
	n := tab.pdfView.Pages()
	if len(tab.pdfSizeCache) == n {
		return tab.pdfSizeCache
	}
	sizes := make([][2]float32, n)
	for i := 0; i < n; i++ {
		w, h, err := tab.pdfView.Size(i)
		if err != nil || w <= 0 || h <= 0 {
			w, h = 595, 842
		}
		sizes[i] = [2]float32{w, h}
	}
	tab.pdfSizeCache = sizes
	return sizes
}

// pdfDPI 把缩放比换成渲染用的 DPI。
func pdfDPI(scale float32) float32 {
	dpi := 96 * scale
	if dpi < 36 {
		dpi = 36
	}
	if dpi > 400 {
		dpi = 400
	}
	return dpi
}

// pdfPageAtY 返回纵坐标 y 落在第几页（-1 表示落在页间距里）。
func (a *App) pdfPageAtY(sizes [][2]float32, tops []float32, scale, y float32) int {
	for i, top := range tops {
		if y >= top && y < top+sizes[i][1]*scale {
			return i
		}
	}
	return -1
}

// pdfPageAt 返回某个纵坐标最靠近的页（工具栏显示用）。
func (a *App) pdfPageAt(sizes [][2]float32, tops []float32, scale, y float32) int {
	if p := a.pdfPageAtY(sizes, tops, scale, y); p >= 0 {
		return p
	}
	best, bestD := 0, float32(1e18)
	for i, top := range tops {
		d := top - y
		if d < 0 {
			d = -d
		}
		if d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

// pdfVisiblePages 算出当前视口里能看到哪几页（前后各多给一页）。
func (a *App) pdfVisiblePages(sizes [][2]float32, tops []float32, scale, scrollY, viewH float32) (int, int) {
	first, last := -1, -1
	for i, top := range tops {
		bottom := top + sizes[i][1]*scale
		if bottom < scrollY || top > scrollY+viewH {
			continue
		}
		if first < 0 {
			first = i
		}
		last = i
	}
	if first < 0 {
		return 0, 0
	}
	if first > 0 {
		first--
	}
	if last < len(tops)-1 {
		last++
	}
	return first, last
}

// pdfPageImage 取某一页的位图，按 (页, DPI) 缓存。
func (a *App) pdfPageImage(tab *tab, page int, dpi float32) *ui.Bitmap {
	if tab.pdfView == nil {
		return nil
	}
	if tab.pdfCache == nil {
		tab.pdfCache = map[string]*ui.Bitmap{}
	}
	key := fmt.Sprintf("%d@%d", page, int(dpi))
	if bmp, ok := tab.pdfCache[key]; ok {
		return bmp
	}
	img, err := tab.pdfView.Render(page, dpi)
	if err != nil || img == nil {
		return nil
	}
	bmp := ui.NewBitmap(img)
	// 只留最近几页，避免放大缩小把内存吃光
	if len(tab.pdfCache) > 10 {
		tab.pdfCache = map[string]*ui.Bitmap{}
	}
	tab.pdfCache[key] = bmp
	return bmp
}

// pdfScrollToSelection 把视图滚到选区开头那一行：落在可视区上方三分之一处。
func (a *App) pdfScrollToSelection(tab *tab, sizes [][2]float32, tops []float32, scale float32) {
	if !tab.pdfScrollToSel || !tab.pdfSelFrom.valid() {
		return
	}
	tab.pdfScrollToSel = false
	page := tab.pdfSelFrom.Page
	if page < 0 || page >= len(tops) {
		return
	}
	chars := a.pdfCharsAt(tab, page)
	if len(chars) == 0 || tab.pdfSelFrom.Char >= len(chars) {
		return
	}
	y := tops[page] + chars[tab.pdfSelFrom.Char].Y0*scale
	target := y - tab.pdfStage.H/3
	if target < 0 {
		target = 0
	}
	tab.pdfScroll.ScrollTo(0, target, ui.Smooth)
}

// errText 把可能为 nil 的 error 写成字符串。
func errText(err error) string {
	if err == nil {
		return i18n.T("pdf.error.unknown")
	}
	return err.Error()
}

// pdfPanelWidth 是左侧面板的宽度。
const pdfPanelWidth = 172

// pdfPanelView 是左侧面板：缩略图与批注两个页签。
func (a *App) pdfPanelView(c *ui.Context, pal chrome, tab *tab, sizes [][2]float32) {
	t := c.Theme()
	ui.Column(c).Width(pdfPanelWidth).Shrink(0).
		Background(pal.window).Padding(6).Gap(6).Children(func() {
		// 页签
		ui.Row(c).Gap(4).Children(func() {
			for i, label := range []string{i18n.T("pdf.panel.thumbnails"), i18n.T("pdf.panel.annotations")} {
				i, label := i, label
				b := ui.ButtonBase(c).Height(22).Padding(2, 8).Radius(6).
					Cursor(ui.CursorPointer).Label(label)
				if tab.pdfPanel == i+1 {
					b.Background(a.tokens.AccentSoft)
					b.TextColor(t.Text)
				} else {
					b.TextColor(t.TextMuted)
				}
				b.OnClick(func() { tab.pdfPanel = i + 1 })
				b.Children(func() { ui.Text(c, label).FontSize(11) })
			}
			ui.Box(c).Grow(1)
			// 关闭
			closeBtn := ui.ButtonBase(c).Width(20).Height(22).Radius(6).
				Cursor(ui.CursorPointer).Label(i18n.T("pdf.panel.close"))
			closeBtn.Children(func() { ui.Icon(c, iconClose).Size(11, 11) })
			closeBtn.OnClick(func() { tab.pdfPanel = 0 })
		})

		if tab.pdfPanel == 1 {
			a.pdfThumbPanel(c, pal, tab, sizes)
			return
		}
		a.pdfAnnotPanel(c, pal, tab)
	})
}

// pdfThumbPanel 画所有页的缩略图。
func (a *App) pdfThumbPanel(c *ui.Context, pal chrome, tab *tab, sizes [][2]float32) {
	t := c.Theme()
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(8).Children(func() {
			for i := range sizes {
				i := i
				w := float32(124)
				h := w * sizes[i][1] / sizes[i][0]
				row := ui.Column(c).Gap(2).AlignItems(ui.Center).Cursor(ui.CursorPointer)
				row.OnClick(func() { tab.pdfScrollToPage = i })
				row.Children(func() {
					box := ui.Box(c).Width(w).Height(h).Radius(2).
						Background(a.tokens.Page).Shadow(0, 1, 5, 0, a.tokens.PageShadow)
					if i == tab.page {
						box.Border(2, t.Accent)
					}
					box.Children(func() {
						if bmp := a.pdfThumb(tab, i); bmp != nil {
							ui.Image(c, bmp).Size(w, h).Fit(ui.FillBox)
						}
					})
					ui.Text(c, fmt.Sprint(i+1)).FontSize(10).
						TextColor(t.TextMuted)
				})
			}
		})
	})
}

// pdfThumb 取某一页的缩略图（宽度约 124 DIP）。
func (a *App) pdfThumb(tab *tab, page int) *ui.Bitmap {
	if tab.pdfView == nil {
		return nil
	}
	if tab.pdfThumbs == nil {
		tab.pdfThumbs = map[int]*ui.Bitmap{}
	}
	if bmp, ok := tab.pdfThumbs[page]; ok {
		return bmp
	}
	pw, _, err := tab.pdfView.Size(page)
	if err != nil || pw <= 0 {
		return nil
	}
	dpi := 124 / pw * 72
	if dpi < 12 {
		dpi = 12
	}
	img, err := tab.pdfView.Render(page, float32(dpi))
	if err != nil || img == nil {
		return nil
	}
	bmp := ui.NewBitmap(img)
	tab.pdfThumbs[page] = bmp
	return bmp
}

// pdfAnnotPanel 画批注列表与备注编辑框。
func (a *App) pdfAnnotPanel(c *ui.Context, pal chrome, tab *tab) {
	t := c.Theme()
	if len(tab.pdfAnnots) == 0 {
		ui.Column(c).Padding(8).Gap(6).Children(func() {
			ui.Text(c, i18n.T("pdf.annotations.empty")).FontSize(11.5).TextColor(t.TextMuted)
			ui.Text(c, i18n.T("pdf.annotations.empty_hint")).FontSize(10.5).TextColor(t.TextMuted)
		})
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			for i, an := range tab.pdfAnnots {
				i, an := i, an
				row := ui.ButtonBase(c).Height(34).Padding(3, 6).Radius(6).
					Cursor(ui.CursorPointer).Label(i18n.T("pdf.annotations.item") + fmt.Sprint(i+1))
				if i == tab.pdfAnnotSel {
					row.Background(a.tokens.AccentSoft)
				} else if row.Hovered() {
					row.Background(t.SurfaceHover)
				}
				row.OnClick(func() {
					tab.pdfAnnotSel = i
					tab.pdfSelFrom = pdfPos{Page: an.Page, Char: an.From}
					tab.pdfSelTo = pdfPos{Page: an.Page, Char: an.To}
					tab.pdfScrollToSel = true
				})
				row.Children(func() {
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Box(c).Width(3).Height(22).Radius(2).Background(pdfAnnotFill(an.Color))
						ui.Column(c).Gap(1).Children(func() {
							ui.Text(c, fmt.Sprintf(i18n.T("pdf.annotations.page"), an.Page+1)).FontSize(10).
								TextColor(t.TextMuted)
							ui.Text(c, a.annotSummary(tab, i)).FontSize(11).SingleLine().
								Ellipsis("…")
						})
					})
				})
			}
		})
	})
	if tab.pdfAnnotSel >= 0 && tab.pdfAnnotSel < len(tab.pdfAnnots) {
		an := tab.pdfAnnots[tab.pdfAnnotSel]
		ui.Column(c).Gap(4).Children(func() {
			input := ui.TextInput(c, &tab.pdfAnnotNote).Placeholder(i18n.T("pdf.annotations.note_placeholder")).Label(i18n.T("pdf.annotations.note_label"))
			if input.Changed() {
				a.setPDFAnnotNote(tab, tab.pdfAnnotSel, tab.pdfAnnotNote)
			}
			ui.Row(c).Gap(6).Children(func() {
				// 换颜色
				for _, col := range pdfAnnotColors {
					col := col
					dot := ui.ButtonBase(c).Width(16).Height(16).Radius(8).
						Background(col.Fill).Cursor(ui.CursorPointer).Label(i18n.T("pdf.annotations.color") + pdfAnnotColorName(col.Key))
					if an.Color == col.Key {
						dot.Border(2, t.Text)
					}
					dot.OnClick(func() {
						tab.pdfAnnots[tab.pdfAnnotSel].Color = col.Key
						a.savePDFAnnots(tab)
					})
				}
				ui.Box(c).Grow(1)
				if ui.Button(c, i18n.T("common.delete")).Clicked() {
					a.deletePDFAnnot(tab, tab.pdfAnnotSel)
					tab.pdfAnnotNote = ""
				}
			})
		})
	}
}

package app

import (
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"papero/internal/i18n"
	"papero/internal/vault"
)

// view 由状态重建整个窗口。MyGo 是立即模式：这一帧画完就丢，状态全在 App 里。
//
// 窗口结构是「侧栏 + 一张纸」：侧栏铺满窗口高度，文稿放在一张圆角卡片里，
// 标签栏是卡片的顶部。标题栏隐藏，macOS 上红绿灯浮在侧栏上方，
// 其余平台由应用自己画最小化 / 最大化 / 关闭按钮。
func (a *App) view(c *ui.Context) {
	a.autosave()
	a.shortcuts(c)
	// 到点了就在后台问一次 GitHub 有没有新版本（设置里可以关）
	a.updateTick(c)
	// 语言在每帧开头同步一次：设置里改的只是 cfg，界面要下一帧才整体换过来。
	// 只认 cfg 这一个来源，不会出现同一帧上半个中文半个英文。
	i18n.SetLanguage(a.cfg.Language)
	// 每帧先清掉悬停预览，由这一帧真正被悬停的行重新设置
	a.linkHover = ""
	c.SetTheme(paperoTheme(a.tokens))
	pal := chromeFor(a.tokens)
	bar := c.TitleBar()
	c.Root().Background(pal.card)
	// 侧栏的开合是滑动而不是瞬移。进度挂在根行上：收起之后侧栏整块不在
	// 树里，动画状态存不住，只能存在一个每帧都在的元素上。
	root := ui.Row(c).Fill().MinWidth(0).AlignItems(ui.Stretch)
	a.sidebarP = clamp01(root.AnimateTo("sidebar", boolF(!a.sidebarCollapsed), sidebarSlide))
	root.Children(func() {
		// 收到 0 就不再画：留一个 0 宽的盒子只会让里面的按钮还能被点到。
		if a.sidebarP > 0 {
			a.sidebar(c, pal)
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			a.header(c, bar, pal)
			a.card(c, pal)
		})
	})
	a.overlays(c)
}

// sidebarSlide 是侧栏开合的曲线：Material 的标准缓动，起手不窜、收尾不拖。
var sidebarSlide = ui.Animation{
	Duration: 240 * time.Millisecond,
	Ease:     ui.CubicBezier(0.4, 0, 0.2, 1),
}

// boolF 把开关换成 0/1 喂给动画。
func boolF(on bool) float32 {
	if on {
		return 1
	}
	return 0
}

// clamp01 把动画值夹进 0..1。曲线本身不越界，但换成弹簧会，布局不能跟着越界。
func clamp01(v float32) float32 {
	return min(max(v, 0), 1)
}

// shortcuts 处理窗口级快捷键。
func (a *App) shortcuts(c *ui.Context) {
	switch {
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyP):
		a.commandOpen = true
		a.commandQuery = ""
		a.commandSel = 0
	case c.Shortcut(ui.Cmd, ui.KeyK), c.Shortcut(ui.Cmd, ui.KeyP):
		a.paletteOpen = true
		a.paletteQuery = ""
		a.paletteSel = 0
	case c.Shortcut(ui.Cmd, ui.KeyS):
		a.saveActive()
		a.say(i18n.T("common.saved"))
	case c.Shortcut(ui.Cmd, ui.KeyN):
		a.newNote(a.currentDir())
	case c.Shortcut(ui.Cmd, ui.KeyW):
		a.closeTab(a.active)
	case c.Shortcut(ui.Cmd, ui.KeyO):
		a.chooseVault()
	case c.Shortcut(ui.Cmd, ui.KeyComma):
		a.openSettings()
	case c.Shortcut(ui.Cmd, ui.KeyBackslash):
		a.sidebarCollapsed = !a.sidebarCollapsed
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyR):
		a.refresh()
		a.say(i18n.T("command.rescan_done"))
	}
}

// currentDir 返回新建文稿应放在哪个目录：当前文稿所在目录。
func (a *App) currentDir() string {
	if a.active >= 0 && a.active < len(a.tabs) {
		d := path.Dir(a.tabs[a.active].rel)
		if d == "." {
			return ""
		}
		return d
	}
	return ""
}

// headerSideW 是收起后顶栏左侧那块（品牌 + 展开按钮 + 分隔线）的宽度，
// 实测内容约 121，留几像素余量。写死是为了滑动时按进度让出宽度：内容本身
// 定宽，让的时候只是被裁掉，不会跟着重排（重排会让文字在中途跳字）。
const headerSideW float32 = 124

// headerSideFade 是顶栏那块内容的淡入曲线：进度到 from 时还是 0，到 from-span
// 时到 1。起点都卡在「这一截刚好整个露出来」的宽度上，再早就会看见半截。
func headerSideFade(p, from, span float32) float32 {
	return clamp01((from - p) / span)
}

// fade 只在真需要的时候套一层透明度：不透明就不套，免得静止的界面被拉去合层。
func fade(e ui.Element, o float32) ui.Element {
	if o < 1 {
		return e.Opacity(o)
	}
	return e
}

// header 是唯一的顶栏：左边软件名与侧栏开关，中间标签，右边窗口按钮。
// 标题栏隐藏后由视图自己画，macOS 上红绿灯浮在最左，其余平台用自绘按钮。
// 菜单栏取消了，常用动作放进「更多」菜单与命令面板。
func (a *App) header(c *ui.Context, bar ui.TitleBar, pal chrome) {
	// Obsidian: --header-height 40，标签条 1px 底边，标签从顶往下 6px 开始
	height := max(bar.Height, 40)
	ui.Column(c).Children(func() {
		row := ui.Row(c).Height(height).Padding(6, 0, 0, max(bar.Left, 10)).Gap(2).
			AlignItems(ui.Center).Background(pal.card).DragWindow()
		// 标签栏底部那条线自己画：画在子元素下面，当前标签的白底会把它盖住
		row.Draw(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y + r.H - 1, W: r.W, H: 1}, pal.hairline, 0)
		})
		row.Children(func() {
			// 侧栏收起时这里才有展开按钮；展开时开关在侧栏顶部。
			// 宽度按侧栏进度从 0 长到 headerSideW，标签才会跟着卡片一起平移，
			// 而不是等侧栏滑完了才「跳一格」。
			if w := headerSideW * (1 - a.sidebarP); a.sidebarCollapsed || w > 0.5 {
				side := ui.Box(c).Width(w).Height(height).Shrink(0)
				if w < headerSideW {
					side = side.ClipX()
				}
				side.Children(func() {
					ui.Row(c).Width(headerSideW).Height(height).Shrink(0).Gap(2).
						AlignItems(ui.Center).Children(func() {
						// 块比内容窄时右边会被裁掉，硬裁会露出「Pa」这种半个词。
						// 所以按宽度分两段淡入：品牌名先（进度 0.45 时它刚好整个露出来），
						// 按钮与分隔线后（0.2 时它们才完整），各自都不带截断地显形。
						mark := fade(ui.Box(c).Shrink(0), headerSideFade(a.sidebarP, 0.45, 0.25))
						mark.Children(func() {
							// 品牌跟过来：收起后侧栏整块不画，只剩按钮就认不出是哪个应用。
							brandMark(c, pal)
						})
						tail := fade(ui.Box(c).Shrink(0), headerSideFade(a.sidebarP, 0.2, 0.15))
						tail.Children(func() {
							// 按钮排在名字右边，和展开时「名字在左、开关在右」一致。
							ui.Row(c).Gap(2).AlignItems(ui.Center).Children(func() {
								ui.Box(c).Width(6)
								iconButton(c, pal, iconSidebar, i18n.T("sidebar.expand_tooltip"), func() {
									a.sidebarCollapsed = false
								})
								ui.Box(c).Width(4)
								vline(c, pal)
							})
						})
					})
				})
			}
			a.tabsInHeader(c, pal)
			// 右：最小化 / 最大化 / 关闭
			if runtime.GOOS != "darwin" {
				windowButtons(c, pal, a.win, height)
			}
		})
	})
}

// tabsInHeader 把标签直接排在标题栏里：溢出时可横向滚动，末尾是新建与「更多」。
func (a *App) tabsInHeader(c *ui.Context, pal chrome) {
	if a.active != a.lastTab {
		a.lastTab = a.active
		a.tabScrollPending = true
	}
	rects := make([]ui.Rect, len(a.tabs))
	overflow := a.tabScroll.MaxX > 1
	if overflow {
		a.scrollArrow(c, pal, false, a.tabScroll.X > 1, func() { a.scrollTabs(-1) })
	}
	scroll := ui.ScrollHorizontal(c).Grow(1).Scrollbars(false).TrackScroll(&a.tabScroll)
	scroll.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			for i, tab := range a.tabs {
				chip := a.tabChip(c, pal, i, tab)
				rects[i] = chip.Bounds()
			}
		})
	})
	if overflow {
		a.scrollArrow(c, pal, true, a.tabScroll.X < a.tabScroll.MaxX-1, func() { a.scrollTabs(1) })
	}
	if a.tabScrollPending && a.scrollTabIntoView(scroll, rects) {
		a.tabScrollPending = false
	}
	a.tabRects = rects
}

// scrollArrow 是标签栏两侧的小箭头，用来在没有滚动条时翻看标签。
func (a *App) scrollArrow(c *ui.Context, pal chrome, right, enabled bool, fn func()) {
	b := ui.ButtonBase(c).Size(18, 22).Radius(5).Label(map[bool]string{true: i18n.T("tab.scroll_right"), false: i18n.T("tab.scroll_left")}[right]).
		Tooltip(map[bool]string{true: i18n.T("tab.scroll_right_tooltip"), false: i18n.T("tab.scroll_left_tooltip")}[right]).Cursor(ui.CursorPointer)
	if enabled {
		b.TextColor(pal.muted)
		if b.Hovered() {
			b.Background(pal.tabHover)
			b.TextColor(pal.text)
		}
		b.OnClick(fn)
	} else {
		b.TextColor(pal.hairline)
	}
	b.Children(func() {
		icon := ui.Icon(c, iconChevron).Size(12, 12)
		if !right {
			icon.Rotate(180)
		}
	})
}

// scrollTabs 按方向翻看标签栏，一次半屏左右。
func (a *App) scrollTabs(dir int) {
	const step = 200
	to := a.tabScroll.X + float32(dir)*step
	if to < 0 {
		to = 0
	}
	if to > a.tabScroll.MaxX {
		to = a.tabScroll.MaxX
	}
	a.tabScroll.ScrollTo(to, 0, ui.Smooth)
}

// scrollTabIntoView 把当前标签滚进可见范围。rects 是这一帧各标签的位置，
// 上一帧的位置存在 tabRects 里，作为新建标签时的兜底。
func (a *App) scrollTabIntoView(scroll ui.Element, rects []ui.Rect) bool {
	if !scroll.Valid() {
		return false
	}
	view := scroll.Bounds()
	if view.W <= 0 {
		return false
	}
	idx := a.active
	if idx < 0 || idx >= len(a.tabs) {
		return true
	}
	b := ui.Rect{}
	switch {
	case idx < len(rects) && rects[idx].W > 0:
		b = rects[idx]
	case idx < len(a.tabRects) && a.tabRects[idx].W > 0:
		b = a.tabRects[idx]
	default:
		// 新标签还没有位置：直接滚到最右端。
		a.tabScroll.ScrollTo(1e9, 0, ui.Smooth)
		return true
	}
	const margin = 8
	switch {
	case b.X < view.X+margin:
		a.tabScroll.ScrollTo(a.tabScroll.X-(view.X+margin-b.X), 0, ui.Smooth)
	case b.X+b.W > view.X+view.W-margin:
		a.tabScroll.ScrollTo(a.tabScroll.X+(b.X+b.W-(view.X+view.W)+margin), 0, ui.Smooth)
	}
	return true
}

// tabChip 画一个标签，返回它的元素（用于把当前标签滚进视野）。
func (a *App) tabChip(c *ui.Context, pal chrome, i int, tab *tab) ui.Element {
	active := i == a.active
	// 标签的无障碍名字加上「标签」前缀，和侧栏里的同名文稿区分开。
	// 左右各多留 6px：底部外翻的弧占了边上 6px，内容要让开
	chip := ui.ButtonBase(c).Height(26).Padding(2, 14).Radius(7).
		Cursor(ui.CursorPointer).Label(i18n.T("tab.label_prefix") + tab.rel)
	// 贴顶的文件夹页：当前页是白的，下沿与正文连成一片；其余页透明，
	// 彼此之间用一道极细的竖线分开。
	// 和 Obsidian 一样：整条都是纸色，当前页用「上圆角 + 上左右三条边」框出来，
	// 白底正好盖住顶栏那条底线，于是它和下面的正文连成一片。
	chip.Height(34)
	switch {
	case active:
		// 页签形状自己画：上圆角 6，底部两侧 6px 向外翻的圆弧（Obsidian 的 --tab-curve）
		chip.Draw(func(p *ui.Painter, r ui.Rect) {
			drawTabShape(p, r, pal.card, pal.hairline)
		})
	case chip.Hovered():
		chip.Background(pal.tabHover).Radius(6, 6, 0, 0)
	}
	chip.OnClick(func() { a.active = i })
	// 中键直接关闭（和浏览器一致）。
	if chip.PressedInside() {
		if button, ok := chip.PressedButton(); ok && button == 2 {
			a.closeTab(i)
			return chip
		}
	}
	svc := c.Services()
	chip.PopoverMenu(func(m *ui.Menu) {
		if m.Item(i18n.T("common.close")).Chosen() {
			a.closeTab(i)
		}
		if m.Item(i18n.T("tab.close_others")).Chosen() {
			a.closeOthers(i)
		}
		if m.Item(i18n.T("tab.close_right")).Chosen() {
			a.closeToRight(i)
		}
		m.Separator()
		if m.Item(i18n.T("link.copy_path")).Chosen() {
			svc.WriteClipboard(tab.rel)
			a.say(i18n.T("link.copy_path_done"))
		}
		if m.Item(i18n.T("link.reveal")).Chosen() {
			a.reveal(tab.rel)
		}
	})
	chip.Children(func() {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, kindIcon(tab.kind)).Size(12, 12).TextColor(pal.muted)
			ink := pal.muted
			if active {
				ink = pal.text
			}
			ui.Text(c, tabTitle(tab)).FontSize(12).SingleLine().MaxWidth(170).TextColor(ink)
			if tab.dirty() {
				ui.Box(c).Size(5, 5).Radius(3).Background(pal.accent)
			}
			close := ui.ButtonBase(c).Size(16, 16).Radius(4).Cursor(ui.CursorPointer).Label(i18n.T("tab.close_prefix") + tab.rel)
			if close.Hovered() {
				close.Background(pal.hairline)
			}
			close.OnClick(func() { a.closeTab(i) })
			close.Children(func() { ui.Icon(c, iconClose).Size(9, 9).TextColor(pal.muted) })
		})
	})
	return chip
}

// tabTitle 是标签上显示的名字。
func tabTitle(t *tab) string {
	base := path.Base(t.rel)
	if t.kind == vault.KindMarkdown {
		return strings.TrimSuffix(base, path.Ext(base))
	}
	return base
}

// statusBar 是卡片底部的一行：左边路径，右边保存状态、编码、行数与显示模式。
func (a *App) statusBar(c *ui.Context, pal chrome) {
	ui.Row(c).Height(26).Padding(0, 12).Gap(10).AlignItems(ui.Center).Children(func() {
		if a.active < 0 || a.active >= len(a.tabs) {
			ui.Text(c, i18n.T("tab.empty")).FontSize(11).TextColor(pal.muted).Grow(1)
			return
		}
		tab := a.tabs[a.active]
		ui.Text(c, tab.rel).FontSize(11).TextColor(pal.muted).SingleLine().TruncateStart().Grow(1)
		// Markdown 的显示方式放在这里，正文区就只剩文稿本身。
		if tab.kind == vault.KindMarkdown {
			for _, m := range []struct{ key, label string }{
				{"blocks", i18n.T("settings.editor.mode_edit")}, {"split", i18n.T("settings.editor.mode_split")}, {"source", i18n.T("settings.editor.mode_source")},
			} {
				mode := m.key
				on := tab.mode == mode
				b := ui.ButtonBase(c).Height(20).Padding(1, 7).Radius(5).Cursor(ui.CursorPointer).Label(m.label)
				if on {
					b.Background(pal.tabHover)
					b.TextColor(pal.text)
				} else {
					b.TextColor(pal.muted)
				}
				b.OnClick(func() { tab.mode = mode })
				b.Children(func() { ui.Text(c, m.label).FontSize(11) })
			}
		}
		if tab.buf != nil {
			state := i18n.T("common.saved")
			if tab.dirty() {
				state = i18n.T("tab.unsaved")
			}
			ui.Text(c, state).FontSize(11).TextColor(pal.muted)
			ui.Text(c, "UTF-8").FontSize(11).TextColor(pal.muted)
			ui.Text(c, joinCounts(
				i18n.Tn(tab.buf.LineCount(), "tab.stats.lines.one", "tab.stats.lines.other", tab.buf.LineCount()),
				i18n.Tn(wordCount(tab.text()), "tab.stats.words.one", "tab.stats.words.other", wordCount(tab.text())))).
				FontSize(11).TextColor(pal.muted)
			if tab.kind == vault.KindMarkdown {
				if note := a.noteOf(tab.rel); note != nil && len(note.Back) > 0 {
					ui.Text(c, i18n.Tn(len(note.Back), "tab.backlinks.one", "tab.backlinks.other", len(note.Back))).FontSize(11).TextColor(pal.muted)
				}
			}
		} else {
			ui.Text(c, tab.kind.Label()).FontSize(11).TextColor(pal.muted)
		}
		if a.syncMsg != "" {
			ui.Text(c, a.syncMsg).FontSize(11).TextColor(pal.muted).SingleLine().Ellipsis("…").MaxWidth(200)
		}
		iconButton(c, pal, iconSync, i18n.T("common.sync_now"), func() { a.runSync() })
	})
}

// iconButton 是外壳里的图标按钮。
func iconButton(c *ui.Context, pal chrome, icon *ui.SVG, label string, fn func()) ui.Element {
	b := ui.ButtonBase(c).Size(26, 26).Radius(7).Label(label).Tooltip(label).Cursor(ui.CursorPointer)
	b.TextColor(pal.muted)
	if b.Hovered() {
		b.Background(pal.tabHover)
		b.TextColor(pal.text)
	}
	b.OnClick(fn)
	b.Children(func() { ui.Icon(c, icon).Size(15, 15) })
	return b
}

// textButton 是外壳里的文字按钮。
func textButton(c *ui.Context, pal chrome, label, tip string, fn func()) ui.Element {
	b := ui.ButtonBase(c).Height(24).Padding(2, 9).Radius(6).Label(tip).Tooltip(tip).Cursor(ui.CursorPointer)
	b.TextColor(pal.muted)
	if b.Hovered() {
		b.Background(pal.tabHover)
		b.TextColor(pal.text)
	}
	b.OnClick(fn)
	b.Children(func() { ui.Text(c, label).FontSize(11.5) })
	return b
}

// toastView 显示短提示，几秒后自动消失。
func (a *App) toastView(c *ui.Context) {
	if a.toast == "" || time.Since(a.toastAt) > 3*time.Second {
		a.toast = ""
		return
	}
	pal := chromeFor(a.tokens)
	c.After(500 * time.Millisecond)
	ui.Overlay(c, func() {
		ui.Row(c).Fill().Justify(ui.Center).AlignItems(ui.End).Padding(0, 0, 44, 0).Children(func() {
			ui.Box(c).Padding(8, 14).Radius(10).Background(pal.text).Children(func() {
				ui.Text(c, a.toast).FontSize(12).TextColor(pal.card)
			})
		})
	})
}

// chooseVault 让用户挑一个文件夹作为知识库。
func (a *App) chooseVault() {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       i18n.T("vault.choose_folder"),
		Directory:   true,
		ButtonLabel: i18n.T("common.open"),
	})
	if err != nil || len(paths) == 0 {
		return
	}
	a.setVault(paths[0])
}

// vline 是标题栏里分隔两组按钮的竖线。
func vline(c *ui.Context, pal chrome) {
	ui.Box(c).Width(1).Height(16).Margin(6, 6).Background(pal.hairline)
}

// card 是右侧的主区域：一张圆角卡片，里面是标签栏、正文与状态栏。
func (a *App) card(c *ui.Context, pal chrome) {
	ui.Column(c).Grow(1).MinWidth(0).Background(pal.card).Children(func() {
		a.content(c)
		a.statusBar(c, pal)
	})
}

// helpMenu 是「帮助」菜单。
func (a *App) helpMenu(m *ui.Menu) {
	if m.Item(i18n.T("command.shortcuts")).Chosen() {
		a.say(i18n.T("command.shortcuts_hint"))
	}
	if m.Item(i18n.T("command.about")).Chosen() {
		a.settingsPage = "about"
		a.openSettings()
	}
}

// windowButtons 画最小化 / 最大化 / 关闭：46 宽、贴到窗口右上角，
// 图形用 IconMCP 的 minus / maximize / minimize / close——
// 不是最大化时显示「向外扩」的 maximize，最大化之后换成「向内收」的 minimize（还原）。
func windowButtons(c *ui.Context, pal chrome, win *mygo.Window, height float32) {
	maximized := win != nil && win.IsMaximized()
	paint := func(icon *ui.SVG, label string, danger bool, fn func()) {
		b := ui.ButtonBase(c).Size(46, height).Label(label).Tooltip(label).Cursor(ui.CursorPointer)
		color := pal.text
		if b.Hovered() {
			if danger {
				b.Background(pal.danger)
				color = ui.Hex("#ffffff")
			} else {
				b.Background(pal.tabHover)
			}
		}
		b.OnClick(fn)
		b.Children(func() {
			ui.Icon(c, icon).Size(14, 14).TextColor(color)
		})
	}
	paint(iconMinus, i18n.T("window.minimize"), false, func() {
		if win != nil {
			win.Minimize()
		}
	})
	if maximized {
		paint(iconMinimizeWindow, i18n.T("window.restore"), false, func() {
			if win != nil {
				win.Unmaximize()
			}
		})
	} else {
		paint(iconMaximizeWindow, i18n.T("window.maximize"), false, func() {
			if win != nil {
				win.Maximize()
			}
		})
	}
	paint(iconClose, i18n.T("window.close"), true, func() {
		if win != nil {
			win.Close()
		}
	})
}

// focusedEditor 返回当前拿到焦点的编辑器（块编辑器或源码编辑器）。
func (a *App) focusedEditor() *editor.State {
	tab := a.currentTab()
	if tab == nil {
		return nil
	}
	return tab.state
}

// drawTabShape 画一个页签：上面两个内圆角，下面两个角向外翻（凹弧），
// 于是它的底边能和标签栏的底线接上，像浏览器与 Obsidian 的页签。
func drawTabShape(p *ui.Painter, r ui.Rect, fill, edge ui.Color) {
	// 照 Obsidian 的 app.css：
	//   --tab-radius-active: 6px 6px 0 0   （上圆角）
	//   --tab-curve: 6px                   （底部两侧向外翻的圆弧半径）
	// 底部两个角是半径 6 的四分之一圆，圆心在直边上，弧朝外鼓。
	const rad, c = 6, 6
	const k = 0.5523 // 圆的标准贝塞尔系数
	x, y, w, h := r.X, r.Y, r.W, r.H
	l, rt := x+c, x+w-c

	// 一条从右下起笔、绕到左下的路径：不闭合，用来描边（底边不描）
	open := &ui.Path{}
	open.MoveTo(x+w, y+h)
	open.CubeTo(x+w-c*k, y+h, rt, y+h-c+c*k, rt, y+h-c) // 右下外翻
	open.LineTo(rt, y+rad)
	open.CubeTo(rt, y+rad-rad*k, rt-rad+rad*k, y, rt-rad, y) // 右上圆角
	open.LineTo(l+rad, y)
	open.CubeTo(l+rad-rad*k, y, l, y+rad-rad*k, l, y+rad) // 左上圆角
	open.LineTo(l, y+h-c)
	open.CubeTo(l, y+h-c+c*k, x+c*k, y+h, x, y+h) // 左下外翻

	// 填充要闭合：在描边路径上补一条底边
	closed := &ui.Path{}
	closed.MoveTo(x, y+h)
	closed.CubeTo(x+c*k, y+h, l, y+h-c+c*k, l, y+h-c)
	closed.LineTo(l, y+rad)
	closed.CubeTo(l, y+rad-rad*k, l+rad-rad*k, y, l+rad, y)
	closed.LineTo(rt-rad, y)
	closed.CubeTo(rt-rad+rad*k, y, rt, y+rad-rad*k, rt, y+rad)
	closed.LineTo(rt, y+h-c)
	closed.CubeTo(rt, y+h-c+c*k, x+w-c*k, y+h, x+w, y+h)
	closed.Close()

	p.FillPath(closed, fill)
	p.StrokePath(open, 1, edge)
}

// joinCounts 把两段计数接成一个状态行。中间的「 · 」两种语言通用，不进文案表。
func joinCounts(parts ...string) string { return strings.Join(parts, " · ") }

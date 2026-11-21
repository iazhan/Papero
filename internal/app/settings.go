package app

import (
	"fmt"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"papero/internal/config"
	"papero/internal/i18n"
	"papero/internal/theme"
	"papero/internal/vault"
)

// 设置：左侧是页面导航，右侧是这一页的内容。
//
// 之前所有选项堆在一个长列表里，项一多就找不到东西。现在按「用的时候在想什么」
// 分成五页：通用（知识库与外观）、编辑器（写作时的行为）、同步、插件、关于。

// appVersion 是界面上显示的版本号。
const appVersion = "0.2.1"

// settingsPage 是一个设置页。
type settingsPage struct {
	Key   string
	Label string
	Icon  *ui.SVG
	// Hint 是页面标题下面那行说明。
	Hint string
}

// settingsPages 是设置页的顺序。
//
// 做成函数而不是包级变量：文案要经 i18n.T 取，而包级变量的初始化只在 init 时跑
// 一次——那时语言还是默认值，切到英文后导航会永远停在中文。
func settingsPages() []settingsPage {
	return []settingsPage{
		{Key: "general", Label: i18n.T("settings.nav.general"), Icon: iconSettings, Hint: i18n.T("settings.nav.general_hint")},
		{Key: "sidebar", Label: i18n.T("settings.nav.sidebar"), Icon: iconSidebar, Hint: i18n.T("settings.nav.sidebar_hint")},
		{Key: "theme", Label: i18n.T("settings.appearance.theme"), Icon: iconPalette, Hint: i18n.T("settings.nav.theme_hint")},
		{Key: "editor", Label: i18n.T("settings.nav.editor"), Icon: iconFile, Hint: i18n.T("settings.nav.editor_hint")},
		{Key: "sync", Label: i18n.T("settings.nav.sync"), Icon: iconSync, Hint: i18n.T("settings.nav.sync_hint")},
		{Key: "plugins", Label: i18n.T("settings.nav.plugins"), Icon: iconSparkle, Hint: i18n.T("settings.nav.plugins_hint")},
		{Key: "about", Label: i18n.T("settings.nav.about"), Icon: iconCommand, Hint: i18n.T("settings.nav.about_hint")},
	}
}

// openSettings 打开设置：有真窗口时开一个独立窗口，没有（测试 / 无头）时退回浮层。
func (a *App) openSettings() {
	if a.win != nil {
		a.openSettingsWindow()
		return
	}
	a.settingsOpen = true
}

// openSettingsWindow 打开独立的设置窗口。关闭按钮由系统标题栏提供，
// 所以在内容里不再放一个 ✕。
func (a *App) openSettingsWindow() {
	if a.settingsWin != nil {
		a.settingsWin.Focus()
		a.settingsWin.Invalidate()
		return
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:    i18n.T("settings.title"),
		Width:    860,
		Height:   600,
		MinWidth: 640, MinHeight: 420,
		StateKey: "papero-settings",
		Content:  ui.View(a.settingsWindowView),
		// 标题栏自己画：左边是「设置」，右边是窗口按钮，中间可以拖窗口。
		TitleBarStyle:  mygo.TitleBarHidden,
		TitleBarHeight: 32,
		// 按钮自己画，免得和系统那套叠在一起出现两套
		HideWindowButtons: true,
	})
	a.settingsWin = win
	win.OnClosed(func() { a.settingsWin = nil })
}

// settingsWindowView 是独立设置窗口的内容：自绘标题栏 + 两栏正文。
func (a *App) settingsWindowView(c *ui.Context) {
	c.SetTheme(paperoTheme(a.tokens))
	pal := chromeFor(a.tokens)
	c.Root().Background(pal.card)
	bar := c.TitleBar()
	height := max(bar.Height, 32)
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(height).Padding(0, 0, 0, max(bar.Left, 12)).Gap(2).
			AlignItems(ui.Center).Background(pal.window).DragWindow().Children(func() {
			ui.Text(c, i18n.T("settings.title")).FontSize(12.5).Bold().TextColor(pal.text).Padding(0, 12).
				SingleLine()
			ui.Box(c).Grow(1)
			if runtime.GOOS != "darwin" {
				windowButtons(c, pal, a.settingsWin, height)
			}
		})
		ui.Box(c).Height(1).Background(pal.hairline)
		ui.Row(c).Grow(1).MinHeight(0).AlignItems(ui.Stretch).Children(func() {
			a.settingsBody(c, pal, false)
		})
	})
}

// settingsView 是退路：没有独立窗口时，设置画在主窗口的浮层里。
func (a *App) settingsView(c *ui.Context) {
	if !a.settingsOpen {
		return
	}
	pal := chromeFor(a.tokens)
	ui.Modal(c, &a.settingsOpen, func() {
		ui.Column(c).Width(840).Height(620).Radius(14).Background(pal.card).Children(func() {
			ui.Row(c).Height(46).Padding(0, 12, 0, 18).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("settings.title")).FontSize(14).Bold().TextColor(pal.text).Grow(1)
				iconButton(c, pal, iconClose, i18n.T("common.close"), func() { a.settingsOpen = false })
			})
			ui.Box(c).Height(1).Background(pal.hairline)
			ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
				a.settingsBody(c, pal, true)
			})
		})
	})
}

// settingsBody 是设置的正文：左侧页面导航，右侧当前页。
func (a *App) settingsBody(c *ui.Context, pal chrome, modal bool) {
	if a.settingsPage == "" {
		a.settingsPage = "general"
	}
	a.settingsNav(c, pal)
	ui.Box(c).Width(1).FillHeight().Background(pal.hairline)
	ui.Column(c).Grow(1).MinWidth(0).Padding(22, 22).Gap(16).Children(func() {
		page := a.currentSettingsPage()
		ui.Column(c).Gap(3).Children(func() {
			ui.Text(c, page.Label).FontSize(15).Bold().TextColor(pal.text)
			ui.Text(c, page.Hint).FontSize(11.5).TextColor(pal.muted)
		})
		ui.Box(c).Height(1).Background(pal.hairline)
		ui.Scroll(c).Grow(1).MinWidth(0).Children(func() {
			ui.Column(c).Gap(16).Padding(0, 4).Children(func() {
				a.settingsContent(c, page.Key)
			})
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("settings.chrome.autosave")).FontSize(10.5).TextColor(pal.muted).Grow(1)
			if modal && ui.PrimaryButton(c, i18n.T("common.done")).Clicked() {
				a.commit()
				a.settingsOpen = false
			}
		})
	})
}

// settingsNav 画左侧的页面列表。
func (a *App) settingsNav(c *ui.Context, pal chrome) {
	ui.Column(c).Width(190).FillHeight().Padding(10).Gap(2).AlignItems(ui.Stretch).Children(func() {
		for _, page := range settingsPages() {
			page := page
			active := a.settingsPage == page.Key
			row := ui.ButtonBase(c).Height(34).Radius(7).Padding(0, 10).FillWidth().
				Cursor(ui.CursorPointer).Label(page.Label)
			if active {
				row.Background(pal.tabHover)
			}
			row.OnClick(func() { a.settingsPage = page.Key })
			row.Children(func() {
				ui.Row(c).Grow(1).Gap(9).AlignItems(ui.Center).Children(func() {
					color := pal.muted
					if active {
						color = pal.accent
					}
					ui.Icon(c, page.Icon).Size(15, 15).TextColor(color)
					ui.Text(c, page.Label).FontSize(12.5).TextColor(pal.text)
				})
			})
		}
	})
}

// currentSettingsPage 返回当前页。
func (a *App) currentSettingsPage() settingsPage {
	for _, page := range settingsPages() {
		if page.Key == a.settingsPage {
			return page
		}
	}
	return settingsPages()[0]
}

// settingsContent 画出某一页的内容。
func (a *App) settingsContent(c *ui.Context, key string) {
	switch key {
	case "sidebar":
		a.settingsSidebar(c)
	case "theme":
		a.settingsTheme(c)
	case "editor":
		a.settingsEditor(c)
	case "sync":
		a.settingsSync(c)
	case "plugins":
		a.settingsPlugins(c)
	case "about":
		a.settingsAbout(c)
	default:
		a.settingsGeneral(c)
	}
}

// settingsGeneral 是「通用」页：知识库、外观、字号。
func (a *App) settingsGeneral(c *ui.Context) {
	t := c.Theme()
	section(c, i18n.T("settings.general.section"), i18n.T("settings.general.section_hint"))
	a.fields(c, func() {
		a.field(c, i18n.T("settings.general.location"), i18n.T("settings.general.location_hint"), func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, a.vault.Root()).FontSize(11.5).TextColor(t.TextMuted).
					SingleLine().TruncateMiddle().MaxWidth(300)
				textButton(c, chromeFor(a.tokens), i18n.T("settings.general.change"), i18n.T("settings.general.change_tooltip"), a.chooseVault)
			})
		})
		a.field(c, i18n.T("settings.general.size"), "", func() {
			ui.Text(c, joinCounts(
				i18n.Tn(a.docCount(), "settings.general.size_docs.one", "settings.general.size_docs.other", a.docCount()),
				i18n.Tn(a.fileCount(), "settings.general.size_files.one", "settings.general.size_files.other", a.fileCount()))).
				FontSize(11.5).TextColor(t.TextMuted)
		})
	})

	section(c, i18n.T("settings.appearance.section"), i18n.T("settings.appearance.section_hint"))
	a.fields(c, func() {
		a.field(c, i18n.T("settings.appearance.theme"), i18n.T("settings.appearance.theme_hint"), func() {
			name := map[string]string{"": i18n.T("settings.theme.follow_system"), "system": i18n.T("settings.theme.follow_system"), "light": i18n.T("settings.theme.light"), "dark": i18n.T("settings.theme.dark")}[a.cfg.Theme]
			if name == "" {
				name = i18n.T("settings.theme.follow_system")
			}
			textButton(c, chromeFor(a.tokens), name, i18n.T("settings.appearance.theme_tooltip"),
				func() { a.settingsPage = "theme" })
		})
		a.field(c, i18n.T("settings.appearance.font_size"), i18n.T("settings.appearance.font_size_hint"), func() {
			size := a.cfg.FontSize
			a.stepper(c, &size, 12, 24, 1, i18n.T("settings.appearance.font_size"), "px", func(n float64) {
				a.cfg.FontSize = n
				a.commit()
			})
		})
		// 语言放在外观页而不是单独一页：设置浮层高度是固定的，另起一页会
		// 落到可视区外，用户翻不到。
		a.field(c, i18n.T("settings.appearance.language"), i18n.T("settings.appearance.language_hint"), func() {
			// 只写配置，不在这里切：view 每帧开头按 cfg 同步，
			// 这样这一帧不会出现「上半截中文、下半截英文」。
			labels := []string{i18n.T("settings.appearance.language_zh"), "English"}
			keys := []string{"", "en"}
			choice := labels[0]
			for i, k := range keys {
				if a.cfg.Language == k {
					choice = labels[i]
				}
			}
			ui.Select(c, &choice, labels).Width(150).Label(i18n.T("settings.appearance.language")).Changed()
			for i, label := range labels {
				if choice == label && a.cfg.Language != keys[i] {
					a.cfg.Language = keys[i]
					a.commit()
				}
			}
		})
	})
}

// stepper 是紧凑的数值控件：− 数值 单位 +，靠右对齐。
//
// 中间的数值仍然是 NumberInput（能直接打字、自动夹在范围内），只是把它的
// 两个步进按钮裁掉，换成两边圆形的 − 与 +，看起来才是一个整体的胶囊。
// apply 是「值真的变了」时调用的：两侧按钮直接写配置。
// 不能只改 *v——那是这一帧的局部变量，点击事件可能落在上一帧的闭包里，
// 写进去就丢了（NumberInput 自己处理输入，所以它写 *v 没问题）。
func (a *App) stepper(c *ui.Context, v *float64, lo, hi, step float64, label, unit string, apply func(float64)) {
	t := c.Theme()
	ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
		a.stepButton(c, "−", func() { apply(clampValue(*v-step, lo, hi)) })
		// 只留 NumberInput 的输入区：它的宽度是 Space(20)，右边两个按钮裁掉
		box := t.Space(20)
		ui.Box(c).Clip().Width(box).Children(func() {
			ui.NumberInput(c, v, lo, hi, step).Width(box + t.Space(16)).Label(label).Changed()
		})
		if unit != "" {
			ui.Text(c, unit).FontSize(11).TextColor(t.TextMuted)
		}
		a.stepButton(c, "+", func() { apply(clampValue(*v+step, lo, hi)) })
	})
}

// stepButton 是胶囊两端的圆形按钮。
func (a *App) stepButton(c *ui.Context, glyph string, fn func()) {
	t := c.Theme()
	b := ui.ButtonBase(c).Size(28, 28).Radius(7).Cursor(ui.CursorPointer).
		Label(i18n.T("settings.appearance.step") + glyph).Tooltip(i18n.T("settings.appearance.step") + glyph)
	b.Background(t.Surface).Border(1, t.Border)
	b.TextColor(t.Text)
	b.OnClick(fn)
	b.Children(func() { ui.Text(c, glyph).FontSize(13).TextColor(t.Text) })
}

// clampValue 把数值夹进区间。
func clampValue(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// segmented 是三选一的分段控件：比数值框更适合「差不多就行」的项。
func (a *App) segmented(c *ui.Context, v *float64, options []struct {
	Label string
	Value float64
}) {
	pal := chromeFor(a.tokens)
	ui.Row(c).Height(28).Padding(2).Radius(8).Gap(2).
		Background(pal.selectColor).Children(func() {
		for _, o := range options {
			o := o
			on := *v == o.Value
			b := ui.ButtonBase(c).Height(24).Padding(2, 12).Radius(6).
				Cursor(ui.CursorPointer).Label(i18n.T("settings.sidebar.width_label") + o.Label)
			if on {
				b.Background(pal.card).TextColor(pal.text)
			} else {
				b.TextColor(pal.muted)
			}
			b.OnClick(func() { *v = o.Value })
			b.Children(func() { ui.Text(c, o.Label).FontSize(11.5) })
		}
	})
}

// settingsSidebar 是「侧栏」页：宽度、最近打开、顶部动作的显隐与顺序。
func (a *App) settingsSidebar(c *ui.Context) {
	t := c.Theme()
	section(c, i18n.T("settings.sidebar.section_size"), i18n.T("settings.sidebar.section_size_hint"))
	a.fields(c, func() {
		a.field(c, i18n.T("settings.sidebar.width"), "200 – 420", func() {
			w := a.cfg.SidebarWidth
			a.stepper(c, &w, 200, 420, 10, i18n.T("settings.sidebar.width"), "px", func(n float64) {
				a.cfg.SidebarWidth = n
				a.commit()
			})
		})
	})

	section(c, i18n.T("settings.sidebar.section_content"), i18n.T("settings.sidebar.section_content_hint"))
	a.fields(c, func() {
		a.field(c, i18n.T("common.recent"), i18n.T("settings.sidebar.recent_hint"), func() {
			show := !a.cfg.HideRecent
			ui.Switch(c, &show).Label(i18n.T("settings.sidebar.recent_show")).Changed()
			if show == a.cfg.HideRecent {
				a.cfg.HideRecent = !show
				a.commit()
			}
		})
	})
	section(c, i18n.T("settings.sidebar.section_actions"), i18n.T("settings.sidebar.section_actions_hint"))
	if a.sidebarList.Key == nil {
		a.sidebarList.Key = func(i int) any {
			if i < len(a.cfg.SidebarActions) {
				return a.cfg.SidebarActions[i]
			}
			return i
		}
		a.sidebarList.Reorder = a.reorderSidebarActions
	}
	// 列表里始终是全部动作：开关管显隐，拖动管顺序
	n := len(a.cfg.SidebarActions)
	var pendingKey string
	var pendingOn bool
	if n > 0 {
		a.fields(c, func() {
			ui.List(c, &a.sidebarList, n, func(i int) {
				if i >= len(a.cfg.SidebarActions) {
					return
				}
				key := a.cfg.SidebarActions[i]
				act, ok := a.sidebarAction(key)
				if !ok {
					return
				}
				on := a.hasSidebarAction(key)
				// 用行号当分隔线的计数：List 只画可视的那几行，
				// 自增计数在滚动之后会错位（第一条看得见的行少一条线）
				a.groupRows = i
				a.fieldWith(c, func() {
					ui.Icon(c, iconMore).Size(12, 12).TextColor(t.TextMuted)
				}, act.label, act.shortcut, func() {
					// 中文「显示最近打开」连写，英文要空格，所以走格式串而不是拼接。
					ui.Switch(c, &on).Label(fmt.Sprintf(i18n.T("settings.sidebar.show_action"), act.label)).Changed()
					if on != a.hasSidebarAction(key) {
						pendingKey, pendingOn = key, on
					}
				})
			})
		})
	}
	// 改配置放到列表画完之后：画到一半改长度会越界
	if pendingKey != "" {
		a.setSidebarAction(pendingKey, pendingOn)
	}
}

// reorderSidebarActions 是拖动排序的回调：把 rows 挪到 to 之前。
func (a *App) reorderSidebarActions(rows []int, to int) {
	cur := a.cfg.SidebarActions
	inRows := func(i int) bool {
		for _, r := range rows {
			if r == i {
				return true
			}
		}
		return false
	}
	moved := make([]string, 0, len(rows))
	for _, r := range rows {
		if r >= 0 && r < len(cur) {
			moved = append(moved, cur[r])
		}
	}
	rest := make([]string, 0, len(cur))
	at := -1
	for i, k := range cur {
		if i == to {
			at = len(rest)
		}
		if !inRows(i) {
			rest = append(rest, k)
		}
	}
	if at < 0 {
		// 拖到列表末尾之后
		at = len(rest)
	}
	out := make([]string, 0, len(cur))
	out = append(out, rest[:at]...)
	out = append(out, moved...)
	out = append(out, rest[at:]...)
	a.cfg.SidebarActions = out
	a.commit()
}

// hasSidebarAction 报告某个动作现在是否显示。
func (a *App) hasSidebarAction(key string) bool {
	if a.sidebarActionIndex(key) < 0 {
		return false
	}
	for _, k := range a.cfg.HiddenActions {
		if k == key {
			return false
		}
	}
	return true
}

// sidebarActionIndex 返回动作在当前顺序里的位置，没有就是 -1。
func (a *App) sidebarActionIndex(key string) int {
	for i, k := range a.cfg.SidebarActions {
		if k == key {
			return i
		}
	}
	return -1
}

// setSidebarAction 显示/隐藏一个动作（顺序不动）。
func (a *App) setSidebarAction(key string, on bool) {
	kept := make([]string, 0, len(a.cfg.HiddenActions))
	for _, k := range a.cfg.HiddenActions {
		if k != key {
			kept = append(kept, k)
		}
	}
	if !on {
		kept = append(kept, key)
	}
	a.cfg.HiddenActions = kept
	a.commit()
}

// moveSidebarAction 把动作往前/往后挪一格。
func (a *App) moveSidebarAction(key string, dir int) {
	i := a.sidebarActionIndex(key)
	j := i + dir
	if i < 0 || j < 0 || j >= len(a.cfg.SidebarActions) {
		return
	}
	a.cfg.SidebarActions[i], a.cfg.SidebarActions[j] = a.cfg.SidebarActions[j], a.cfg.SidebarActions[i]
	a.commit()
}

// settingsEditor 是「编辑器」页：打开文稿时的默认行为。
func (a *App) settingsEditor(c *ui.Context) {
	t := c.Theme()
	section(c, i18n.T("settings.editor.section"), i18n.T("settings.editor.section_hint"))
	a.fields(c, func() {
		a.field(c, i18n.T("settings.editor.default_view"), i18n.T("settings.editor.default_view_hint"), func() {
			labels := []string{i18n.T("settings.editor.mode_edit"), i18n.T("settings.editor.mode_split"), i18n.T("settings.editor.mode_source")}
			keys := []string{"blocks", "split", "source"}
			choice := labels[0]
			for i, k := range keys {
				if a.cfg.Split == k {
					choice = labels[i]
				}
			}
			ui.Select(c, &choice, labels).Width(150).Label(i18n.T("settings.editor.default_view")).Changed()
			for i, label := range labels {
				if choice == label && a.cfg.Split != keys[i] {
					a.cfg.Split = keys[i]
					a.commit()
				}
			}
		})
		a.field(c, i18n.T("settings.editor.read_width"), i18n.T("settings.editor.read_width_hint"), func() {
			w := a.cfg.ReadWidth
			a.segmented(c, &w, []struct {
				Label string
				Value float64
			}{{i18n.T("settings.editor.read_narrow"), 620}, {i18n.T("settings.editor.read_comfortable"), 760}, {i18n.T("settings.editor.read_wide"), 940}})
			if w != a.cfg.ReadWidth {
				a.cfg.ReadWidth = w
				a.commit()
			}
		})
		a.field(c, i18n.T("settings.editor.live_layout"), i18n.T("settings.editor.live_layout_hint"), func() {
			live := a.cfg.LivePreview
			ui.Switch(c, &live).Label(i18n.T("settings.editor.live_layout")).Changed()
			if live != a.cfg.LivePreview {
				a.cfg.LivePreview = live
				a.commit()
			}
		})
		a.field(c, i18n.T("settings.editor.graph_window"), i18n.T("settings.editor.graph_window_hint"), func() {
			choice := i18n.T("settings.editor.graph_embed")
			if a.cfg.GraphMode == "window" {
				choice = i18n.T("settings.editor.graph_separate")
			}
			ui.Select(c, &choice, []string{i18n.T("settings.editor.graph_embed"), i18n.T("settings.editor.graph_separate")}).Width(150).Label(i18n.T("settings.editor.graph_window")).Changed()
			mode := "embed"
			if choice == i18n.T("settings.editor.graph_separate") {
				mode = "window"
			}
			if mode != a.cfg.GraphMode {
				a.cfg.GraphMode = mode
				a.commit()
				if mode == "window" && a.graphOpen {
					a.graphOpen = false
					a.openGraph()
				}
			}
		})
	})

	section(c, i18n.T("settings.editor.section_keys"), "")
	for _, line := range []string{
		i18n.T("settings.editor.key_wikilink"),
		i18n.T("settings.editor.key_enter"),
		i18n.T("settings.editor.key_drag"),
		i18n.T("settings.editor.key_click"),
	} {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(4, 4).Radius(2).Background(t.TextMuted)
			ui.Text(c, line).FontSize(11.5).TextColor(t.TextMuted)
		})
	}
}

// settingsPlugins 是「插件」页。
func (a *App) settingsPlugins(c *ui.Context) {
	t := c.Theme()
	section(c, i18n.T("settings.plugins.section"), i18n.T("settings.plugins.section_hint"))
	if a.plugins == nil || len(a.plugins.List()) == 0 {
		ui.Text(c, i18n.T("settings.plugins.empty")).FontSize(11.5).TextColor(t.TextMuted)
		return
	}
	// 一个插件一行，和其他页的卡片行同一套：名字 + 版本在左，说明在下面，开关在右
	a.fields(c, func() {
		for _, p := range a.plugins.List() {
			p := p
			enabled := !a.isPluginDisabled(p.ID)
			a.field(c, fmt.Sprintf("%s %s", p.Name, p.Version), p.Description, func() {
				on := enabled
				ui.Switch(c, &on).Label(p.Name).Changed()
				if on != enabled {
					a.setPluginEnabled(p.ID, on)
				}
			})
		}
	})
}

// settingsAbout 是「关于」页。
func (a *App) settingsAbout(c *ui.Context) {
	t := c.Theme()
	section(c, "Papero", i18n.T("settings.about.tagline"))
	a.fields(c, func() {
		a.field(c, i18n.T("settings.about.version"), "", func() {
			ui.Text(c, appVersion).FontSize(12)
		})
		// 自更新：左边是状态，右边是随状态变的按钮（检查 / 下载 / 重启并安装）
		a.field(c, i18n.T("settings.update.title"), a.updateHint(), func() {
			a.updateButton(c)
		})
		// 有新版本或者刚更新过，才多一行「更新日志」
		if a.upd.rel != nil || a.upd.updatedTo != "" {
			a.field(c, i18n.T("update.notes.title"), a.notesSubtitle(), func() {
				if ui.Button(c, i18n.T("update.notes.view")).Clicked() {
					a.upd.overlay = true
				}
			})
		}
		a.field(c, i18n.T("settings.update.auto"), i18n.T("settings.update.auto_hint"), func() {
			on := !a.cfg.NoUpdateCheck
			// 无障碍名字要和左边那行的标题区分开，否则按名字点会点到标题文字上
			ui.Switch(c, &on).Label(i18n.T("settings.update.auto_label")).Changed()
			if on != !a.cfg.NoUpdateCheck {
				a.cfg.NoUpdateCheck = !on
				a.commit()
			}
		})
		a.field(c, i18n.T("settings.about.platform"), "", func() {
			ui.Text(c, fmt.Sprintf("%s · %s", runtime.GOOS, runtime.GOARCH)).FontSize(12).TextColor(t.TextMuted)
		})
		a.field(c, i18n.T("settings.general.section"), "", func() {
			ui.Text(c, a.vault.Root()).FontSize(11.5).TextColor(t.TextMuted).
				SingleLine().TruncateMiddle().MaxWidth(330)
		})
		a.field(c, i18n.T("settings.about.config"), "", func() {
			dir, err := config.Dir()
			if err != nil {
				dir = i18n.T("settings.about.config_missing")
			}
			ui.Text(c, dir).FontSize(11.5).TextColor(t.TextMuted).
				SingleLine().TruncateMiddle().MaxWidth(330)
		})
	})
	section(c, i18n.T("settings.about.section_credits"), i18n.T("settings.about.credits_hint"))
	for _, line := range []string{
		i18n.T("settings.about.credit_mygo"),
		i18n.T("settings.about.credit_arcmd"),
		i18n.T("settings.about.credit_onevra"),
	} {
		ui.Text(c, "· "+line).FontSize(11.5).TextColor(t.TextMuted)
	}
}

// docCount 数知识库里的 Markdown 文稿（和侧栏底部的数字一致）。
func (a *App) docCount() int {
	count := 0
	var walk func(n *vault.Node)
	walk = func(n *vault.Node) {
		if n == nil {
			return
		}
		if !n.Dir && n.Kind == vault.KindMarkdown {
			count++
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(a.tree)
	return count
}

// fileCount 数知识库里的文件。
func (a *App) fileCount() int {
	count := 0
	var walk func(n *vault.Node)
	walk = func(n *vault.Node) {
		if n == nil {
			return
		}
		if !n.Dir {
			count++
		}
		for _, child := range n.Children {
			walk(child)
		}
	}
	walk(a.tree)
	return count
}

// settingsTheme 是「主题」页：明暗 + 三个颜色，其余颜色都是算出来的。
//
// 只给三个入口是刻意的——色板越大越难调出能看的一套；这里给什么就改什么，
// 面板层次、次要文字、悬停色由 theme.Derive 推导，算不准的（危险色）写死。
func (a *App) settingsTheme(c *ui.Context) {
	t := c.Theme()
	in := themeInput(a.cfg)
	pal := chromeFor(a.tokens)
	ui.Row(c).Gap(16).AlignItems(ui.Start).Children(func() {
		ui.Column(c).Width(300).Shrink(0).Gap(10).Children(func() {
			section(c, i18n.T("settings.theme.section_appearance"), i18n.T("settings.theme.section_appearance_hint"))
			ui.Row(c).Gap(6).Children(func() {
				for _, mode := range []struct{ key, label string }{
					{"system", i18n.T("settings.theme.follow_system")}, {"light", i18n.T("settings.theme.light")}, {"dark", i18n.T("settings.theme.dark")},
				} {
					a.modeChip(c, pal, mode.key, mode.label)
				}
			})

			section(c, i18n.T("settings.theme.section_color"), i18n.T("settings.theme.section_color_hint"))
			a.fields(c, func() {
				a.colorRow(c, i18n.T("settings.theme.paper"), i18n.T("settings.theme.paper_hint"), &in.Paper, in.Paper, "paper")
				a.colorRow(c, i18n.T("settings.theme.ink"), i18n.T("settings.theme.ink_hint"), &in.Ink, in.Ink, "ink")
				a.colorRow(c, i18n.T("settings.theme.accent"), i18n.T("settings.theme.accent_hint"), &in.Accent, in.Accent, "accent")
			})
			if a.themeCustomized() {
				ui.Row(c).Justify(ui.End).Children(func() {
					textButton(c, pal, i18n.T("settings.theme.reset"), i18n.T("settings.theme.reset_tooltip"),
						a.resetThemeColors)
				})
			}

			a.derivedCard(c)
		})
		ui.Column(c).Grow(1).MinWidth(0).Gap(10).Children(func() {
			ui.Text(c, i18n.T("settings.theme.preview")).FontSize(12).Bold().TextColor(t.Text)
			a.themePreview(c)
			a.themeSamples(c)
			a.contrastCard(c)
		})
	})
}

// modeChip 是明暗三种选择的按钮。
func (a *App) modeChip(c *ui.Context, pal chrome, key, label string) {
	on := a.cfg.Theme == key || (a.cfg.Theme == "" && key == "system")
	b := ui.ButtonBase(c).Height(26).Padding(2, 10).Radius(7).
		Cursor(ui.CursorPointer).Label(i18n.T("settings.theme.swatch_label") + label)
	switch {
	case on:
		b.Background(pal.selectColor)
		b.TextColor(pal.text)
	case b.Hovered():
		b.Background(pal.tabHover)
		b.TextColor(pal.text)
	default:
		b.Border(1, pal.hairline)
		b.TextColor(pal.muted)
	}
	b.OnClick(func() {
		a.cfg.Theme = key
		// 明暗各有一套内置颜色；切过去时放下上一套的自定义值，
		// 否则白纸会被带到深色模式里
		a.cfg.ThemePaper, a.cfg.ThemeInk, a.cfg.ThemeAccent = "", "", ""
		a.commit()
		a.reloadTheme()
	})
	b.Children(func() { ui.Text(c, label).FontSize(11) })
}

// colorRow 是一行「名字 + 说明 + 色块」。
// current 是这一帧生效的颜色，用它当基准：配置里为空时表示用内置值，
// 直接拿空串比会把内置值当成「用户改过」，一进页面就把默认色写死。
func (a *App) colorRow(c *ui.Context, label, hint string, target *ui.Color, current ui.Color, key string) {
	t := c.Theme()
	_ = current
	// 和「通用」页同一套卡片行：名字 + 说明在左，色块落在控件位
	a.field(c, label, hint, func() {
		sw := ui.ButtonBase(c).Size(46, 26).Radius(6).Cursor(ui.CursorPointer).
			Label(fmt.Sprintf(i18n.T("settings.theme.picker_label"), label)).Tooltip(i18n.T("settings.theme.picker_tooltip"))
		sw.Background(*target).Border(1, t.Border)
		sw.OnClick(func() {
			a.themePickKey = key
			a.themePickColor = *target
			a.themePickOpen = true
		})
		// 只用取色器本体，不带下面那一排常用色：框架自带的 ColorWell 会连
		// 一排与主题无关的色块一起弹出来，还容易溢出弹窗
		if a.themePickKey == key {
			open := &a.themePickOpen
			ui.Popover(c, sw, open, func() {
				// 框架的取色器末尾带一排常用色，对「只调三个颜色」来说纯属噪声，
				// 这里裁掉：保留色相方块、色相条、透明度与色值输入
				ui.Box(c).Clip().Height(pickerHeight(t)).Children(func() {
					ui.Column(c).Padding(10).Gap(8).Children(func() {
						ui.ColorPicker(c, &a.themePickColor).Changed()
						if hexOf(a.themePickColor) != hexOf(*target) {
							a.setThemeColor(key, a.themePickColor)
							*target = a.themePickColor
						}
					})
				})
			})
		}
	})
}

// themeCustomized 报告三个颜色里有没有被改过。
func (a *App) themeCustomized() bool {
	return a.cfg.ThemePaper != "" || a.cfg.ThemeInk != "" || a.cfg.ThemeAccent != ""
}

// resetThemeColors 把三个颜色恢复成当前明暗的内置值。
func (a *App) resetThemeColors() {
	a.cfg.ThemePaper, a.cfg.ThemeInk, a.cfg.ThemeAccent = "", "", ""
	a.commit()
	a.reloadTheme()
}

// setThemeColor 存下一个颜色并重算 token。
func (a *App) setThemeColor(key string, c ui.Color) {
	switch key {
	case "paper":
		a.cfg.ThemePaper = hexOf(c)
	case "ink":
		a.cfg.ThemeInk = hexOf(c)
	case "accent":
		a.cfg.ThemeAccent = hexOf(c)
	}
	a.commit()
	a.reloadTheme()
}

// derivedCard 列出「你没改、但是算出来的」几个颜色，说明推导不是黑箱。
//
// 排两列：一列五行会顶出设置浮层的可视区（浮层高度固定），而这张卡片恰恰是
// 要让人看见「改一个色，这些都跟着变」——被裁掉一半等于没画。
func (a *App) derivedCard(c *ui.Context) {
	tk := a.tokens
	// 选区是半透明的：色块要画「压在纸上的样子」，否则透出卡片底色，
	// 旁边那个色值也和眼睛看到的对不上。
	sel := tk.Paper.Mix(tk.Selection, float32(tk.Selection.A)/255)
	rows := []struct {
		name  string
		color ui.Color
	}{
		{i18n.T("settings.theme.derived_panel"), tk.Panel},
		{i18n.T("settings.theme.derived_line"), tk.Line},
		{i18n.T("settings.theme.derived_muted"), tk.InkMuted},
		{i18n.T("settings.theme.derived_accent_hover"), tk.AccentHover},
		{i18n.T("settings.theme.derived_selection"), sel},
	}
	ui.Column(c).FillWidth().Radius(10).Border(1, tk.Line).Padding(10).Gap(6).
		Children(func() {
			ui.Text(c, i18n.T("settings.theme.derived")).FontSize(11).Bold().TextColor(tk.Ink)
			for i := 0; i < len(rows); i += 2 {
				ui.Row(c).FillWidth().Gap(10).Children(func() {
					for j := i; j < i+2; j++ {
						if j < len(rows) {
							derivedCell(c, tk, rows[j].name, rows[j].color)
							continue
						}
						// 单数行的右半边留一个同样宽的空位：不留的话左边那格
						// 会吃满整行，它的色值就跑到第二列的右边去了
						ui.Box(c).Grow(1)
					}
				})
			}
		})
}

// derivedCell 是推导色表格里的一格：色块 + 名字 + 色值。
func derivedCell(c *ui.Context, tk theme.Tokens, name string, col ui.Color) {
	ui.Row(c).Grow(1).MinWidth(0).Height(20).AlignItems(ui.Center).Gap(6).Children(func() {
		ui.Box(c).Width(14).Height(14).Radius(4).
			Background(col).Border(1, tk.Ink.Alpha(0.15))
		ui.Text(c, name).FontSize(10.5).TextColor(tk.Ink).SingleLine().Ellipsis("…")
		ui.Box(c).Grow(1)
		ui.Text(c, theme.Hex(col)).FontSize(9).TextColor(tk.InkMuted)
	})
}

// contrastCard 是对比度体检：只列不达标的项。
func (a *App) contrastCard(c *ui.Context) {
	issues := a.tokens.Check()
	if len(issues) == 0 {
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, iconCheck).Size(12, 12).TextColor(a.tokens.InkMuted)
			ui.Text(c, i18n.T("settings.theme.contrast_ok")).FontSize(11).TextColor(a.tokens.InkMuted)
		})
		return
	}
	ui.Column(c).FillWidth().Radius(10).Border(1, a.tokens.Line).
		Padding(10).Gap(6).Children(func() {
		ui.Text(c, fmt.Sprintf(i18n.T("settings.theme.contrast_bad"), len(issues))).
			FontSize(11).Bold().TextColor(a.tokens.Ink)
		for _, is := range issues {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Text(c, is.What).FontSize(10.5).TextColor(a.tokens.Ink).Grow(1)
				ui.Text(c, fmt.Sprintf("%.2f / %.1f", is.Got, is.Target)).
					FontSize(10.5).TextColor(a.tokens.Danger)
			})
		}
	})
}

// pickerHeight 是取色器「到色值输入框为止」的高度，用来裁掉末尾那排常用色。
// 用主题的空间单位算，跟着 DPI 缩放走；写死像素在高分屏上会切错地方。
func pickerHeight(t *ui.Theme) float32 { return t.Space(66) }

// hexOf 把颜色写成 #rrggbb。
func hexOf(c ui.Color) string { return theme.Hex(c) }

// themePreview 是实时预览：一扇缩小版的主窗口。
//
// 只画色块没用——主题改的从来不只是正文，标签、侧栏、状态栏各吃一批不同的 token。
func (a *App) themePreview(c *ui.Context) {
	tk := a.tokens
	ui.Column(c).FillWidth().Radius(10).Background(tk.Paper).
		Border(1, tk.Line).Clip().Children(func() {
		// 标签栏
		ui.Row(c).Height(30).Padding(6, 6).Gap(4).AlignItems(ui.Center).
			Background(tk.Paper).Children(func() {
			for i, name := range []string{i18n.T("settings.preview.tab_doc"), "notes"} {
				tab := ui.Row(c).Height(24).Padding(0, 9).AlignItems(ui.Center)
				if i == 0 {
					tab.Radius(6).Background(tk.Paper).Border(1, tk.Line)
				}
				tab.Children(func() {
					ui.Text(c, name).FontSize(10.5).
						TextColor(map[bool]ui.Color{true: tk.Ink, false: tk.InkMuted}[i == 0])
				})
			}
		})
		ui.Box(c).Height(1).Background(tk.Line)

		ui.Row(c).Grow(1).AlignItems(ui.Stretch).Children(func() {
			// 文件树
			ui.Column(c).Width(88).Shrink(0).Padding(6).Gap(1).
				Background(tk.Panel).Children(func() {
				for i, name := range []string{i18n.T("settings.preview.tab_doc"), i18n.T("settings.preview.file_notes"), i18n.T("settings.preview.file_canvas"), i18n.T("settings.preview.file_pdf")} {
					row := ui.Row(c).Height(18).Padding(0, 5).Radius(4).AlignItems(ui.Center)
					if i == 0 {
						row.Background(tk.PanelPressed)
					}
					row.Children(func() {
						ui.Text(c, name).FontSize(9.5).TextColor(tk.Ink)
					})
				}
			})
			// 正文 + 行号
			ui.Row(c).Grow(1).MinWidth(0).Padding(8, 10).Gap(8).
				Background(tk.Paper).Children(func() {
				ui.Column(c).Shrink(0).Gap(3).Children(func() {
					for i := 1; i <= 5; i++ {
						ui.Text(c, fmt.Sprint(i)).FontSize(8.5).TextColor(tk.InkMuted)
					}
				})
				ui.Column(c).Grow(1).MinWidth(0).Gap(5).Children(func() {
					ui.Text(c, i18n.T("settings.preview.heading")).FontSize(12.5).Bold().TextColor(tk.Ink)
					ui.Text(c, i18n.T("settings.preview.body")).FontSize(9.5).TextColor(tk.Ink)
					ui.Text(c, i18n.T("settings.preview.link")).FontSize(9.5).TextColor(tk.Accent)
					ui.Box(c).Radius(4).Padding(5).Background(tk.CodeBg).Children(func() {
						ui.Text(c, "const sum = (a, b) => a + b").FontSize(8.5).
							TextColor(tk.CodeText)
					})
				})
			})
		})

		// 状态栏
		ui.Row(c).Height(20).Padding(0, 8).AlignItems(ui.Center).Gap(6).
			Background(tk.Panel).Children(func() {
			ui.Icon(c, iconCheck).Size(9, 9).TextColor(tk.InkMuted)
			ui.Text(c, i18n.T("common.saved")).FontSize(9).TextColor(tk.InkMuted)
			ui.Box(c).Grow(1)
			ui.Text(c, "UTF-8").FontSize(9).TextColor(tk.InkMuted)
		})
	})
}

// themeSamples 是控件样例：按钮、输入框、提示条各用一组不同的 token。
func (a *App) themeSamples(c *ui.Context) {
	tk := a.tokens
	ui.Column(c).FillWidth().Radius(10).Border(1, tk.Line).Padding(10).Gap(8).
		Children(func() {
			ui.Row(c).Gap(6).Children(func() {
				ui.Row(c).Height(26).Padding(0, 12).Radius(6).AlignItems(ui.Center).
					Background(tk.Accent).Children(func() {
					ui.Text(c, i18n.T("settings.preview.primary")).FontSize(11).TextColor(tk.AccentInk)
				})
				ui.Row(c).Height(26).Padding(0, 12).Radius(6).AlignItems(ui.Center).
					Background(tk.Panel).Border(1, tk.Line).Children(func() {
					ui.Text(c, i18n.T("settings.preview.secondary")).FontSize(11).TextColor(tk.Ink)
				})
				ui.Row(c).Height(26).Padding(0, 10).Radius(6).AlignItems(ui.Center).
					Border(1, tk.Line).Children(func() {
					ui.Text(c, i18n.T("settings.preview.search")).FontSize(11).TextColor(tk.InkMuted)
				})
			})
			for _, s := range []struct {
				label string
				fill  ui.Color
			}{
				{i18n.T("settings.preview.warning"), tk.Accent},
				{i18n.T("settings.preview.error"), tk.Danger},
			} {
				ui.Row(c).Padding(6, 10).Radius(6).Background(s.fill.Alpha(0.14)).
					Children(func() {
						ui.Text(c, s.label).FontSize(10.5).TextColor(tk.Ink)
					})
			}
		})
}

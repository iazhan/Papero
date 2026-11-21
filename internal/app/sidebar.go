package app

import (
	"fmt"
	"path"
	"strings"

	"github.com/egoist/mygo/ui"

	"papero/internal/i18n"
	"papero/internal/vault"
)

// sidebar 画出左侧栏，结构是「动作 + 当前知识库 + 最近 + 工作区间」：
//
//	新建文稿 / 快速搜索 / 打开知识库 / 发现
//	文档 | 大纲    搜索框
//	目录树（或大纲）
//	最近打开
//	工作区间
func (a *App) sidebar(c *ui.Context, pal chrome) {
	// 面板比外框窄左右各 8。开合时外框宽度按进度收放、面板整体往左退，
	// 面板自己定宽不动，里面的文字就不会一边缩一边重排——只是被窗口左边缘吃掉。
	const pad float32 = 8
	p := a.sidebarP
	w := a.sidebarW - pad*2
	outer := ui.Box(c).Width(a.sidebarW*p+pad*2).Shrink(0).FillHeight().Padding(pad, 4, pad, pad)
	if p < 1 {
		outer = outer.ClipX()
	}
	outer.Children(func() {
		// 浮起的面板：四周留出边距，圆角 + 轻微渐变 + 柔和投影，不画描边。
		// 背景与正文同色，面板只靠投影与一道极淡的边立起来。
		panel := ui.Box(c).Width(w).Shrink(0).FillHeight()
		if p < 1 {
			panel = panel.Translate(-(1-p)*w, 0)
		}
		panel.Children(func() {
			ui.Column(c).Fill().Radius(14).Background(pal.glass).
				Border(1, pal.hairline).Shadow(0, 3, 18, 0, pal.shadow).
				Padding(0, 8, 10, 10).Gap(2).
				Children(func() { a.sidebarBody(c, pal) })
		})
	})
}

// brandMark 是「应用图标 + Papero」。展开时在侧栏顶部；收起后侧栏不画了，
// 它跟着搬到标题栏的展开按钮旁边——不然收起后窗口左上角只剩一个按钮，
// 认不出这是哪个应用。
func brandMark(c *ui.Context, pal chrome) {
	ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
		// 应用图标本身，不刷成强调色：它靠三种颜色区分形状
		ui.Image(c, iconBrand).Size(16, 16)
		ui.Text(c, "Papero").FontSize(13).Bold().TextColor(pal.text).SingleLine()
	})
}

// sidebarBody 是面板里的内容。
func (a *App) sidebarBody(c *ui.Context, pal chrome) {
	// 品牌行：软件名与收起按钮都在侧栏里，和右边顶栏共用一条中线
	brand := ui.Row(c).Height(40).Shrink(0).Padding(0, 6).Gap(7).
		AlignItems(ui.Center).DragWindow()
	brand.Children(func() {
		brandMark(c, pal)
		ui.Box(c).Grow(1)
		iconButton(c, pal, iconSearch, i18n.T("sidebar.search_tooltip"), func() {
			a.paletteOpen = true
			a.paletteQuery = ""
			a.paletteSel = 0
		})
		iconButton(c, pal, iconSidebar, i18n.T("sidebar.collapse_tooltip"), func() {
			a.sidebarCollapsed = true
		})
	})
	for _, key := range a.cfg.SidebarActions {
		if !a.hasSidebarAction(key) {
			continue
		}
		if act, ok := a.sidebarAction(key); ok {
			a.navItem(c, pal, act.icon, act.label, act.shortcut, act.fn)
		}
	}

	a.viewTabs(c, pal)
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Column(c).Gap(1).AlignItems(ui.Stretch).Children(func() {
			switch a.sidebarTab {
			case 1:
				a.outlineRows(c, pal)
				return
			case 2:
				a.linksRows(c, pal)
				return
			}
			if a.tree != nil {
				a.treeRows(c, pal, a.tree, 0)
			}
		})
	})

	if a.sidebarTab == 2 {
		a.linkPreview(c, pal)
	}

	shown := 0
	for _, rel := range a.recent {
		if a.cfg.HideRecent {
			break
		}
		if shown >= 4 {
			break
		}
		if !a.recentStillExists(rel) {
			continue
		}
		if shown == 0 {
			sectionLabel(c, pal, i18n.T("common.recent"))
		}
		rel := rel
		a.smallRow(c, pal, iconClock, path.Base(rel), func() { a.open(rel) })
		shown++
	}

	if len(a.vaults) > 1 {
		sectionLabel(c, pal, i18n.T("sidebar.vaults"))
		for i, dir := range a.vaults {
			if i >= 3 {
				break
			}
			dir := dir
			current := dir == a.vault.Root()
			row := ui.ButtonBase(c).Height(24).Radius(6).Padding(0, 6).FillWidth().
				Cursor(ui.CursorPointer).Label(dir)
			if current {
				row.Background(pal.selectColor)
			} else if row.Hovered() {
				row.Background(pal.tabHover)
			}
			row.OnClick(func() {
				if !current {
					a.setVault(dir)
				}
			})
			row.Children(func() {
				ui.Row(c).Grow(1).Gap(5).AlignItems(ui.Center).Justify(ui.Start).Children(func() {
					ui.Icon(c, iconLibrary).Size(12, 12).TextColor(pal.muted)
					ui.Text(c, path.Base(dir)).FontSize(12).TextColor(pal.text).SingleLine().Ellipsis("…")
				})
			})
		}
	}

	a.updateBanner(c, pal)
	ui.Box(c).Height(1).Background(pal.hairline)
	a.navItem(c, pal, iconSettings, i18n.T("settings.title"), "Ctrl+,", func() { a.openSettings() })
	notes, files := a.tree.Stats()
	ui.Row(c).Height(22).Padding(0, 8).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, joinCounts(
			i18n.Tn(notes, "sidebar.stats.docs.one", "sidebar.stats.docs.other", notes),
			i18n.Tn(files, "sidebar.stats.files.one", "sidebar.stats.files.other", files))).FontSize(10.5).TextColor(pal.muted).Grow(1)
		textButton(c, pal, i18n.T("settings.nav.sync"), i18n.T("common.sync_now"), func() { a.runSync() })
	})
}

// viewTabs 是「文档 / 大纲」的轻量切换：两个小文字标签，当前项高亮，
// 右侧留一个在当前目录新建文稿的按钮。
func (a *App) viewTabs(c *ui.Context, pal chrome) {
	ui.Row(c).Height(26).Padding(4, 2, 0, 4).Gap(2).AlignItems(ui.Center).Children(func() {
		for i, label := range []string{i18n.T("tab.docs"), i18n.T("tab.outline"), i18n.T("tab.links")} {
			index := i
			on := a.sidebarTab == index
			b := ui.ButtonBase(c).Height(21).Padding(0, 9).Radius(6).Cursor(ui.CursorPointer).Label(label)
			if on {
				b.Background(pal.tabHover)
				b.TextColor(pal.text)
			} else {
				b.TextColor(pal.muted)
				if b.Hovered() {
					b.Background(pal.tabHover.Alpha(0.6))
					b.TextColor(pal.text)
				}
			}
			b.OnClick(func() { a.sidebarTab = index })
			b.Children(func() { ui.Text(c, label).FontSize(11.5) })
		}
		ui.Box(c).Grow(1)
		iconButton(c, pal, iconPlus, i18n.T("sidebar.new_here_tooltip"), func() { a.newNote(a.currentDir()) })
	})
}

// linksRows 画出当前文稿的出链、反向链接与还没创建的链接。
func (a *App) linksRows(c *ui.Context, pal chrome) {
	tab := a.currentTab()
	if tab == nil || tab.kind != vault.KindMarkdown {
		ui.Text(c, i18n.T("sidebar.links_only_md")).FontSize(11).TextColor(pal.muted).Padding(6)
		return
	}
	back, out, unresolved := a.linksOf()
	if len(back) == 0 && len(out) == 0 && len(unresolved) == 0 {
		ui.Text(c, i18n.T("sidebar.links_empty")).FontSize(11).TextColor(pal.muted).Padding(6)
		ui.Text(c, i18n.T("sidebar.links_empty_hint")).FontSize(11).TextColor(pal.muted).Padding(6)
		return
	}
	if len(back) > 0 {
		sectionLabel(c, pal, fmt.Sprintf(i18n.T("sidebar.backlinks"), len(back)))
		for _, note := range back {
			note := note
			row := a.linkRow(c, pal, note.Title, a.linkContext(note.Rel, tabTitle(tab)), func() { a.open(note.Rel) })
			a.trackHover(row, note.Rel)
		}
	}
	if len(out) > 0 {
		sectionLabel(c, pal, fmt.Sprintf(i18n.T("sidebar.outgoing"), len(out)))
		for _, note := range out {
			note := note
			row := a.linkRow(c, pal, note.Title, "", func() { a.open(note.Rel) })
			a.trackHover(row, note.Rel)
		}
	}
	if len(unresolved) > 0 {
		ui.Row(c).Height(22).Padding(6, 6, 0, 8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, fmt.Sprintf(i18n.T("sidebar.unresolved"), len(unresolved))).FontSize(10.5).TextColor(pal.muted).Grow(1)
			if len(unresolved) > 1 {
				textButton(c, pal, i18n.T("sidebar.create_all"), i18n.T("sidebar.create_all_tooltip"), func() {
					n := a.createMissingLinks(unresolved, tab.rel)
					a.say(fmt.Sprintf(i18n.T("sidebar.created_count"), n))
				})
			}
		})
		for _, name := range unresolved {
			name := name
			a.linkRow(c, pal, name, i18n.T("sidebar.unresolved_create"), func() { a.openWiki(name, "", tab.rel) })
		}
	}
}

// linkPreview 是链接页底部的预览区：显示指针所在链接的目标文稿开头。
// 做成固定区域而不是浮层，是因为浮层会挡住指针、让悬停状态卡住。
func (a *App) linkPreview(c *ui.Context, pal chrome) {
	ui.Column(c).Height(92).Padding(8, 10).Gap(3).Radius(8).Background(pal.window).Children(func() {
		if a.linkHover == "" {
			ui.Text(c, i18n.T("link.hover_empty_title")).FontSize(10.5).TextColor(pal.muted)
			ui.Text(c, i18n.T("link.hover_empty_hint")).FontSize(10.5).TextColor(pal.muted)
			return
		}
		title, excerpt := a.noteExcerpt(a.linkHover)
		ui.Text(c, title).FontSize(12).Bold().TextColor(pal.text).SingleLine().Ellipsis("…")
		ui.Text(c, a.linkHover).FontSize(10).TextColor(pal.muted).SingleLine().Ellipsis("…")
		if excerpt == "" {
			excerpt = i18n.T("link.no_body")
		}
		if runes := []rune(excerpt); len(runes) > 80 {
			excerpt = string(runes[:80]) + "…"
		}
		ui.Text(c, excerpt).FontSize(11).TextColor(pal.muted)
	})
}

// trackHover 记住指针停在哪一行上，供悬停预览定位。
// 用 PointerPosition 判断指针是否真的在这行里：Hovered 在指针移到不可交互的
// 空白处时可能还留着上一次的值。
func (a *App) trackHover(row ui.Element, rel string) {
	if !row.Hovered() {
		return
	}
	x, y, ok := row.PointerPosition()
	if !ok {
		return
	}
	r := row.Bounds()
	if x < 0 || y < 0 || x > r.W || y > r.H {
		return
	}
	_ = r
	a.linkHover = rel
}

// linkRow 是链接面板里的一行：标题 + 可选的次要说明。
func (a *App) linkRow(c *ui.Context, pal chrome, title, hint string, fn func()) ui.Element {
	height := float32(24)
	if hint != "" {
		height = 34
	}
	row := ui.ButtonBase(c).Height(height).Radius(6).Padding(2, 6).FillWidth().
		Cursor(ui.CursorPointer).Label(title)
	if row.Hovered() {
		row.Background(pal.tabHover)
	}
	row.OnClick(fn)
	row.Children(func() {
		ui.Column(c).Grow(1).Gap(1).Justify(ui.Center).Children(func() {
			ui.Text(c, title).FontSize(12.5).TextColor(pal.text).SingleLine().Ellipsis("…")
			if hint != "" {
				ui.Text(c, hint).FontSize(10.5).TextColor(pal.muted).SingleLine().Ellipsis("…")
			}
		})
	})
	return row
}

// recentStillExists 报告「最近打开」里的文稿还在不在当前知识库里。
func (a *App) recentStillExists(rel string) bool {
	if a.tree == nil {
		return false
	}
	n := a.tree.Find(rel)
	return n != nil && !n.Dir
}

// navItem 是侧栏顶部的动作行：图标、名字与可选的快捷键提示。
func (a *App) navItem(c *ui.Context, pal chrome, icon *ui.SVG, label, shortcut string, fn func()) {
	row := ui.ButtonBase(c).Height(28).Radius(7).Padding(0, 8).FillWidth().
		Cursor(ui.CursorPointer).Label(label)
	if row.Hovered() {
		row.Background(pal.tabHover)
	}
	row.OnClick(fn)
	row.Children(func() {
		ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Justify(ui.Start).Children(func() {
			ui.Icon(c, icon).Size(14, 14).TextColor(pal.muted)
			// 英文的动作名比中文长，侧栏宽度又固定，所以要能优雅截断，
			// 而不是把右边的快捷键挤出去。
			ui.Text(c, label).FontSize(12.5).TextColor(pal.text).Grow(1).SingleLine().Ellipsis("…")
			if shortcut != "" {
				ui.Text(c, shortcut).FontSize(10.5).TextColor(pal.muted)
			}
		})
	})
}

// smallRow 是「最近打开」里的一行。
func (a *App) smallRow(c *ui.Context, pal chrome, icon *ui.SVG, label string, fn func()) {
	row := ui.ButtonBase(c).Height(22).Radius(6).Padding(0, 6).FillWidth().Cursor(ui.CursorPointer).Label(label)
	if row.Hovered() {
		row.Background(pal.tabHover)
	}
	row.OnClick(fn)
	row.Children(func() {
		ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Justify(ui.Start).Children(func() {
			ui.Icon(c, icon).Size(11, 11).TextColor(pal.muted)
			ui.Text(c, label).FontSize(11.5).TextColor(pal.muted).SingleLine().Ellipsis("…")
		})
	})
}

// sectionLabel 是侧栏里的小节标题。
func sectionLabel(c *ui.Context, pal chrome, title string) {
	ui.Row(c).Height(22).Padding(6, 6, 0, 8).AlignItems(ui.Center).Children(func() {
		ui.Text(c, title).FontSize(10.5).TextColor(pal.muted)
	})
}

// treeRows 递归画目录树。搜索时只显示匹配的文稿与它们的上级目录。
func (a *App) treeRows(c *ui.Context, pal chrome, n *vault.Node, depth int) {
	filtering := strings.TrimSpace(a.filter) != ""
	for _, child := range n.Children {
		if filtering {
			if child.Dir {
				if !hasMatch(child, a.filter) {
					continue
				}
			} else if !fuzzyMatch(child.Title(), a.filter) && !fuzzyMatch(child.Rel, a.filter) {
				continue
			}
		}
		if child.Dir {
			collapsed := a.collapsed[child.Rel] && !filtering
			a.folderRow(c, pal, child, depth, collapsed)
			if !collapsed {
				a.treeRows(c, pal, child, depth+1)
			}
			continue
		}
		a.fileRow(c, pal, child, depth)
	}
}

// hasMatch 报告目录里是否有匹配查询的文稿。
func hasMatch(n *vault.Node, query string) bool {
	for _, f := range n.Files() {
		if fuzzyMatch(f.Title(), query) || fuzzyMatch(f.Rel, query) {
			return true
		}
	}
	return false
}

// folderRow 画一个目录行。
func (a *App) folderRow(c *ui.Context, pal chrome, n *vault.Node, depth int, collapsed bool) {
	row := ui.ButtonBase(c).Height(24).Radius(6).Padding(0, 6, 0, 4+float32(depth)*12).
		FillWidth().Cursor(ui.CursorPointer).Label(n.Rel)
	if row.Hovered() {
		row.Background(pal.tabHover)
	}
	row.OnClick(func() { a.collapsed[n.Rel] = !a.collapsed[n.Rel] })
	a.folderRowMenu(c, row, n)
	row.Children(func() {
		ui.Row(c).Grow(1).Gap(4).AlignItems(ui.Center).Justify(ui.Start).Children(func() {
			ui.Icon(c, iconChevron).Size(11, 11).Rotate(rotation(collapsed)).TextColor(pal.muted)
			ui.Icon(c, iconFolder).Size(13, 13).TextColor(pal.muted)
			ui.Text(c, n.Name).FontSize(12).TextColor(pal.muted).SingleLine().Ellipsis("…")
		})
	})
}

// fileRowMenu 是文稿行的右键菜单。
func (a *App) fileRowMenu(c *ui.Context, row ui.Element, n *vault.Node) {
	svc := c.Services()
	dir := path.Dir(n.Rel)
	if dir == "." {
		dir = ""
	}
	row.PopoverMenu(func(m *ui.Menu) {
		if m.Item(i18n.T("common.open")).Chosen() {
			a.open(n.Rel)
		}
		if m.Item(i18n.T("link.open_default")).Chosen() {
			a.openWithDefault(n.Rel)
		}
		m.Separator()
		if m.Item(i18n.T("command.new_note")).Chosen() {
			a.newNote(dir)
		}
		if m.Item(i18n.T("command.new_board")).Chosen() {
			a.newBoard(dir)
		}
		if m.Item(i18n.T("link.reveal")).Chosen() {
			a.reveal(n.Rel)
		}
		if m.Item(i18n.T("link.copy_path")).Chosen() {
			svc.WriteClipboard(n.Rel)
			a.say(i18n.T("link.copy_path_done"))
		}
		m.Separator()
		if m.Item(i18n.T("common.rename_ellipsis")).Chosen() {
			a.beginRename(n.Rel)
		}
		if m.Item(i18n.T("link.move_to_trash")).Chosen() {
			a.trash(n.Rel)
		}
	})
}

// folderRowMenu 是目录行的右键菜单。
func (a *App) folderRowMenu(c *ui.Context, row ui.Element, n *vault.Node) {
	svc := c.Services()
	row.PopoverMenu(func(m *ui.Menu) {
		if m.Item(i18n.T("command.new_note")).Chosen() {
			a.newNote(n.Rel)
		}
		if m.Item(i18n.T("command.new_board")).Chosen() {
			a.newBoard(n.Rel)
		}
		if m.Item(i18n.T("common.new_folder")).Chosen() {
			a.newFolder(n.Rel)
		}
		m.Separator()
		if m.Item(i18n.T("link.reveal")).Chosen() {
			a.reveal(n.Rel)
		}
		if m.Item(i18n.T("link.copy_path")).Chosen() {
			svc.WriteClipboard(n.Rel)
			a.say(i18n.T("link.copy_path_done"))
		}
		m.Separator()
		if m.Item(i18n.T("common.rename_ellipsis")).Chosen() {
			a.beginRename(n.Rel)
		}
		if m.Item(i18n.T("link.move_to_trash")).Chosen() {
			a.trash(n.Rel)
		}
	})
}

// rotation 返回折叠箭头的旋转角度。
func rotation(collapsed bool) float32 {
	if collapsed {
		return 0
	}
	return 90
}

// fileRow 画一个文稿行。
func (a *App) fileRow(c *ui.Context, pal chrome, n *vault.Node, depth int) {
	active := a.active >= 0 && a.active < len(a.tabs) && a.tabs[a.active].rel == n.Rel
	row := ui.ButtonBase(c).Height(24).Radius(6).Padding(0, 6, 0, 20+float32(depth)*12).
		FillWidth().Cursor(ui.CursorPointer).Label(n.Rel)
	switch {
	case active:
		row.Background(pal.selectColor)
	case row.Hovered():
		row.Background(pal.tabHover)
	}
	row.OnClick(func() { a.open(n.Rel) })
	a.fileRowMenu(c, row, n)
	row.Children(func() {
		ui.Row(c).Grow(1).Gap(5).AlignItems(ui.Center).Justify(ui.Start).Children(func() {
			ui.Icon(c, kindIcon(n.Kind)).Size(13, 13).TextColor(pal.muted)
			ui.Text(c, n.Title()).FontSize(12.5).TextColor(pal.text).SingleLine().Ellipsis("…")
		})
	})
}

// sidebarAction 是侧栏顶部的一个动作：键名进配置，用于排序与显隐。
type sidebarAction struct {
	key, label, shortcut string
	icon                 *ui.SVG
	fn                   func()
}

// sidebarAction 按键名查动作；未知键返回 false（配置里删掉的旧键会被忽略）。
func (a *App) sidebarAction(key string) (sidebarAction, bool) {
	switch key {
	case "new":
		return sidebarAction{"new", i18n.T("command.new_note"), "Ctrl+N", iconPlus,
			func() { a.newNote(a.currentDir()) }}, true
	case "search":
		return sidebarAction{"search", i18n.T("sidebar.action_search"), "Ctrl+K", iconSearch, func() {
			a.paletteOpen = true
			a.paletteQuery = ""
			a.paletteSel = 0
		}}, true
	case "vault":
		return sidebarAction{"vault", i18n.T("sidebar.action_vault"), "Ctrl+O", iconFolderOpen, a.chooseVault}, true
	case "graph":
		return sidebarAction{"graph", i18n.T("command.open_graph"), "", iconGraph, a.openGraph}, true
	}
	return sidebarAction{}, false
}

// sidebarActionKeys 是设置页里列出的全部动作（按默认顺序）。
func sidebarActionKeys() []string { return []string{"new", "search", "vault", "graph"} }

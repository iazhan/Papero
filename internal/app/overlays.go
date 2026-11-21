package app

import (
	"fmt"
	"path"
	"strings"

	"github.com/egoist/mygo/ui"

	"papero/internal/i18n"
	"papero/internal/vault"
)

// overlays 画出浮层：快速跳转、命令面板、发现、设置与提示条。
func (a *App) overlays(c *ui.Context) {
	a.paletteView(c)
	a.commandView(c)
	a.renameView(c)
	a.trashView(c)
	a.discoverView(c)
	a.settingsView(c)
	a.notesView(c)
	a.graphOverlay(c)
	a.toastView(c)
}

// command 是命令面板里的一条命令。
type command struct {
	Title string
	Hint  string
	Run   func()
}

// commands 返回当前可用的命令。
func (a *App) commands() []command {
	return []command{
		{i18n.T("command.new_note"), "Ctrl+N", func() { a.newNote(a.currentDir()) }},
		{i18n.T("command.new_board"), "", func() { a.newBoard(a.currentDir()) }},
		{i18n.T("command.jump"), "Ctrl+K", func() {
			a.paletteOpen = true
			a.paletteQuery = ""
			a.paletteSel = 0
		}},
		{i18n.T("command.new_board"), "", func() { a.newBoard(a.currentDir()) }},
		{i18n.T("command.open_vault"), "Ctrl+O", a.chooseVault},
		{i18n.T("common.save"), "Ctrl+S", func() { a.saveActive(); a.say(i18n.T("common.saved")) }},
		{i18n.T("command.close_tab"), "Ctrl+W", func() { a.closeTab(a.active) }},
		{i18n.T("command.rescan"), "Ctrl+Shift+R", func() { a.refresh(); a.say(i18n.T("command.rescan_done")) }},
		{i18n.T("common.sync_now"), "", func() { a.runSync() }},
		{i18n.T("command.toggle_sidebar"), "Ctrl+\\", func() { a.sidebarCollapsed = !a.sidebarCollapsed }},
		{i18n.T("command.mode_split"), "", func() { a.setMode("split") }},
		{i18n.T("command.mode_edit"), "", func() { a.setMode("blocks") }},
		{i18n.T("command.mode_source"), "", func() { a.setMode("source") }},
		{i18n.T("command.sidebar_docs"), "", func() { a.sidebarTab = 0 }},
		{i18n.T("command.sidebar_outline"), "", func() { a.sidebarTab = 1 }},
		{i18n.T("command.sidebar_links"), "", func() { a.sidebarTab = 2 }},
		{i18n.T("command.toggle_live_layout"), "", func() { a.cfg.LivePreview = !a.cfg.LivePreview; a.cfg.Save() }},
		{i18n.T("command.open_graph"), "", a.openGraph},
		{i18n.T("command.discover"), "", func() { a.discoverOpen = true }},
		{i18n.T("command.settings"), "Ctrl+,", func() { a.openSettings() }},
		{i18n.T("command.shortcuts"), "", func() {
			a.say(i18n.T("command.shortcuts_hint"))
		}},
		{i18n.T("command.about"), "", func() {
			a.settingsPage = "about"
			a.openSettings()
		}},
	}
}

// setMode 切换当前 Markdown 文稿的显示方式。
func (a *App) setMode(mode string) {
	if tab := a.currentTab(); tab != nil && tab.kind == vault.KindMarkdown {
		tab.mode = mode
	}
}

// commandResults 返回匹配查询的命令。
func (a *App) commandResults() []command {
	query := strings.TrimSpace(a.commandQuery)
	var out []command
	for _, cmd := range a.commands() {
		if query == "" || fuzzyMatch(cmd.Title, query) {
			out = append(out, cmd)
		}
	}
	return out
}

// commandView 是 Ctrl+Shift+P 的命令面板。
func (a *App) commandView(c *ui.Context) {
	if !a.commandOpen {
		return
	}
	pal := chromeFor(a.tokens)
	results := a.commandResults()
	if a.commandSel >= len(results) {
		a.commandSel = len(results) - 1
	}
	if a.commandSel < 0 {
		a.commandSel = 0
	}
	ui.Modal(c, &a.commandOpen, func() {
		ui.Column(c).Width(520).Padding(10).Gap(8).Background(pal.card).Radius(14).Children(func() {
			input := ui.TextInput(c, &a.commandQuery).Placeholder(i18n.T("palette.command_placeholder")).Label(i18n.T("palette.command_label")).AutoFocus()
			if input.Submitted() && len(results) > 0 {
				results[a.commandSel].Run()
				a.commandOpen = false
			}
			if input.Changed() {
				a.commandSel = 0
			}
			ui.Scroll(c).MaxHeight(340).Children(func() {
				ui.Column(c).Gap(1).AlignItems(ui.Stretch).Children(func() {
					for i, cmd := range results {
						cmd := cmd
						row := ui.ButtonBase(c).Height(26).Padding(0, 8).Radius(7).
							FillWidth().Cursor(ui.CursorPointer).Label(cmd.Title)
						if i == a.commandSel {
							row.Background(pal.selectColor)
						} else if row.Hovered() {
							row.Background(pal.tabHover)
						}
						row.OnClick(func() {
							cmd.Run()
							a.commandOpen = false
						})
						row.Children(func() {
							ui.Row(c).Grow(1).Gap(8).AlignItems(ui.Center).Justify(ui.Start).Children(func() {
								ui.Text(c, cmd.Title).FontSize(12.5).TextColor(pal.text).Grow(1)
								if cmd.Hint != "" {
									ui.Text(c, cmd.Hint).FontSize(11).TextColor(pal.muted)
								}
							})
						})
					}
					if len(results) == 0 {
						ui.Text(c, i18n.T("palette.command_empty")).FontSize(12).TextColor(pal.muted).Padding(8)
					}
				})
			})
		})
	})
}

// renameView 是重命名对话框（来自目录树或标签的右键菜单）。
func (a *App) renameView(c *ui.Context) {
	if !a.renameOpen {
		return
	}
	pal := chromeFor(a.tokens)
	ui.Modal(c, &a.renameOpen, func() {
		ui.Column(c).Width(420).Padding(16).Gap(10).Background(pal.card).Radius(12).Children(func() {
			ui.Text(c, i18n.T("common.rename")).FontSize(14).Bold().TextColor(pal.text)
			ui.Text(c, a.renameRel).FontSize(11).TextColor(pal.muted).SingleLine().TruncateMiddle()
			input := ui.TextInput(c, &a.renameText).Label(i18n.T("rename.new_name")).AutoFocus().FillWidth()
			if input.Submitted() {
				a.commitRename()
			}
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, i18n.T("common.cancel")).Clicked() {
					a.renameOpen = false
				}
				if ui.PrimaryButton(c, i18n.T("common.rename")).Clicked() {
					a.commitRename()
				}
			})
		})
	})
}

// trashView 是「移到回收站」前的确认：列出会被断链的文稿。
func (a *App) trashView(c *ui.Context) {
	if !a.trashOpen {
		return
	}
	pal := chromeFor(a.tokens)
	back := a.backlinksOf(a.trashRel)
	title := strings.TrimSuffix(path.Base(a.trashRel), ".md")
	ui.Modal(c, &a.trashOpen, func() {
		ui.Column(c).Width(520).Padding(18).Gap(10).Background(pal.card).Radius(14).Children(func() {
			ui.Text(c, i18n.T("trash.title")).FontSize(14).Bold().TextColor(pal.text)
			ui.Text(c, fmt.Sprintf(i18n.T("trash.backlinks_warning"), title, len(back))).
				FontSize(12).TextColor(pal.muted)
			ui.Scroll(c).MaxHeight(200).Children(func() {
				ui.Column(c).Gap(2).AlignItems(ui.Stretch).Children(func() {
					for _, note := range back {
						note := note
						row := ui.ButtonBase(c).Height(30).Radius(6).Padding(2, 8).FillWidth().
							Cursor(ui.CursorPointer).Label(note.Title)
						if row.Hovered() {
							row.Background(pal.tabHover)
						}
						row.OnClick(func() { a.open(note.Rel) })
						row.Children(func() {
							ui.Column(c).Grow(1).Gap(1).Justify(ui.Center).Children(func() {
								ui.Text(c, note.Title).FontSize(12.5).TextColor(pal.text).SingleLine().Ellipsis("…")
								ui.Text(c, note.Rel).FontSize(10.5).TextColor(pal.muted).SingleLine().Ellipsis("…")
							})
						})
					}
				})
			})
			ui.Text(c, i18n.T("trash.broken_links_hint")).
				FontSize(11).TextColor(pal.muted)
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, i18n.T("common.cancel")).Clicked() {
					a.trashOpen = false
				}
				if ui.PrimaryButton(c, i18n.T("trash.confirm")).Clicked() {
					a.doTrash(a.trashRel)
				}
			})
		})
	})
}

// discoverView 是「发现」面板：插件在这里管理，知识图谱与插件市场以后也放这里。
func (a *App) discoverView(c *ui.Context) {
	if !a.discoverOpen {
		return
	}
	pal := chromeFor(a.tokens)
	ui.Modal(c, &a.discoverOpen, func() {
		ui.Column(c).Width(560).MaxHeight(560).Padding(18).Gap(12).Background(pal.card).Radius(14).Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("discover.title")).FontSize(15).Bold().TextColor(pal.text).Grow(1)
				iconButton(c, pal, iconClose, i18n.T("common.close"), func() { a.discoverOpen = false })
			})
			ui.Text(c, i18n.T("discover.hint")).FontSize(11.5).TextColor(pal.muted)
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(10).AlignItems(ui.Stretch).Children(func() {
					// 知识图谱
					nodes, edges := a.graphNodes()
					card := ui.Column(c).Padding(10).Gap(4).Radius(10).Background(pal.window)
					card.Children(func() {
						ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
							ui.Column(c).Gap(2).Grow(1).Children(func() {
								ui.Text(c, i18n.T("discover.graph_title")).FontSize(12.5).TextColor(pal.text)
								ui.Text(c, i18n.T("discover.graph_from")+" · "+joinCounts(
									i18n.Tn(len(nodes), "discover.graph_docs.one", "discover.graph_docs.other", len(nodes)),
									i18n.Tn(len(edges), "discover.graph_links.one", "discover.graph_links.other", len(edges)))).
									FontSize(11).TextColor(pal.muted)
							})
							if ui.Button(c, i18n.T("discover.open_graph")).Clicked() {
								a.discoverOpen = false
								a.openGraph()
							}
						})
					})
					a.settingsPlugins(c)
				})
			})
			ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
				if ui.Button(c, i18n.T("discover.plugin_settings")).Clicked() {
					a.discoverOpen = false
					a.openSettings()
				}
			})
		})
	})
}

// paletteView 是 Ctrl+K 的快速跳转面板。
func (a *App) paletteView(c *ui.Context) {
	if !a.paletteOpen {
		return
	}
	t := c.Theme()
	results := a.paletteResults()
	if a.paletteSel >= len(results) {
		a.paletteSel = len(results) - 1
	}
	if a.paletteSel < 0 {
		a.paletteSel = 0
	}
	ui.Modal(c, &a.paletteOpen, func() {
		ui.Column(c).Width(560).Padding(10).Gap(8).Background(t.Background).Radius(14).Children(func() {
			input := ui.TextInput(c, &a.paletteQuery).Placeholder(i18n.T("palette.placeholder")).Label(i18n.T("palette.title")).AutoFocus()
			if input.Submitted() && len(results) > 0 {
				a.open(results[a.paletteSel].Rel)
				a.paletteOpen = false
			}
			if input.Changed() {
				a.paletteSel = 0
			}
			ui.Scroll(c).MaxHeight(360).Children(func() {
				ui.Column(c).Gap(1).Children(func() {
					for i, f := range results {
						row := ui.ButtonBase(c).Height(28).Padding(0, 8).Radius(7).Cursor(ui.CursorPointer).Label(f.Rel)
						if i == a.paletteSel {
							row.Background(a.tokens.AccentSoft)
						} else if row.Hovered() {
							row.Background(t.SurfaceHover)
						}
						rel := f.Rel
						row.OnClick(func() {
							a.open(rel)
							a.paletteOpen = false
						})
						row.Children(func() {
							ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
								ui.Icon(c, kindIcon(f.Kind)).Size(13, 13)
								ui.Text(c, f.Title()).FontSize(12.5).SingleLine()
								ui.Box(c).Grow(1)
								ui.Text(c, f.Rel).FontSize(11).TextColor(t.TextMuted).SingleLine().Ellipsis("…").MaxWidth(280)
							})
						})
					}
					if len(results) == 0 {
						ui.Text(c, i18n.T("palette.empty")).FontSize(12).TextColor(t.TextMuted).Padding(8)
					}
					// 正文命中（Markdown / 纯文本 / PDF）
					hits := a.contentHits(a.paletteQuery, 8)
					if len(hits) > 0 {
						ui.Text(c, i18n.T("palette.snippet_kind")).FontSize(10.5).Bold().TextColor(t.TextMuted).
							Padding(6, 8)
						for _, h := range hits {
							h := h
							row := ui.ButtonBase(c).Height(40).Padding(4, 8).Radius(7).
								Cursor(ui.CursorPointer).Label(h.Rel + " " + h.where())
							if row.Hovered() {
								row.Background(t.SurfaceHover)
							}
							row.OnClick(func() {
								a.openContentHit(h)
								a.paletteOpen = false
							})
							row.Children(func() {
								ui.Column(c).Gap(1).FillWidth().AlignItems(ui.Start).Children(func() {
									ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
										ui.Icon(c, kindIcon(a.kindOf(h.Rel))).Size(11, 11)
										ui.Text(c, h.Rel+" · "+h.where()).FontSize(12).SingleLine()
									})
									ui.Text(c, h.Snippet).FontSize(11).TextColor(t.TextMuted).
										SingleLine().Ellipsis("…")
								})
							})
						}
					}
				})
			})
		})
	})
}

// section 画一个小节标题。
func section(c *ui.Context, title, hint string) {
	t := c.Theme()
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, title).FontSize(12).Bold()
		if hint != "" {
			ui.Text(c, hint).FontSize(11).TextColor(t.TextMuted)
		}
	})
}

// field 画一行「标签 + 控件」。
// field 是一行设置：左边是名字与说明，右边是控件，靠右对齐。
// 扫一眼左列就知道有哪些项，右手边一排是同类的值。
func (a *App) field(c *ui.Context, label, hint string, control func()) {
	a.fieldWith(c, nil, label, hint, control)
}

// fieldWith 与 field 相同，只是左端多一个前导位（拖动手柄之类）。
// 单独拆一个是因为「手柄 + 名字/说明 + 控件」仍然是同一行的三段式：
// 手柄自己占一列，名字那列照样 Grow，行高与分隔线也一致。
func (a *App) fieldWith(c *ui.Context, lead func(), label, hint string, control func()) {
	t := c.Theme()
	row := ui.Row(c).FillWidth().Height(52).PaddingStart(12).PaddingEnd(12).Gap(12).AlignItems(ui.Center)
	if a.groupRows > 0 {
		// 卡片里除第一行外都画一条分隔线
		row.BorderWidth(1, 0, 0, 0).BorderColor(t.Border)
	}
	a.groupRows++
	row.Children(func() {
		if lead != nil {
			lead()
		}
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, label).FontSize(12.5).TextColor(t.Text).SingleLine()
			if hint != "" {
				ui.Text(c, hint).FontSize(10.5).TextColor(t.TextMuted).
					SingleLine().Ellipsis("…")
			}
		})
		control()
		// 末尾留白：Row 的 Padding 结束侧在这版框架里不生效，
		// 直接放一个定宽盒子把控件顶离卡片边缘
		ui.Box(c).Width(12).Shrink(0)
	})
}

// fields 把若干 field 包成一张卡片：圆角、细边、行间一条分隔线。
func (a *App) fields(c *ui.Context, build func()) {
	a.groupRows = 0
	ui.Column(c).FillWidth().Radius(10).Border(1, c.Theme().Border).
		Clip().Children(build)
}

// settingsGeneral 是外观与阅读设置。

// settingsSync 是同步设置。
func (a *App) settingsSync(c *ui.Context) {
	// 这里不再画小节标题：页头已经是「同步 + 一句说明」，再写一遍就成了
	// 两个一模一样的「同步」上下叠着。原来那句「冲突会保留两份」并进页头的说明。
	// 整页一张卡片：同步的开关、远端、远端参数、忽略规则都是「同一个东西」的设置，
	// 拆成几张卡反而要再给每张写个小标题，行还是那几行。
	a.fields(c, func() {
		a.field(c, i18n.T("settings.sync.enabled"), "", func() {
			enabled := a.cfg.Sync.Enabled
			ui.Switch(c, &enabled).Label(i18n.T("settings.sync.enabled")).Changed()
			if enabled != a.cfg.Sync.Enabled {
				a.cfg.Sync.Enabled = enabled
				a.commit()
			}
		})
		// 多台设备编同一个库的人：一打开就先拿到别人的改动
		a.field(c, i18n.T("settings.sync.pull_on_open"), i18n.T("settings.sync.pull_on_open_enable"), func() {
			pull := a.cfg.Sync.PullOnOpen
			ui.Switch(c, &pull).Label(i18n.T("settings.sync.pull_on_open")).Changed()
			if pull != a.cfg.Sync.PullOnOpen {
				a.cfg.Sync.PullOnOpen = pull
				a.commit()
			}
		})
		a.field(c, i18n.T("settings.sync.remote"), "", func() {
			// Select 只认字符串，所以显示名与配置值分两张表，按位置对应：
			// 不把显示文本当状态比（英文界面下「文件夹」变成 Folder，比不了）。
			kinds := []string{"dir", "git", "repo", "webdav"}
			labels := []string{
				i18n.T("settings.sync.kind_dir"),
				i18n.T("settings.sync.kind_git"),
				i18n.T("settings.sync.kind_repo"),
				i18n.T("settings.sync.kind_webdav"),
			}
			idx := 0
			for i, k := range kinds {
				if k == a.cfg.Sync.Remote {
					idx = i
				}
			}
			label := labels[idx]
			ui.Select(c, &label, labels).Width(160).Changed()
			for i, l := range labels {
				if l == label && kinds[i] != a.cfg.Sync.Remote {
					a.cfg.Sync.Remote = kinds[i]
					a.commit()
				}
			}
		})
		switch a.cfg.Sync.Remote {
		case "git":
			a.field(c, i18n.T("settings.sync.repo"), "", func() {
				ui.TextInput(c, &a.cfg.Sync.Repo).Placeholder("owner/name").Width(280).Label(i18n.T("settings.sync.repo")).Changed()
			})
			a.field(c, i18n.T("settings.sync.branch"), "", func() {
				ui.TextInput(c, &a.cfg.Sync.Branch).Placeholder("main").Width(160).Label(i18n.T("settings.sync.branch")).Changed()
			})
			a.field(c, i18n.T("settings.sync.token"), "", func() {
				ui.TextInput(c, &a.cfg.Sync.Token).Password().Width(280).Label(i18n.T("settings.sync.github_token")).Changed()
			})
		case "repo":
			// 什么都不用填：仓库从 origin 读，分支从 HEAD 读，凭据用系统里的
			a.field(c, i18n.T("settings.sync.repo_source"), i18n.T("settings.sync.repo_hint"), func() {
				ui.Box(c).Width(1)
			})
		case "webdav":
			a.field(c, i18n.T("settings.sync.address"), "", func() {
				ui.TextInput(c, &a.cfg.Sync.URL).Placeholder("https://dav.jianguoyun.com/dav/Papero").Width(320).Label(i18n.T("settings.sync.address")).Changed()
			})
			a.field(c, i18n.T("settings.sync.user"), "", func() {
				ui.TextInput(c, &a.cfg.Sync.User).Placeholder("me@example.com").Width(280).Label(i18n.T("settings.sync.user")).Changed()
			})
			a.field(c, i18n.T("settings.sync.password"), "", func() {
				ui.TextInput(c, &a.cfg.Sync.Password).Password().Width(280).Label(i18n.T("settings.sync.password")).Changed()
			})
		default:
			a.field(c, i18n.T("settings.sync.folder"), "", func() {
				ui.TextInput(c, &a.cfg.Sync.Dir).Placeholder("D:/Papero-sync").Width(320).Label(i18n.T("settings.sync.folder_label")).Changed()
			})
		}
		// 哪些路径不进同步：工具目录（.obsidian 这类）常常不需要跟着走
		a.field(c, i18n.T("settings.sync.ignore_dot"), i18n.T("settings.sync.ignore_dot_enable"), func() {
			skip := a.cfg.Sync.IgnoreDot
			ui.Switch(c, &skip).Label(i18n.T("settings.sync.ignore_dot")).Changed()
			if skip != a.cfg.Sync.IgnoreDot {
				a.cfg.Sync.IgnoreDot = skip
				a.commit()
			}
		})
		a.field(c, i18n.T("settings.sync.ignore_gitignore"), i18n.T("settings.sync.ignore_gitignore_enable"), func() {
			skip := a.cfg.Sync.IgnoreGitignore
			ui.Switch(c, &skip).Label(i18n.T("settings.sync.ignore_gitignore")).Changed()
			if skip != a.cfg.Sync.IgnoreGitignore {
				a.cfg.Sync.IgnoreGitignore = skip
				a.commit()
			}
		})
	})
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		if ui.Button(c, i18n.T("common.sync_now")).Clicked() {
			a.commit()
			a.runSync()
		}
		// 只往下拿：多台设备编同一个库时，先看看别人改了什么
		if ui.Button(c, i18n.T("settings.sync.pull")).Clicked() {
			a.commit()
			a.pullCloud(true)
		}
		// 先试连接再同步：地址或密码错了，这一步就能说清，不用等整轮同步
		if ui.Button(c, i18n.T("settings.sync.test")).Clicked() {
			a.commit()
			a.testConnection()
		}
	})
	ui.Text(c, a.syncMsg).FontSize(11).TextColor(c.Theme().TextMuted).SingleLine().Ellipsis("…").MaxWidth(520)
}

// settingsPlugins 列出已安装的插件与它们的开关。

package app

import (
	"fmt"
	"math"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"papero/internal/i18n"
	"papero/internal/vault"
	"papero/internal/wiki"
)

// content 画出卡片里的正文：按文稿类型分派给对应的查看器。
func (a *App) content(c *ui.Context) {
	pal := chromeFor(a.tokens)
	ui.Box(c).Grow(1).MinWidth(0).Children(func() {
		if len(a.tabs) == 0 || a.active < 0 || a.active >= len(a.tabs) {
			a.emptyState(c, pal)
			return
		}
		tab := a.tabs[a.active]
		switch tab.kind {
		case vault.KindMarkdown:
			a.markdownTab(c, tab)
		case vault.KindText:
			a.sourceEditor(c, tab)
		case vault.KindImage:
			a.imageTab(c, pal, tab)
		case vault.KindWord:
			a.wordTab(c, tab)
		case vault.KindPDF:
			a.pdfTab(c, pal, tab)
		case vault.KindBoard:
			a.boardTab(c, pal, tab)
		default:
			a.unsupportedTab(c, pal, tab)
		}
	})
}

// emptyState 是起始页：没有打开文稿时，直接给出可以动手的入口。
//
// 不是一句「从左侧选择」就完事——三个最常用的动作摆出来，下面接「最近打开」，
// 让空窗口也有事可做。
func (a *App) emptyState(c *ui.Context, pal chrome) {
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.Center).Padding(48, 24).Children(func() {
			ui.Column(c).Width(460).Gap(6).Children(func() {
				ui.Text(c, "Papero").FontSize(24).Bold().TextColor(pal.text)
				ui.Text(c, i18n.T("start.tagline")).FontSize(12.5).TextColor(pal.muted)

				ui.Box(c).Height(18)
				ui.Row(c).Gap(10).Children(func() {
					a.startAction(c, pal, iconPlus, i18n.T("command.new_note"), "Ctrl+N", func() {
						a.newNote(a.currentDir())
					})
					a.startAction(c, pal, iconSearch, i18n.T("sidebar.action_search"), "Ctrl+K", func() {
						a.paletteOpen = true
						a.paletteQuery = ""
						a.paletteSel = 0
					})
					a.startAction(c, pal, iconFolderOpen, i18n.T("sidebar.action_vault"), "Ctrl+O", a.chooseVault)
				})

				ui.Box(c).Height(20)
				if len(a.recent) > 0 {
					ui.Text(c, i18n.T("common.recent")).FontSize(11).TextColor(pal.muted).Padding(0, 2)
					for i, rel := range a.recent {
						if i >= 6 {
							break
						}
						rel := rel
						row := ui.ButtonBase(c).Height(32).Radius(7).Padding(0, 8).FillWidth().
							Cursor(ui.CursorPointer).Label(i18n.T("start.recent_prefix") + rel)
						if row.Hovered() {
							row.Background(pal.tabHover)
						}
						row.OnClick(func() { a.open(rel) })
						row.Children(func() {
							ui.Row(c).Grow(1).Gap(9).AlignItems(ui.Center).Children(func() {
								ui.Icon(c, iconFile).Size(13, 13).TextColor(pal.muted)
								ui.Text(c, noteTitle(rel)).FontSize(12.5).TextColor(pal.text).
									SingleLine().MaxWidth(240)
								ui.Box(c).Grow(1)
								ui.Text(c, rel).FontSize(10.5).TextColor(pal.muted).
									SingleLine().Ellipsis("…").MaxWidth(160)
							})
						})
					}
				} else {
					ui.Text(c, i18n.T("start.empty")).
						FontSize(11.5).TextColor(pal.muted)
				}

				ui.Box(c).Height(14)
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Text(c, a.vault.Root()).FontSize(10.5).TextColor(pal.muted).
						SingleLine().TruncateMiddle().MaxWidth(300)
					ui.Box(c).Grow(1)
					ui.Text(c, fmt.Sprintf(i18n.T("start.doc_count"), a.docCount())).
						FontSize(10.5).TextColor(pal.muted)
				})
			})
		})
	})
}

// startAction 是起始页上的一个动作卡片：图标 + 名字 + 快捷键。
func (a *App) startAction(c *ui.Context, pal chrome, icon *ui.SVG, label, shortcut string, fn func()) {
	card := ui.ButtonBase(c).Width(146).Height(64).Radius(10).Padding(0, 12).
		Cursor(ui.CursorPointer).Label(label)
	card.Background(pal.window)
	if card.Hovered() {
		card.Background(pal.tabHover)
	}
	card.OnClick(fn)
	card.Children(func() {
		ui.Column(c).Grow(1).Gap(3).Children(func() {
			ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Icon(c, icon).Size(14, 14).TextColor(pal.accent)
				ui.Text(c, label).FontSize(12.5).TextColor(pal.text)
			})
			ui.Text(c, shortcut).FontSize(10.5).TextColor(pal.muted)
		})
	})
}

// noteTitle 把相对路径变成好看的标题。
func noteTitle(rel string) string {
	name := rel
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		name = name[i+1:]
	}
	name = strings.TrimSuffix(name, ".md")
	name = strings.TrimSuffix(name, ".markdown")
	return name
}

// markdownTab 画出 Markdown 文稿：编辑、分栏或源码。
// 显示方式的切换在状态栏右侧，正文区只放文稿本身。
func (a *App) markdownTab(c *ui.Context, tab *tab) {
	switch tab.mode {
	case "preview":
		// 旧配置里的「预览」并进编辑模式
		a.editorTab(c, tab)
	case "source":
		a.sourceEditor(c, tab)
	case "blocks":
		a.editorTab(c, tab)
	default:
		ui.SplitPercent(c, &a.splitPct, func() { a.sourceEditor(c, tab) }, func() { a.markdownPreview(c, tab) }).Grow(1)
	}
}

// sourceEditor 画出源码编辑器。
func (a *App) sourceEditor(c *ui.Context, tab *tab) {
	if tab.state == nil {
		tab.state = &editor.State{
			Buffer:     tab.buf,
			SoftWrap:   true,
			LineHeight: 1.6,
			Font:       ui.Font{Family: fontMono, Size: 13},
		}
	}
	// 实时排版：把 Markdown 标记藏起来，标题行放大
	a.livePreview(tab)
	// 输入 [[ 时列出库里的文稿
	a.updateWikiCompletions(tab)
	ui.Box(c).Grow(1).MinWidth(0).Children(func() {
		editor.View(c, tab.state).Grow(1).FillWidth()
	})
}

// markdownPreview 画出渲染后的 Markdown。
func (a *App) markdownPreview(c *ui.Context, tab *tab) {
	tab.md.OnLink = a.linkHandler(c, tab)
	tab.md.LinkMenu = a.linkMenu(tab)
	ui.Scroll(c).Grow(1).MinWidth(0).Children(func() {
		ui.Row(c).Justify(ui.Center).Padding(32, 28).Children(func() {
			ui.Column(c).Width(float32(a.cfg.ReadWidth)).MinWidth(0).Children(func() {
				// 双链先改写成普通链接，交给 ui/markdown 渲染，点击再回到 openWiki。
				src := tab.text()
				if strings.Contains(src, "[[") {
					src = wiki.Rewrite(src, wikiDest)
				}
				markdown.View(c, src, &tab.md)
			})
		})
	})
}

// imageTab 画出图片查看器。
func (a *App) imageTab(c *ui.Context, pal chrome, tab *tab) {
	ui.Column(c).Fill().Children(func() {
		// 图片还没量到可用区域时先按窗口估一个，下一帧用实测值纠正
		fit := tab.imgZoom
		if tab.img != nil {
			w, h := tab.img.Size()
			fit = fitZoom(float32(w), float32(h), tab.imgStage.W, tab.imgStage.H)
		}
		isFit := tab.img != nil && math.Abs(float64(tab.imgZoom-fit)) < 0.005
		isActual := math.Abs(float64(tab.imgZoom)-1) < 0.005

		toolbarBar(c, pal, func() {
			// 左边：视图动作（按下就把倍率改成对应的值，所以输入框里永远是真实百分比）
			toolButton(c, pal, iconFitWindow, i18n.T("viewer.fit_window"), i18n.T("viewer.fit_image_tooltip"), isFit,
				func() { tab.imgZoom = fit })
			toolTextButton(c, pal, "1:1", i18n.T("viewer.actual_size"), isActual, func() { tab.imgZoom = 1 })
			toolSep(c, pal)
			// 中间：缩放（百分比可以直接敲）
			tab.zoomField.sync(zoomPercentText(tab.imgZoom))
			zoomStepper(c, pal, &tab.zoomField,
				func() { tab.imgZoom = stepZoomValue(tab.imgZoom, -1) },
				func() { tab.imgZoom = stepZoomValue(tab.imgZoom, 1) },
				func(text string) {
					if z, ok := parseZoom(text); ok {
						tab.imgZoom = z
					}
				})
			ui.Box(c).Grow(1)
			// 右边：尺寸与文件大小
			if tab.img != nil {
				w, h := tab.img.Size()
				shownW := int(math.Round(float64(float32(w) * tab.imgZoom)))
				shownH := int(math.Round(float64(float32(h) * tab.imgZoom)))
				toolInfo(c, pal, fmt.Sprintf("%d × %d · %s", shownW, shownH, humanBytes(tab.size)), 260)
			}
		})
		if tab.imgErr != "" {
			ui.Column(c).Fill().Center().Children(func() {
				ui.Text(c, tab.imgErr).FontSize(12).TextColor(c.Theme().Danger)
			})
			return
		}
		if tab.img == nil {
			return
		}
		scroll := ui.ScrollBoth(c).Grow(1).Background(pal.card).Children(func() {
			ui.Box(c).Fill().Padding(20).Center().Children(func() {
				w, h := tab.img.Size()
				ui.Image(c, tab.img).
					Size(float32(w)*tab.imgZoom, float32(h)*tab.imgZoom).
					Fit(ui.FillBox).Radius(4)
			})
		})
		// 记下可用区域，供「适应窗口」与下一帧的比例计算
		if r := scroll.Bounds(); r.W > 0 && r.H > 0 {
			tab.imgStage = ui.Rect{W: r.W - 40, H: r.H - 40}
			if tab.imgZoom <= 0 {
				w, h := tab.img.Size()
				tab.imgZoom = fitZoom(float32(w), float32(h), tab.imgStage.W, tab.imgStage.H)
			}
		}
	})
}

// wordTab 画出 Word 文稿：.docx 读成 Markdown 后按排版显示。
func (a *App) wordTab(c *ui.Context, tab *tab) {
	if tab.docxErr != "" {
		ui.Column(c).Fill().Center().Padding(40).Children(func() {
			ui.Text(c, tab.docxErr).FontSize(12).TextColor(c.Theme().Danger)
		})
		return
	}
	ui.Scroll(c).Grow(1).Children(func() {
		ui.Row(c).Justify(ui.Center).Padding(32, 28).Children(func() {
			ui.Column(c).Width(float32(a.cfg.ReadWidth)).MinWidth(0).Children(func() {
				markdown.View(c, tab.docxMD, &tab.md)
			})
		})
	})
}

// unsupportedTab 显示尚未支持的文件类型。
func (a *App) unsupportedTab(c *ui.Context, pal chrome, tab *tab) {
	ui.Column(c).Fill().Center().Gap(8).Children(func() {
		ui.Icon(c, kindIcon(tab.kind)).Size(28, 28).TextColor(pal.muted)
		ui.Text(c, fmt.Sprintf(i18n.T("start.unsupported"), tab.kind.Label())).FontSize(14).TextColor(pal.text)
		ui.Text(c, tab.rel).FontSize(11).TextColor(pal.muted).SingleLine().TruncateMiddle().MaxWidth(480)
		ui.Text(c, i18n.T("start.unsupported_hint")).FontSize(11).TextColor(pal.muted)
	})
}

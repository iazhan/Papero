package app

import (
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/components/graph"

	"papero/internal/i18n"
)

// 知识图谱用 Onevra 的关系图谱组件绘制：布局、镜头、悬停高亮、滚轮缩放
// 都由它负责，Papero 只提供数据（节点、分组、边）与「打开节点」的动作。

// graphData 把链接图变成 Onevra 需要的数据。
//
// 分组用文稿所在的顶层目录：同一个目录的笔记会聚在一起，并按组分配色相，
// 一眼能看出「这一簇是哪个主题的笔记」。节点大小取链接数（出链 + 反向链接）。
func (a *App) graphData() graph.Data {
	nodes, _ := a.graphNodes()
	var data graph.Data
	seen := map[string]bool{}
	for _, note := range nodes {
		group := ""
		if i := strings.IndexByte(note.Rel, '/'); i > 0 {
			group = note.Rel[:i]
		}
		data.Nodes = append(data.Nodes, graph.Node{
			ID:       note.Rel,
			Label:    note.Title,
			Group:    group,
			Weight:   float64(len(note.Links) + len(note.Back)),
			Openable: true,
		})
		seen[note.Rel] = true
	}
	for _, note := range nodes {
		for _, to := range note.Links {
			if !seen[to] {
				continue
			}
			data.Edges = append(data.Edges, graph.Edge{Source: note.Rel, Target: to})
		}
	}
	return data
}

// graphPalette 让图谱的底色跟着窗口走。
func (a *App) graphPalette(pal chrome, dark bool) *graph.Palette {
	p := graph.DefaultPalette(dark)
	p.Background = pal.window
	return &p
}

// openGraph 按设置打开知识图谱：嵌在主窗口的浮层里，或另开一个窗口。
func (a *App) openGraph() {
	if a.cfg.GraphMode == "window" && a.win != nil {
		a.openGraphWindow()
		return
	}
	a.graphOpen = true
}

// openGraphWindow 打开独立的图谱窗口。
func (a *App) openGraphWindow() {
	if a.graphWin != nil {
		a.graphWin.Focus()
		a.graphWin.Invalidate()
		return
	}
	win := mygo.NewWindow(mygo.WindowOptions{
		Title:    i18n.T("graph.window_title"),
		Width:    960,
		Height:   720,
		MinWidth: 520, MinHeight: 380,
		StateKey: "papero-graph",
		Content:  ui.View(a.graphWindowView),
	})
	a.graphWin = win
	win.OnClosed(func() { a.graphWin = nil })
}

// closeGraph 关掉当前打开的图谱（浮层或窗口）。
func (a *App) closeGraph() {
	a.graphOpen = false
	if a.graphWin != nil {
		a.graphWin.Close()
		a.graphWin = nil
	}
}

// graphWindowView 是独立图谱窗口的内容。
func (a *App) graphWindowView(c *ui.Context) {
	c.SetTheme(paperoTheme(a.tokens))
	pal := chromeFor(a.tokens)
	c.Root().Background(pal.card)
	ui.Column(c).Fill().Children(func() {
		a.graphBody(c, pal, false)
	})
}

// graphOverlay 把知识图谱画在主窗口的浮层里。
func (a *App) graphOverlay(c *ui.Context) {
	if !a.graphOpen || a.cfg.GraphMode == "window" {
		return
	}
	pal := chromeFor(a.tokens)
	ui.Overlay(c, func() {
		// 压暗背后的窗口，让图谱像是浮在上面
		ui.Column(c).Fill().Padding(28).Background(ui.Hex("#00000040")).Children(func() {
			panel := ui.Column(c).Fill().Radius(14).Background(pal.card).
				Shadow(0, 6, 30, 0, ui.Hex("#00000022"))
			panel.Children(func() {
				a.graphBody(c, pal, true)
			})
		})
	})
}

// graphBody 是图谱的主体：一行工具条加图谱画布。浮层与独立窗口共用。
func (a *App) graphBody(c *ui.Context, pal chrome, embedded bool) {
	data := a.graphData()
	ui.Row(c).Height(40).Padding(0, 12).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, i18n.T("graph.title")).FontSize(13).Bold().TextColor(pal.text)
		ui.Text(c, joinCounts(
			i18n.Tn(len(data.Nodes), "graph.stats.docs.one", "graph.stats.docs.other", len(data.Nodes)),
			i18n.Tn(len(data.Edges), "graph.stats.links.one", "graph.stats.links.other", len(data.Edges)))).
			FontSize(11).TextColor(pal.muted)
		ui.Text(c, i18n.T("graph.hint")).FontSize(11).TextColor(pal.muted).Grow(1)
		textButton(c, pal, i18n.T("graph.fit"), i18n.T("graph.fit_tooltip"), func() { a.graphState.Fit() })
		textButton(c, pal, i18n.T("graph.relayout"), i18n.T("graph.relayout_tooltip"), func() {
			a.graphState = graph.State{}
			a.graphState.Fit()
		})
		if embedded {
			iconButton(c, pal, iconClose, i18n.T("graph.close"), a.closeGraph)
		}
	})
	graph.View(c, &a.graphState, data, graph.Options{
		Label:     i18n.T("graph.title"),
		Palette:   a.graphPalette(pal, c.Theme().Dark),
		EmptyText: i18n.T("graph.empty"),
		Open: func(id string) {
			a.open(id)
			if embedded {
				// 浮层模式点完就收起；独立窗口继续留着
				a.graphOpen = false
			}
		},
	}).Grow(1).MinWidth(0)
}

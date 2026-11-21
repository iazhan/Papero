package app

import (
	"fmt"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"

	"papero/internal/i18n"
	"papero/internal/richdoc"
	"papero/internal/vault"
)

// heading 是 Markdown 里的一个标题。
type heading struct {
	Level int
	Text  string
	Line  int // 从 1 开始
	Idx   int // 是第几个标题（0 起），用来对上编辑器里的块
}

// outlineOf 从 Markdown 原文里取出标题，跳过代码围栏里的 # 号。
func outlineOf(src string) []heading {
	var out []heading
	inFence := false
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence || !strings.HasPrefix(trimmed, "#") {
			continue
		}
		n := 0
		for n < len(trimmed) && trimmed[n] == '#' {
			n++
		}
		if n < 1 || n > 6 || len(trimmed) <= n || trimmed[n] != ' ' {
			continue
		}
		text := strings.TrimSpace(trimmed[n:])
		// ATX 标题可以以任意个 # 收尾，去掉它们
		text = strings.TrimRight(text, "#")
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		out = append(out, heading{Level: n, Text: text, Line: i + 1, Idx: len(out)})
	}
	return out
}

// outlineRows 画出当前 Markdown 文稿的大纲。点击标题跳到对应位置。
func (a *App) outlineRows(c *ui.Context, pal chrome) {
	if a.active < 0 || a.active >= len(a.tabs) {
		ui.Text(c, i18n.T("tab.empty")).FontSize(11).TextColor(pal.muted).Padding(6)
		return
	}
	tab := a.tabs[a.active]
	if tab.kind != vault.KindMarkdown || tab.buf == nil {
		ui.Text(c, i18n.T("outline.md_only")).FontSize(11).TextColor(pal.muted).Padding(6)
		return
	}
	heads := outlineOf(tab.text())
	if len(heads) == 0 {
		ui.Text(c, i18n.T("outline.empty")).FontSize(11).TextColor(pal.muted).Padding(6)
		return
	}
	// 顶部一行：折叠全部 / 展开全部
	ui.Row(c).Gap(4).Padding(0, 6).Children(func() {
		toggleAll(c, pal, i18n.T("outline.collapse"), func() { a.setOutlineAll(tab, heads, true) })
		toggleAll(c, pal, i18n.T("outline.expand"), func() { a.setOutlineAll(tab, heads, false) })
	})
	a.outlineNodes(c, pal, tab, outlineTree(heads), 0)
}

// toggleAll 是大纲顶部的小按钮。
func toggleAll(c *ui.Context, pal chrome, label string, fn func()) {
	b := ui.ButtonBase(c).Height(20).Padding(1, 7).Radius(5).
		Cursor(ui.CursorPointer).Label(label)
	b.TextColor(pal.muted)
	if b.Hovered() {
		b.Background(pal.tabHover)
	}
	b.OnClick(fn)
	b.Children(func() { ui.Text(c, label).FontSize(10.5).TextColor(pal.muted) })
}

// outlineNode 是大纲树上的一个标题。
type outlineNode struct {
	h    heading
	path string // 由各级下标拼成，用来记住折叠状态
	kids []*outlineNode
}

// outlineTree 把平铺的标题按级别搭成树：比前一个级别深的挂到它下面。
func outlineTree(heads []heading) []*outlineNode {
	var roots []*outlineNode
	type frame struct {
		node   *outlineNode
		level  int
		prefix string
	}
	var stack []frame
	for _, h := range heads {
		for len(stack) > 0 && stack[len(stack)-1].level >= h.Level {
			stack = stack[:len(stack)-1]
		}
		path := fmt.Sprint(len(roots))
		if len(stack) > 0 {
			parent := stack[len(stack)-1]
			path = parent.prefix + "." + fmt.Sprint(len(parent.node.kids))
		}
		n := &outlineNode{h: h, path: path}
		if len(stack) == 0 {
			roots = append(roots, n)
		} else {
			p := stack[len(stack)-1].node
			p.kids = append(p.kids, n)
		}
		stack = append(stack, frame{node: n, level: h.Level, prefix: path})
	}
	return roots
}

// outlineKey 是折叠状态的键：同一份文稿 + 同一条路径。
func outlineKey(tab *tab, path string) string { return tab.rel + "|" + path }

// setOutlineAll 折叠或展开全部有子标题的项。
func (a *App) setOutlineAll(tab *tab, heads []heading, collapsed bool) {
	if a.outlineCollapsed == nil {
		a.outlineCollapsed = map[string]bool{}
	}
	var walk func(nodes []*outlineNode)
	walk = func(nodes []*outlineNode) {
		for _, n := range nodes {
			if len(n.kids) > 0 {
				a.outlineCollapsed[outlineKey(tab, n.path)] = collapsed
			}
			walk(n.kids)
		}
	}
	walk(outlineTree(heads))
}

// outlineNodes 递归画一层标题。
func (a *App) outlineNodes(c *ui.Context, pal chrome, tab *tab, nodes []*outlineNode, depth int) {
	for _, n := range nodes {
		n := n
		hasKids := len(n.kids) > 0
		collapsed := hasKids && a.outlineCollapsed[outlineKey(tab, n.path)]

		row := ui.ButtonBase(c).Height(24).Radius(6).
			Padding(0, 6, 0, 4+float32(depth)*12).
			FillWidth().Cursor(ui.CursorPointer).Label(i18n.T("outline.jump_prefix") + n.h.Text)
		if row.Hovered() {
			row.Background(pal.tabHover)
		}
		row.OnClick(func() {
			// 点最左边那一小条是折叠开关，其余地方跳转
			if x, _, ok := row.PointerPosition(); ok && hasKids && x < 18 {
				a.outlineCollapsed[outlineKey(tab, n.path)] = !collapsed
				return
			}
			a.jumpToHeading(tab, n.h)
		})
		row.Children(func() {
			ui.Row(c).Grow(1).Gap(4).AlignItems(ui.Center).Justify(ui.Start).Children(func() {
				if hasKids {
					ui.Icon(c, iconChevron).Size(10, 10).Rotate(rotation(collapsed)).
						TextColor(pal.muted)
				} else {
					ui.Box(c).Width(10)
				}
				text := ui.Text(c, n.h.Text).FontSize(12).
					TextColor(pal.text).SingleLine().Ellipsis("…")
				if n.h.Level <= 2 {
					text.Bold()
				} else {
					text.TextColor(pal.muted)
				}
			})
		})
		if !collapsed {
			a.outlineNodes(c, pal, tab, n.kids, depth+1)
		}
	}
}

// jumpToHeading 跳到某个标题：编辑模式下滚到对应的块，分栏模式下用预览的锚点。
// 源码模式没有渲染层，先切到分栏，否则跳了也看不见。
func (a *App) jumpToHeading(tab *tab, h heading) {
	if tab.mode == "source" {
		tab.mode = "split"
	}
	if tab.mode == "blocks" && tab.edit != nil && tab.rich != nil {
		if id := headingBlockID(tab.rich, h.Idx); id != "" {
			tab.edit.ScrollToBlock(id)
			return
		}
	}
	tab.md.Anchor = markdown.Slug(h.Text)
	tab.md.AnchorVersion++
}

// headingBlockID 把「第 idx 个标题」对到编辑器里的块：两边都是文档顺序，
// 而 markdown 的标题不会出现在代码围栏里，所以按序对得上。
func headingBlockID(doc *richdoc.Document, idx int) string {
	n := 0
	for _, b := range doc.Blocks {
		if b.Kind != "heading" {
			continue
		}
		if n == idx {
			return b.ID
		}
		n++
	}
	return ""
}

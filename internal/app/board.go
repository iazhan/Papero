package app

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"papero/internal/board"
	"papero/internal/i18n"
)

// boardTab 画出白板。白板是内置插件 dev.papero.board 贡献的查看器：
// 用 MyGo 的 Painter 原生绘制 Excalidraw 元素，不加载任何网页。
func (a *App) boardTab(c *ui.Context, pal chrome, tab *tab) {
	t := c.Theme()
	if tab.board == nil {
		ui.Column(c).Fill().Center().Children(func() {
			ui.Text(c, i18n.T("board.error.open")).FontSize(13).TextColor(t.Danger)
		})
		return
	}
	if tab.boardTool == "" {
		tab.boardTool = "select"
	}
	if tab.boardZoom == 0 {
		tab.boardZoom = 1
	}

	ui.Column(c).Fill().Children(func() {
		// 工具栏
		ui.Row(c).Height(36).Padding(0, 10).Gap(4).AlignItems(ui.Center).Children(func() {
			for _, tool := range []struct {
				key, label string
				icon       *ui.SVG
			}{
				{"select", i18n.T("board.tool.select"), iconCursor}, {"rect", i18n.T("board.tool.rect"), iconSquare},
				{"ellipse", i18n.T("board.tool.ellipse"), iconCircle}, {"diamond", i18n.T("board.tool.diamond"), iconDiamond},
				{"arrow", i18n.T("board.tool.arrow"), iconArrow}, {"line", i18n.T("board.tool.line"), iconLine},
				{"pen", i18n.T("board.tool.pen"), iconPen}, {"text", i18n.T("board.tool.text"), iconTextTool},
			} {
				key, label, icon := tool.key, tool.label, tool.icon
				on := tab.boardTool == key
				b := ui.ButtonBase(c).Size(24, 24).Radius(4).Cursor(ui.CursorPointer).
					Label(label).Tooltip(label)
				if on {
					b.Background(t.SurfaceHover)
				}
				b.OnClick(func() { tab.boardTool = key })
				b.Children(func() {
					color := t.TextMuted
					if on {
						color = t.Accent
					} else if b.Hovered() {
						color = t.Text
					}
					ui.Icon(c, icon).Size(14, 14).TextColor(color)
				})
			}
			ui.Box(c).Width(1).Height(18).Background(t.Border)

			ui.Box(c).Grow(1)
			// 选中文字元素时可以直接改文字
			if el := a.boardPrimary(tab); el != nil && el.Type == "text" {
				input := ui.TextArea(c, &el.Text).Width(200).Height(56).Label(i18n.T("board.tool.text"))
				if input.Changed() {
					if el.ContainerID != "" {
						if shape := tab.board.Find(el.ContainerID); shape != nil {
							tab.board.CenterContainerText(shape)
						}
					}
					tab.boardDirty = true
				}
				if c.Shortcut(ui.Cmd, ui.KeyS) {
					a.saveBoard(tab)
				}
			}
			toolButton(c, pal, iconUndo, i18n.T("board.tool.undo"), i18n.T("board.tool.undo_tooltip"), false,
				func() { a.boardUndoStep(tab) })
			toolButton(c, pal, iconRedo, i18n.T("board.tool.redo"), i18n.T("board.tool.redo_tooltip"), false,
				func() { a.boardRedoStep(tab) })
			toolButton(c, pal, iconImage, i18n.T("board.tool.image"), i18n.T("board.tool.image"), false,
				func() { a.insertBoardImage(tab) })
			toolButton(c, pal, iconSave, i18n.T("common.save"), i18n.T("board.tool.save_tooltip"), tab.boardDirty,
				func() { a.saveBoard(tab) })
			toolButton(c, pal, iconExport, i18n.T("common.export"), i18n.T("board.tool.export_tooltip"), false,
				func() { a.exportBoard(tab) })
			ui.Box(c).Width(6)
			ui.Text(c, fmt.Sprintf("%.0f%%", tab.boardZoom*100)).FontSize(11).
				TextColor(t.TextMuted).Width(38).TextAlign(ui.Center)
			toolButton(c, pal, iconMinus, i18n.T("viewer.zoom.out"), i18n.T("viewer.zoom.out"), false,
				func() { tab.boardZoom = clampFloat(tab.boardZoom/1.2, 0.2, 4) })
			toolButton(c, pal, iconPlus, i18n.T("viewer.zoom.in"), i18n.T("viewer.zoom.in"), false,
				func() { tab.boardZoom = clampFloat(tab.boardZoom*1.2, 0.2, 4) })
			toolButton(c, pal, iconFit, i18n.T("viewer.zoom.fit"), i18n.T("board.fit_tooltip"), false,
				func() { a.fitBoard(tab) })
		})

		// 选中元素时多一行样式：描边、填充、线宽、字号
		if el := a.boardPrimary(tab); el != nil {
			a.boardStyleRow(c, pal, tab, el)
		}

		canvas := ui.Box(c).Grow(1).MinWidth(0).Background(a.tokens.Canvas).Label(i18n.T("kind.board"))
		canvas.Children(func() {
			a.boardInput(c, canvas)
			canvas.Draw(func(p *ui.Painter, r ui.Rect) {
				p.Fill(r, a.tokens.Canvas, 0)
				for _, el := range tab.board.Visible() {
					sel := a.boardIsSel(tab, el.ID)
					primary := sel && len(tab.boardSel) > 0 && tab.boardSel[len(tab.boardSel)-1] == el.ID
					a.drawBoardElement(p, r, tab, el, tab.boardZoom, tab.boardPanX, tab.boardPanY, sel, primary)
				}
			})
		})
	})
}

// saveBoard 把白板写回文件。
func (a *App) saveBoard(tab *tab) {
	if tab.board == nil || !tab.boardDirty {
		return
	}
	data, err := tab.board.Encode()
	if err != nil {
		a.say(i18n.T("board.error.save") + err.Error())
		return
	}
	if err := a.vault.Write(tab.rel, data); err != nil {
		a.say(i18n.T("board.error.save") + err.Error())
		return
	}
	tab.boardDirty = false
	a.say(i18n.T("board.saved"))
}

// exportBoard 把白板导出成同名 .svg，放在白板旁边。
func (a *App) exportBoard(tab *tab) {
	if tab.board == nil {
		return
	}
	svg := boardSVG(tab.board)
	out := strings.TrimSuffix(tab.rel, ".excalidraw") + ".svg"
	if !strings.HasSuffix(out, ".svg") {
		out += ".svg"
	}
	if err := a.vault.Write(out, []byte(svg)); err != nil {
		a.say(i18n.T("board.error.export") + err.Error())
		return
	}
	a.say(i18n.T("board.exported") + out)
}

// fitBoard 让全部内容落在视野里。
func (a *App) fitBoard(tab *tab) {
	minX, minY, maxX, maxY := tab.board.Bounds()
	if maxX <= minX || maxY <= minY {
		tab.boardZoom = 1
		tab.boardPanX, tab.boardPanY = 40, 40
		return
	}
	const viewW, viewH = 900.0, 600.0
	zoom := math.Min(viewW/(maxX-minX), viewH/(maxY-minY))
	tab.boardZoom = clampFloat(float32(zoom), 0.2, 3)
	tab.boardPanX = 40 - float32(minX)*tab.boardZoom
	tab.boardPanY = 40 - float32(minY)*tab.boardZoom
}

// clampFloat 把 v 限制在 [lo, hi]。
func clampFloat(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// drawBoardElement 画一个白板元素。
func (a *App) drawBoardElement(p *ui.Painter, r ui.Rect, tab *tab, el board.Element, zoom, panX, panY float32, selected, primary bool) {
	x := r.X + panX + float32(el.X)*zoom
	y := r.Y + panY + float32(el.Y)*zoom
	w := float32(el.Width) * zoom
	h := float32(el.Height) * zoom
	stroke := colorOr(el.Stroke, "#1e1e1e")
	width := float32(el.StrokeWidth) * zoom
	if width < 1 {
		width = 1
	}
	fill := el.Fill
	hasFill := fill != "" && fill != "transparent"

	amp := elementAmp(el, zoom)

	switch el.Type {
	case "rectangle":
		if hasFill {
			p.Fill(ui.Rect{X: x, Y: y, W: w, H: h}, colorOr(fill, "#ffffff"), 4*zoom)
		}
		if amp > 0 {
			roughRect(p, x, y, w, h, width, amp, stroke, el.Seed)
		} else {
			p.Stroke(ui.Rect{X: x, Y: y, W: w, H: h}, stroke, 4*zoom, width)
		}
	case "ellipse":
		if hasFill {
			p.FillPath(ellipsePath(x, y, w, h), colorOr(fill, "#ffffff"))
		}
		if amp > 0 {
			roughEllipse(p, x, y, w, h, width, amp, stroke, el.Seed)
		} else {
			p.StrokePath(ellipsePath(x, y, w, h), width, stroke)
		}
	case "diamond":
		pts := [][2]float32{{x + w/2, y}, {x + w, y + h/2}, {x + w/2, y + h}, {x, y + h/2}}
		if hasFill {
			p.FillPath(polyPath(pts), colorOr(fill, "#ffffff"))
		}
		if amp > 0 {
			roughDiamond(p, x, y, w, h, width, amp, stroke, el.Seed)
		} else {
			p.StrokePath(polyPath(pts), width, stroke)
		}
	case "arrow", "line", "freedraw":
		pts := make([][2]float32, 0, len(el.Points))
		for _, pt := range el.Points {
			if len(pt) < 2 {
				continue
			}
			pts = append(pts, [2]float32{x + float32(pt[0])*zoom, y + float32(pt[1])*zoom})
		}
		var path ui.Path
		if el.Type == "freedraw" {
			// 笔画用二次曲线穿过相邻点的中点，手抖的锯齿就没了
			smoothedPath(&path, pts)
		} else {
			for i, p := range pts {
				if i == 0 {
					path.MoveTo(p[0], p[1])
				} else {
					path.LineTo(p[0], p[1])
				}
			}
		}
		if amp > 0 && el.Type != "freedraw" {
			for i := 1; i < len(el.Points); i++ {
				a1, a2 := el.Points[i-1], el.Points[i]
				if len(a1) < 2 || len(a2) < 2 {
					continue
				}
				roughLine(p,
					x+float32(a1[0])*zoom, y+float32(a1[1])*zoom,
					x+float32(a2[0])*zoom, y+float32(a2[1])*zoom,
					width, amp, stroke, el.Seed+int64(i)*977)
			}
		} else {
			p.StrokePath(&path, width, stroke)
		}
		if el.Type == "arrow" && len(el.Points) >= 2 {
			last := el.Points[len(el.Points)-1]
			prev := el.Points[len(el.Points)-2]
			drawArrowHead(p, x+float32(prev[0])*zoom, y+float32(prev[1])*zoom,
				x+float32(last[0])*zoom, y+float32(last[1])*zoom, stroke, zoom, float32(el.StrokeWidth))
		}
	case "image":
		rect := ui.Rect{X: x, Y: y, W: w, H: h}
		if bmp := a.boardImage(tab, el.FileID); bmp != nil {
			p.Image(bmp, rect, ui.FillBox)
		} else {
			// 图没解出来（缺文件或格式不支持）就画个占位框，别让它凭空消失
			p.Fill(rect, a.tokens.Panel, 4)
			p.Stroke(rect, a.tokens.LineStrong, 4, 1)
			p.Text(x+6, y+6, i18n.T("common.image"), 12, a.tokens.InkMuted)
		}
	case "text":
		size := float32(el.FontSize) * zoom
		if size < 6 {
			size = 6
		}
		lh := size * float32(board.LineHeight)
		for i, line := range strings.Split(el.Text, "\n") {
			if line == "" {
				continue
			}
			p.Text(x, y+float32(i)*lh, line, size, stroke)
		}
	}

	if selected {
		p.Stroke(ui.Rect{X: x - 4, Y: y - 4, W: w + 8, H: h + 8}, a.tokens.BoardSelect, 2, 1)
	}
	// 主选中元素画四个角，拖它能改尺寸（文字、画笔、线不画）
	if primary && el.Type != "text" && el.Type != "freedraw" &&
		el.Type != "arrow" && el.Type != "line" {
		for _, c := range [][2]float32{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}} {
			p.Fill(ui.Rect{X: c[0] - 4, Y: c[1] - 4, W: 8, H: 8}, a.tokens.BoardHandle, 2)
			p.Stroke(ui.Rect{X: c[0] - 4, Y: c[1] - 4, W: 8, H: 8}, a.tokens.BoardSelect, 2, 1.5)
		}
	}
	// 选中箭头时两端各画一个把手，拖它就能改接
	if selected && (el.Type == "arrow" || el.Type == "line") && len(el.Points) >= 2 {
		first, last := el.Points[0], el.Points[len(el.Points)-1]
		if len(first) >= 2 && len(last) >= 2 {
			for _, pt := range [][]float64{first, last} {
				hx := x + float32(pt[0])*zoom
				hy := y + float32(pt[1])*zoom
				p.Fill(ui.Rect{X: hx - 4, Y: hy - 4, W: 8, H: 8}, a.tokens.BoardHandle, 4)
				p.Stroke(ui.Rect{X: hx - 4, Y: hy - 4, W: 8, H: 8}, a.tokens.BoardSelect, 4, 1.5)
			}
		}
	}
}

// ellipsePath 用四段贝塞尔画椭圆。
func ellipsePath(x, y, w, h float32) *ui.Path {
	const k = 0.5523
	cx, cy := x+w/2, y+h/2
	rx, ry := w/2, h/2
	path := &ui.Path{}
	path.MoveTo(cx, cy-ry)
	path.CubeTo(cx+rx*k, cy-ry, cx+rx, cy-ry*k, cx+rx, cy)
	path.CubeTo(cx+rx, cy+ry*k, cx+rx*k, cy+ry, cx, cy+ry)
	path.CubeTo(cx-rx*k, cy+ry, cx-rx, cy+ry*k, cx-rx, cy)
	path.CubeTo(cx-rx, cy-ry*k, cx-rx*k, cy-ry, cx, cy-ry)
	path.Close()
	return path
}

// drawArrowHead 画箭头的头部。
func drawArrowHead(p *ui.Painter, x0, y0, x1, y1 float32, color ui.Color, zoom, strokeWidth float32) {
	angle := math.Atan2(float64(y1-y0), float64(x1-x0))
	// 箭头随线宽变粗变大，否则细线配大箭头、粗线配小箭头都不好看
	size := (10 + strokeWidth*2.5) * zoom
	w := strokeWidth * zoom
	if w < 1.6 {
		w = 1.6
	}
	for _, sign := range []float64{-1, 1} {
		a := angle + sign*0.42
		p.Line(x1, y1, x1-float32(math.Cos(a))*size, y1-float32(math.Sin(a))*size, w, color)
	}
}

// colorOr 把 #rrggbb 字符串换成颜色，坏值用兜底色。
func colorOr(s, fallback string) ui.Color {
	s = strings.TrimSpace(s)
	if len(s) == 7 && strings.HasPrefix(s, "#") {
		return ui.Hex(s)
	}
	return ui.Hex(fallback)
}

// boardSnapshot 在改动前存一份快照，供撤销用。
func (a *App) boardSnapshot(tab *tab) {
	if tab.board == nil {
		return
	}
	tab.boardUndo = append(tab.boardUndo, tab.board.Clone())
	if len(tab.boardUndo) > 60 {
		tab.boardUndo = tab.boardUndo[len(tab.boardUndo)-60:]
	}
	tab.boardRedo = nil
}

// boardUndoStep 撤销一步。
func (a *App) boardUndoStep(tab *tab) {
	if len(tab.boardUndo) == 0 {
		return
	}
	tab.boardRedo = append(tab.boardRedo, tab.board.Clone())
	last := tab.boardUndo[len(tab.boardUndo)-1]
	tab.boardUndo = tab.boardUndo[:len(tab.boardUndo)-1]
	tab.board.Restore(last)
	tab.boardSel = nil
	tab.boardDirty = true
}

// boardRedoStep 重做一步。
func (a *App) boardRedoStep(tab *tab) {
	if len(tab.boardRedo) == 0 {
		return
	}
	tab.boardUndo = append(tab.boardUndo, tab.board.Clone())
	last := tab.boardRedo[len(tab.boardRedo)-1]
	tab.boardRedo = tab.boardRedo[:len(tab.boardRedo)-1]
	tab.board.Restore(last)
	tab.boardSel = nil
	tab.boardDirty = true
}

// resizeBoardElement 在拖动新建时按起点与当前点定尺寸。
func resizeBoardElement(el *board.Element, x0, y0, x1, y1 float64) {
	switch el.Type {
	case "arrow", "line":
		el.X, el.Y = x0, y0
		el.Points = [][]float64{{0, 0}, {x1 - x0, y1 - y0}}
		el.Width = math.Abs(x1 - x0)
		el.Height = math.Abs(y1 - y0)
	case "text":
		el.X, el.Y = x0, y0
	default:
		el.X = math.Min(x0, x1)
		el.Y = math.Min(y0, y1)
		el.Width = math.Abs(x1 - x0)
		el.Height = math.Abs(y1 - y0)
	}
}

// finishBoardCreate 结束新建：太小就给个默认尺寸，箭头两端吸附到形状上。
func (a *App) finishBoardCreate(tab *tab) {
	tab.boardCreating = false
	el := tab.board.Find(tab.boardDragEl)
	if el == nil {
		return
	}
	switch el.Type {
	case "arrow", "line":
		if math.Hypot(el.Width, el.Height) < 8 {
			el.Width = 160
			el.Height = 0
			el.Points = [][]float64{{0, 0}, {160, 0}}
		}
		// 两端吸附：离形状 12 像素以内就粘上
		sx, sy, ex, ey := el.PointsAbs()
		if shape := tab.board.ShapeNear(sx, sy, 12); shape != nil && shape.ID != el.ID {
			el.StartBinding = &board.Binding{ElementID: shape.ID}
		}
		if shape := tab.board.ShapeNear(ex, ey, 12); shape != nil && shape.ID != el.ID {
			el.EndBinding = &board.Binding{ElementID: shape.ID}
		}
		tab.board.Rebind(el)
	case "rectangle", "ellipse", "diamond":
		if el.Width < 8 || el.Height < 8 {
			el.Width, el.Height = 160, 100
		}
	}
	tab.boardDirty = true
}

// finishEndpointDrag 松手时把拖过的那一端吸附到形状上（附近没有就保持自由端）。
func (a *App) finishEndpointDrag(tab *tab) {
	el := tab.board.Find(tab.boardDragEl)
	which := tab.boardEndDrag
	tab.boardEndDrag = -1
	if el == nil {
		return
	}
	sx, sy, ex, ey := el.PointsAbs()
	px, py := sx, sy
	if which == 1 {
		px, py = ex, ey
	}
	if shape := tab.board.ShapeNear(px, py, 12); shape != nil && shape.ID != el.ID {
		if which == 0 {
			el.StartBinding = &board.Binding{ElementID: shape.ID}
		} else {
			el.EndBinding = &board.Binding{ElementID: shape.ID}
		}
		tab.board.Rebind(el)
	} else {
		// 没吸附：解绑这一端，并保证 points 是规范的两点形式
		if which == 0 {
			el.StartBinding = nil
		} else {
			el.EndBinding = nil
		}
		tab.board.SetEndpoints(el, sx, sy, ex, ey)
	}
	tab.boardDirty = true
}

// 白板的配色：描边用深色系，填充用浅色系（与 Excalidraw 的默认色板接近）。
var (
	boardStrokeColors = []string{"#1e1e1e", "#e03131", "#2f9e44", "#1971c2", "#f08c00", "#9c36b5"}
	boardFillColors   = []string{"transparent", "#ffec99", "#b2f2bb", "#a5d8ff", "#ffc9c9", "#d0bfff"}
	boardStrokeWidths = []float64{1, 2, 4}
	boardFontSizes    = []float64{14, 20, 28, 36}
)

// boardStyleRow 画选中元素的样式行。
func (a *App) boardStyleRow(c *ui.Context, pal chrome, tab *tab, el *board.Element) {
	t := c.Theme()
	ui.Row(c).Height(32).Padding(0, 10).Gap(6).AlignItems(ui.Center).Children(func() {
		ui.Text(c, i18n.T("board.style.stroke")).FontSize(10.5).TextColor(t.TextMuted)
		for _, col := range boardStrokeColors {
			col := col
			on := strings.EqualFold(el.Stroke, col)
			a.swatch(c, t, col, on, func() {
				a.boardSnapshot(tab)
				el.Stroke = col
				tab.boardDirty = true
			})
		}
		if el.Type != "text" {
			ui.Box(c).Width(8)
			ui.Text(c, i18n.T("board.style.fill")).FontSize(10.5).TextColor(t.TextMuted)
			for _, col := range boardFillColors {
				col := col
				on := strings.EqualFold(el.Fill, col)
				a.swatch(c, t, col, on, func() {
					a.boardSnapshot(tab)
					el.Fill = col
					tab.boardDirty = true
				})
			}
			ui.Box(c).Width(8)
			ui.Text(c, i18n.T("board.style.stroke_width")).FontSize(10.5).TextColor(t.TextMuted)
			for _, w := range boardStrokeWidths {
				w := w
				on := el.StrokeWidth == w
				label := fmt.Sprintf("%.0f", w)
				b := ui.ButtonBase(c).Width(24).Height(20).Radius(5).
					Cursor(ui.CursorPointer).Label(i18n.T("board.style.stroke_width_prefix") + label)
				if on {
					b.Background(t.SurfaceHover)
					b.TextColor(t.Text)
				} else {
					b.TextColor(t.TextMuted)
				}
				b.OnClick(func() {
					a.boardSnapshot(tab)
					el.StrokeWidth = w
					tab.boardDirty = true
				})
				b.Children(func() { ui.Text(c, label).FontSize(11) })
			}
		} else {
			ui.Box(c).Width(8)
			ui.Text(c, i18n.T("board.style.font_size")).FontSize(10.5).TextColor(t.TextMuted)
			for _, size := range boardFontSizes {
				size := size
				on := el.FontSize == size
				label := fmt.Sprintf("%.0f", size)
				b := ui.ButtonBase(c).Width(26).Height(20).Radius(5).
					Cursor(ui.CursorPointer).Label(i18n.T("board.style.font_size_prefix") + label)
				if on {
					b.Background(t.SurfaceHover)
					b.TextColor(t.Text)
				} else {
					b.TextColor(t.TextMuted)
				}
				b.OnClick(func() {
					a.boardSnapshot(tab)
					el.FontSize = size
					if el.ContainerID != "" {
						if shape := tab.board.Find(el.ContainerID); shape != nil {
							tab.board.CenterContainerText(shape)
						}
					}
					tab.boardDirty = true
				})
				b.Children(func() { ui.Text(c, label).FontSize(11) })
			}
		}
		ui.Box(c).Grow(1)
		// 选择类动作放在这一行末尾：它们只对选中的元素有意义
		textButton(c, pal, i18n.T("board.action.group"), i18n.T("board.action.group_tooltip"), func() { a.boardGroup(tab) })
		textButton(c, pal, i18n.T("board.action.ungroup"), i18n.T("board.action.ungroup_tooltip"), func() { a.boardUngroup(tab) })
		textButton(c, pal, i18n.T("common.delete"), i18n.T("board.action.delete_tooltip"), func() { a.boardDeleteSelection(tab) })
	})
}

// swatch 是一个颜色小方块。
func (a *App) swatch(c *ui.Context, t *ui.Theme, col string, on bool, fn func()) {
	b := ui.ButtonBase(c).Width(16).Height(16).Radius(8).Cursor(ui.CursorPointer).
		Label(i18n.T("board.style.color_prefix") + col)
	if col == "transparent" {
		b.Background(t.Background)
		b.Border(1, t.Border)
	} else {
		b.Background(ui.Hex(col))
	}
	if on {
		b.Border(2, t.Text)
	}
	b.OnClick(fn)
}

// boardImage 解出白板里的一张图（按 fileId 缓存，避免每帧 base64 解码）。
func (a *App) boardImage(tab *tab, fileID string) *ui.Bitmap {
	if fileID == "" || tab.board == nil {
		return nil
	}
	if bmp, ok := tab.boardImages[fileID]; ok {
		return bmp
	}
	f, ok := tab.board.Files[fileID]
	if !ok {
		return nil
	}
	data, err := decodeDataURL(f.DataURL)
	if err != nil {
		return nil
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil
	}
	bmp := ui.NewBitmap(img)
	if tab.boardImages == nil {
		tab.boardImages = map[string]*ui.Bitmap{}
	}
	tab.boardImages[fileID] = bmp
	return bmp
}

// decodeDataURL 拆开 data:image/png;base64,xxxx。
func decodeDataURL(s string) ([]byte, error) {
	i := strings.Index(s, ",")
	if i < 0 || !strings.HasPrefix(s, "data:") {
		return nil, errors.New(i18n.T("board.error.not_data_url"))
	}
	head, body := s[:i], s[i+1:]
	if !strings.Contains(head, "base64") {
		return nil, errors.New(i18n.T("board.error.base64_only"))
	}
	return base64.StdEncoding.DecodeString(body)
}

// insertBoardImage 从磁盘挑一张图片插进白板。
func (a *App) insertBoardImage(tab *tab) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:      a.win,
		Title:       i18n.T("board.tool.image"),
		ButtonLabel: i18n.T("common.insert"),
		Filters: []mygo.FileFilter{
			{Name: i18n.T("common.image"), Extensions: []string{"png", "jpg", "jpeg", "gif", "webp"}},
		},
	})
	if err != nil || len(paths) == 0 {
		return
	}
	data, err := os.ReadFile(paths[0])
	if err != nil {
		a.say(i18n.T("board.error.read_image") + err.Error())
		return
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		a.say(i18n.T("board.error.decode_image") + err.Error())
		return
	}
	mime := mimeForExt(filepath.Ext(paths[0]))

	// 太大的图按 420 宽缩一下，免得一张照片铺满整块白板
	b := img.Bounds()
	w, h := float64(b.Dx()), float64(b.Dy())
	const maxSide = 420.0
	if w > maxSide || h > maxSide {
		scale := maxSide / math.Max(w, h)
		w, h = w*scale, h*scale
	}

	a.boardSnapshot(tab)
	fileID := fmt.Sprintf("img-%d", time.Now().UnixNano())
	if tab.board.Files == nil {
		tab.board.Files = map[string]board.File{}
	}
	tab.board.Files[fileID] = board.File{
		ID:       fileID,
		MimeType: mime,
		DataURL:  "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(data),
		Created:  time.Now().UnixMilli(),
	}
	// 放在现有内容的右边，不压在别的东西上
	maxX, minY, _, _ := tab.board.Bounds()
	el := tab.board.Add("image", maxX+40, minY)
	el.Width, el.Height = w, h
	el.FileID = fileID
	a.boardSetSel(tab, el.ID)
	tab.boardDirty = true
	a.say(i18n.T("board.image_inserted"))
}

// mimeForExt 按扩展名给 MIME。
func mimeForExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".png":
		return "image/png"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	default:
		return "image/jpeg"
	}
}

// appendBoardPoint 往笔画里加一个点（离上一个太近就跳过，免得点太密）。
func appendBoardPoint(el *board.Element, x, y float64) {
	if len(el.Points) == 0 {
		el.Points = [][]float64{{0, 0}}
	}
	last := el.Points[len(el.Points)-1]
	lx, ly := el.X+last[0], el.Y+last[1]
	if math.Hypot(x-lx, y-ly) < 2 {
		return
	}
	el.Points = append(el.Points, []float64{x - el.X, y - el.Y})
	if x-el.X > el.Width {
		el.Width = x - el.X
	}
	if y-el.Y > el.Height {
		el.Height = y - el.Y
	}
}

// finishBoardStroke 收笔：点数太少当点一下处理，并重新算包围盒。
func finishBoardStroke(tab *tab) {
	tab.boardDrawing = false
	el := tab.board.Find(tab.boardDragEl)
	if el == nil {
		return
	}
	if len(el.Points) < 2 {
		// 只点了一下：画一个很小的点，至少看得出来
		el.Points = [][]float64{{0, 0}, {1, 1}}
	}
	minX, minY := 0.0, 0.0
	maxX, maxY := 0.0, 0.0
	for _, p := range el.Points {
		if len(p) < 2 {
			continue
		}
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	// 归一化：把包围盒左上角挪到元素原点上
	el.X += minX
	el.Y += minY
	for i, p := range el.Points {
		if len(p) < 2 {
			continue
		}
		el.Points[i] = []float64{p[0] - minX, p[1] - minY}
	}
	el.Width, el.Height = maxX-minX, maxY-minY
	tab.boardDirty = true
}

// smoothedPath 用中点二次曲线把折线抹平（手绘笔画的常用做法）。
func smoothedPath(path *ui.Path, pts [][2]float32) {
	if len(pts) == 0 {
		return
	}
	if len(pts) == 1 {
		path.MoveTo(pts[0][0], pts[0][1])
		return
	}
	path.MoveTo(pts[0][0], pts[0][1])
	for i := 1; i < len(pts)-1; i++ {
		mx := (pts[i][0] + pts[i+1][0]) / 2
		my := (pts[i][1] + pts[i+1][1]) / 2
		path.QuadTo(pts[i][0], pts[i][1], mx, my)
	}
	last := pts[len(pts)-1]
	path.LineTo(last[0], last[1])
}

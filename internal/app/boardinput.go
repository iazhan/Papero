package app

import (
	"math"

	"github.com/egoist/mygo/ui"

	"papero/internal/board"
	"papero/internal/i18n"
)

// 白板的指针交互：点选、多选、拖动、拉角缩放、拖端点、画笔、新建。

// boardInput 处理白板上的指针输入。
func (a *App) boardInput(c *ui.Context, canvas ui.Element) {
	tab := a.tabs[a.active]
	if tab.board == nil {
		return
	}
	x, y, ok := canvas.PointerPosition()
	if !ok {
		return
	}
	sx := float64((x - tab.boardPanX) / tab.boardZoom)
	sy := float64((y - tab.boardPanY) / tab.boardZoom)

	if canvas.DoubleClicked() && tab.boardTool == "select" {
		a.boardDoubleClick(tab, sx, sy)
		return
	}

	if canvas.PressedInside() && !tab.boardDragging {
		tab.boardDragging = true
		tab.boardDragStartX, tab.boardDragStartY = sx, sy
		a.boardPress(c, tab, sx, sy)
		return
	}

	if tab.boardDragging && !canvas.Pressed() {
		tab.boardDragging = false
		tab.boardPanning = false
		if tab.boardCreating {
			a.finishBoardCreate(tab)
		}
		if tab.boardEndDrag >= 0 {
			a.finishEndpointDrag(tab)
		}
		if tab.boardDrawing {
			finishBoardStroke(tab)
		}
		if tab.boardResize >= 0 {
			tab.boardResize = -1
		}
		tab.boardDragEl = ""
		tab.boardDragOrig = nil
		return
	}

	if tab.boardDragging {
		a.boardDrag(tab, sx, sy)
	}

	// 撤销 / 重做
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyZ) {
		a.boardRedoStep(tab)
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyZ) {
		a.boardUndoStep(tab)
		return
	}
	if c.Shortcut(ui.Cmd, ui.KeyY) {
		a.boardRedoStep(tab)
		return
	}
	// 分组 / 取消分组
	if c.Shortcut(ui.Cmd, ui.KeyG) {
		a.boardGroup(tab)
		return
	}
	if c.Shortcut(ui.Cmd|ui.Shift, ui.KeyG) {
		a.boardUngroup(tab)
		return
	}

	// Delete 删除选中的全部元素
	if len(tab.boardSel) > 0 && (c.KeyDown(ui.KeyDelete) || c.KeyDown(ui.KeyBackspace)) {
		a.boardDeleteSelection(tab)
	}
}

// boardPress 处理按下：按当前工具决定是选、是画还是新建。
func (a *App) boardPress(c *ui.Context, tab *tab, sx, sy float64) {
	shift := c.Modifiers()&ui.Shift != 0
	switch tab.boardTool {
	case "select":
		// 1) 拉角缩放
		if id, corner := a.boardResizeHandleAt(tab, sx, sy); corner >= 0 {
			el := tab.board.Find(id)
			if el != nil {
				a.boardSnapshot(tab)
				tab.boardResize = corner
				tab.boardResizeBox = [4]float64{el.X, el.Y, el.Width, el.Height}
				tab.boardDragEl = id
				return
			}
		}
		// 2) 箭头端点
		if el := a.boardPrimary(tab); el != nil {
			tol := 10 / float64(tab.boardZoom)
			if which := tab.board.EndpointAt(el, sx, sy, tol); which >= 0 {
				a.boardSnapshot(tab)
				tab.boardDragEl = el.ID
				tab.boardEndDrag = which
				if which == 0 {
					el.StartBinding = nil
				} else {
					el.EndBinding = nil
				}
				return
			}
		}
		// 3) 点中元素
		if el := tab.board.Hit(sx, sy); el != nil {
			ids := tab.board.ExpandGroup(el.ID)
			if shift {
				a.boardAddSel(tab, ids...)
			} else if !a.boardIsSel(tab, el.ID) {
				a.boardSetSel(tab, ids...)
			}
			a.boardSnapshot(tab)
			tab.boardDragEl = el.ID
			tab.boardDragOffX = sx - el.X
			tab.boardDragOffY = sy - el.Y
			tab.boardDragOrig = map[string][2]float64{}
			for _, id := range a.boardSelIDs(tab) {
				if e := tab.board.Find(id); e != nil {
					tab.boardDragOrig[id] = [2]float64{e.X, e.Y}
				}
			}
			return
		}
		// 4) 空白处：清空选择并准备平移
		if !shift {
			a.boardClearSel(tab)
		}
		tab.boardDragEl = ""
		tab.boardPanning = true
	case "pen":
		a.boardSnapshot(tab)
		el := tab.board.Add("freedraw", sx, sy)
		el.Points = [][]float64{{0, 0}}
		el.Width, el.Height = 0, 0
		a.boardSetSel(tab, el.ID)
		tab.boardDragEl = el.ID
		tab.boardDrawing = true
		tab.boardDirty = true
	default:
		kind := tab.boardTool
		a.boardSnapshot(tab)
		el := tab.board.Add(kind, sx, sy)
		tab.boardCreateX, tab.boardCreateY = sx, sy
		tab.boardCreating = true
		switch kind {
		case "rectangle", "ellipse", "diamond":
			el.Width, el.Height = 0, 0
		case "arrow", "line":
			el.Points = [][]float64{{0, 0}, {0, 0}}
			el.Width, el.Height = 0, 0
		}
		a.boardSetSel(tab, el.ID)
		tab.boardDragEl = el.ID
		tab.boardDragOffX, tab.boardDragOffY = 0, 0
		tab.boardTool = "select"
		tab.boardDirty = true
	}
}

// boardDrag 处理拖动。
func (a *App) boardDrag(tab *tab, sx, sy float64) {
	if tab.boardPanning {
		tab.boardPanX += float32(sx - tab.boardDragStartX)
		tab.boardPanY += float32(sy - tab.boardDragStartY)
		tab.boardDragStartX, tab.boardDragStartY = sx, sy
		return
	}
	el := tab.board.Find(tab.boardDragEl)
	if el == nil {
		return
	}
	switch {
	case tab.boardDrawing:
		appendBoardPoint(el, sx, sy)
		tab.boardDirty = true
	case tab.boardEndDrag >= 0:
		sx0, sy0, ex0, ey0 := el.PointsAbs()
		if tab.boardEndDrag == 0 {
			tab.board.SetEndpoints(el, sx, sy, ex0, ey0)
		} else {
			tab.board.SetEndpoints(el, sx0, sy0, sx, sy)
		}
		tab.boardDirty = true
	case tab.boardResize >= 0:
		resizeFromCorner(el, tab.boardResize, tab.boardResizeBox, sx, sy)
		tab.board.RebindAll(el.ID)
		tab.board.CenterContainerText(el)
		tab.boardDirty = true
	case tab.boardCreating:
		resizeBoardElement(el, tab.boardCreateX, tab.boardCreateY, sx, sy)
		tab.boardDirty = true
	default:
		// 多选时整体平移：按按下时记下的位置加同一个位移
		dx := sx - (el.X + tab.boardDragOffX)
		dy := sy - (el.Y + tab.boardDragOffY)
		for id, orig := range tab.boardDragOrig {
			if e := tab.board.Find(id); e != nil {
				e.X = orig[0] + dx
				e.Y = orig[1] + dy
				tab.board.RebindAll(e.ID)
				tab.board.CenterContainerText(e)
			}
		}
		tab.boardDirty = true
	}
}

// boardDoubleClick 双击：文字/形状进编辑，箭头加一个折点。
func (a *App) boardDoubleClick(tab *tab, sx, sy float64) {
	el := tab.board.Hit(sx, sy)
	if el == nil {
		return
	}
	a.boardSnapshot(tab)
	switch {
	case el.Type == "text":
		a.boardSetSel(tab, el.ID)
		tab.boardTextFocus = true
	case board.IsShape(*el):
		text := tab.board.EnsureContainerText(el)
		a.boardSetSel(tab, text.ID)
		tab.boardTextFocus = true
		tab.boardDirty = true
	case el.Type == "arrow" || el.Type == "line":
		// 双击线上加一个折点
		if tab.board.AddBend(el, sx, sy) {
			a.boardSetSel(tab, el.ID)
			tab.boardDirty = true
		}
	}
}

// resizeFromCorner 按拖动的角改元素尺寸。
func resizeFromCorner(el *board.Element, corner int, box [4]float64, sx, sy float64) {
	x, y, w, h := box[0], box[1], box[2], box[3]
	const min = 8.0
	switch corner {
	case 0: // 左上
		w, h = x+w-sx, y+h-sy
		x, y = sx, sy
	case 1: // 右上
		w, h = sx-x, y+h-sy
		y = sy
	case 2: // 右下
		w, h = sx-x, sy-y
	case 3: // 左下
		w, h = x+w-sx, sy-y
		x = sx
	}
	if w < min {
		w = min
	}
	if h < min {
		h = min
	}
	el.X, el.Y, el.Width, el.Height = x, y, w, h
}

// boardResizeHandleAt 判断按下的位置是不是某个选中元素的角，返回元素 ID 与角号。
func (a *App) boardResizeHandleAt(tab *tab, sx, sy float64) (string, int) {
	tol := 9 / float64(tab.boardZoom)
	for _, id := range tab.boardSel {
		el := tab.board.Find(id)
		if el == nil || el.Type == "text" || el.Type == "freedraw" ||
			el.Type == "arrow" || el.Type == "line" {
			continue
		}
		corners := [][2]float64{
			{el.X, el.Y},
			{el.X + el.Width, el.Y},
			{el.X + el.Width, el.Y + el.Height},
			{el.X, el.Y + el.Height},
		}
		for i, c := range corners {
			if math.Hypot(sx-c[0], sy-c[1]) <= tol {
				return id, i
			}
		}
	}
	return "", -1
}

// ---- 选择集 ----

// boardIsSel 报告元素是否在选中集合里。
func (a *App) boardIsSel(tab *tab, id string) bool {
	for _, s := range tab.boardSel {
		if s == id {
			return true
		}
	}
	return false
}

// boardSetSel 用给定 ID 替换整个选择集。
func (a *App) boardSetSel(tab *tab, ids ...string) {
	tab.boardSel = append([]string(nil), ids...)
}

// boardAddSel 往选择集里加（Shift 点选）。
func (a *App) boardAddSel(tab *tab, ids ...string) {
	for _, id := range ids {
		if !a.boardIsSel(tab, id) {
			tab.boardSel = append(tab.boardSel, id)
		}
	}
}

// boardClearSel 清空选择。
func (a *App) boardClearSel(tab *tab) {
	tab.boardSel = nil
}

// boardSelIDs 返回选择集（分组已展开，去重）。
func (a *App) boardSelIDs(tab *tab) []string {
	seen := map[string]bool{}
	var out []string
	for _, id := range tab.boardSel {
		for _, g := range tab.board.ExpandGroup(id) {
			if !seen[g] {
				seen[g] = true
				out = append(out, g)
			}
		}
	}
	return out
}

// boardPrimary 返回最后选中的元素（样式行、端点拖动用它）。
func (a *App) boardPrimary(tab *tab) *board.Element {
	if len(tab.boardSel) == 0 {
		return nil
	}
	return tab.board.Find(tab.boardSel[len(tab.boardSel)-1])
}

// boardDeleteSelection 删掉选中的全部元素。
func (a *App) boardDeleteSelection(tab *tab) {
	if len(tab.boardSel) == 0 {
		return
	}
	a.boardSnapshot(tab)
	for _, id := range a.boardSelIDs(tab) {
		tab.board.RemoveWithContainer(id)
	}
	a.boardClearSel(tab)
	tab.boardDirty = true
}

// boardGroup 把选中的元素编成一组。
func (a *App) boardGroup(tab *tab) {
	ids := a.boardSelIDs(tab)
	if len(ids) < 2 {
		a.say(i18n.T("board.hint.select_more"))
		return
	}
	a.boardSnapshot(tab)
	tab.board.Group(ids)
	tab.boardDirty = true
	a.say(i18n.T("board.grouped"))
}

// boardUngroup 取消选中元素的分组。
func (a *App) boardUngroup(tab *tab) {
	ids := a.boardSelIDs(tab)
	if len(ids) == 0 {
		return
	}
	a.boardSnapshot(tab)
	tab.board.Ungroup(ids)
	tab.boardDirty = true
	a.say(i18n.T("board.ungrouped"))
}

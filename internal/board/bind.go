package board

import "math"

// 箭头绑定：箭头两端可以粘在形状上，形状一动，箭头端点跟着走。
//
// 端点位置不是存死坐标，而是每次按「另一端的方向 → 形状边界」重算，
// 这样形状缩放、移动都不用额外处理。

// IsShape 报告元素能不能被箭头粘上（文字和线不行）。
func IsShape(el Element) bool {
	switch el.Type {
	case "rectangle", "ellipse", "diamond":
		return true
	}
	return false
}

// ShapeAt 找出覆盖点 (x, y) 的形状，优先取最上面的。
func (s *Scene) ShapeAt(x, y float64) *Element {
	for i := len(s.Elements) - 1; i >= 0; i-- {
		el := &s.Elements[i]
		if el.Deleted || !IsShape(*el) {
			continue
		}
		if x >= el.X && x <= el.X+el.Width && y >= el.Y && y <= el.Y+el.Height {
			return el
		}
	}
	return nil
}

// ShapeNear 找出离点最近、且在 pad 之内的形状（给「松手时吸附」用）。
func (s *Scene) ShapeNear(x, y, pad float64) *Element {
	var best *Element
	bestD := math.Inf(1)
	for i := range s.Elements {
		el := &s.Elements[i]
		if el.Deleted || !IsShape(*el) {
			continue
		}
		dx := math.Max(math.Max(el.X-x, 0), x-(el.X+el.Width))
		dy := math.Max(math.Max(el.Y-y, 0), y-(el.Y+el.Height))
		d := math.Hypot(dx, dy)
		if d <= pad && d < bestD {
			bestD, best = d, el
		}
	}
	return best
}

// EdgePoint 返回从 (fromX, fromY) 射向形状中心时，落在形状边界上的点。
func (el Element) EdgePoint(fromX, fromY float64) (float64, float64) {
	cx := el.X + el.Width/2
	cy := el.Y + el.Height/2
	dx, dy := fromX-cx, fromY-cy
	if dx == 0 && dy == 0 {
		return cx, cy
	}
	switch el.Type {
	case "ellipse":
		rx, ry := el.Width/2, el.Height/2
		if rx <= 0 || ry <= 0 {
			return cx, cy
		}
		// 椭圆：按参数方程找与射线的交点
		t := math.Atan2(dy/ry, dx/rx)
		return cx + rx*math.Cos(t), cy + ry*math.Sin(t)
	case "diamond":
		// 菱形：四条边里求交点
		pts := [][2]float64{
			{cx, el.Y}, {el.X + el.Width, cy}, {cx, el.Y + el.Height}, {el.X, cy},
		}
		bestT := math.Inf(1)
		var bx, by float64
		for i := 0; i < 4; i++ {
			a, b := pts[i], pts[(i+1)%4]
			if t, ok := raySegment(cx, cy, dx, dy, a[0], a[1], b[0], b[1]); ok && t < bestT {
				bestT = t
				bx, by = cx+dx*t, cy+dy*t
			}
		}
		if math.IsInf(bestT, 1) {
			return cx, cy
		}
		return bx, by
	}
	// 矩形：按方向比例取边
	hx, hy := el.Width/2, el.Height/2
	sx := math.Inf(1)
	if dx != 0 {
		sx = hx / math.Abs(dx)
	}
	sy := math.Inf(1)
	if dy != 0 {
		sy = hy / math.Abs(dy)
	}
	s := math.Min(sx, sy)
	return cx + dx*s, cy + dy*s
}

// raySegment 求射线 (ox,oy)+t*(dx,dy) 与线段的交点参数 t（t>0）。
func raySegment(ox, oy, dx, dy, x1, y1, x2, y2 float64) (float64, bool) {
	ex, ey := x2-x1, y2-y1
	den := dx*ey - dy*ex
	if math.Abs(den) < 1e-9 {
		return 0, false
	}
	t := ((x1-ox)*ey - (y1-oy)*ex) / den
	u := ((x1-ox)*dy - (y1-oy)*dx) / den
	if t <= 0 || u < 0 || u > 1 {
		return 0, false
	}
	return t, true
}

// PointsAbs 返回箭头两端在场景里的绝对坐标。
func (el Element) PointsAbs() (float64, float64, float64, float64) {
	if len(el.Points) < 2 {
		return el.X, el.Y, el.X + el.Width, el.Y + el.Height
	}
	first := el.Points[0]
	last := el.Points[len(el.Points)-1]
	return el.X + first[0], el.Y + first[1], el.X + last[0], el.Y + last[1]
}

// Rebind 按两端绑定的形状重算箭头端点。没有绑定的那一端保持原样。
func (s *Scene) Rebind(el *Element) {
	if el.Type != "arrow" && el.Type != "line" {
		return
	}
	sx, sy, ex, ey := el.PointsAbs()

	if el.StartBinding != nil {
		if shape := s.Find(el.StartBinding.ElementID); shape != nil {
			// 用「另一端」当方向参照；另一端也绑着的话就用它的形状中心
			tx, ty := ex, ey
			if el.EndBinding != nil {
				if other := s.Find(el.EndBinding.ElementID); other != nil {
					tx = other.X + other.Width/2
					ty = other.Y + other.Height/2
				}
			}
			sx, sy = shape.EdgePoint(tx, ty)
		}
	}
	if el.EndBinding != nil {
		if shape := s.Find(el.EndBinding.ElementID); shape != nil {
			tx, ty := sx, sy
			if el.StartBinding != nil {
				if other := s.Find(el.StartBinding.ElementID); other != nil {
					tx = other.X + other.Width/2
					ty = other.Y + other.Height/2
				}
			}
			ex, ey = shape.EdgePoint(tx, ty)
		}
	}
	el.X, el.Y = sx, sy
	el.Points = [][]float64{{0, 0}, {ex - sx, ey - sy}}
	el.Width = math.Abs(ex - sx)
	el.Height = math.Abs(ey - sy)
}

// RebindAll 把所有粘在 id 上的箭头重算一遍（形状移动、缩放后调用）。
func (s *Scene) RebindAll(id string) {
	for i := range s.Elements {
		el := &s.Elements[i]
		if el.Type != "arrow" && el.Type != "line" {
			continue
		}
		if (el.StartBinding != nil && el.StartBinding.ElementID == id) ||
			(el.EndBinding != nil && el.EndBinding.ElementID == id) {
			s.Rebind(el)
		}
	}
}

// Unbind 把粘在 id 上的箭头解开（形状被删掉时调用）。
func (s *Scene) Unbind(id string) {
	for i := range s.Elements {
		el := &s.Elements[i]
		if el.StartBinding != nil && el.StartBinding.ElementID == id {
			el.StartBinding = nil
		}
		if el.EndBinding != nil && el.EndBinding.ElementID == id {
			el.EndBinding = nil
		}
	}
}

// SetEndpoints 用绝对坐标设置一条线的两端（保持 Points 相对元素原点）。
func (s *Scene) SetEndpoints(el *Element, sx, sy, ex, ey float64) {
	el.X, el.Y = sx, sy
	el.Points = [][]float64{{0, 0}, {ex - sx, ey - sy}}
	el.Width = math.Abs(ex - sx)
	el.Height = math.Abs(ey - sy)
}

// EndpointAt 返回指针附近是哪个端点：0 起点、1 终点、-1 都不是。
// tol 是场景坐标下的容差。
func (s *Scene) EndpointAt(el *Element, x, y, tol float64) int {
	if el.Type != "arrow" && el.Type != "line" {
		return -1
	}
	sx, sy, ex, ey := el.PointsAbs()
	ds := math.Hypot(x-sx, y-sy)
	de := math.Hypot(x-ex, y-ey)
	if ds <= tol && ds <= de {
		return 0
	}
	if de <= tol {
		return 1
	}
	return -1
}

// AddBend 在离 (x, y) 最近的那一段上插一个折点，把直线变成折线。
// 返回是否真的加了。
func (s *Scene) AddBend(el *Element, x, y float64) bool {
	if el.Type != "arrow" && el.Type != "line" {
		return false
	}
	if len(el.Points) < 2 {
		return false
	}
	// 点到各段的最短距离，找最近的一段
	bestSeg, bestD := -1, math.Inf(1)
	for i := 1; i < len(el.Points); i++ {
		a := el.Points[i-1]
		b := el.Points[i]
		if len(a) < 2 || len(b) < 2 {
			continue
		}
		ax, ay := el.X+a[0], el.Y+a[1]
		bx, by := el.X+b[0], el.Y+b[1]
		d := distToSegment(x, y, ax, ay, bx, by)
		if d < bestD {
			bestD, bestSeg = d, i
		}
	}
	if bestSeg < 0 {
		return false
	}
	// 插在 bestSeg 之前
	pt := []float64{x - el.X, y - el.Y}
	pts := make([][]float64, 0, len(el.Points)+1)
	pts = append(pts, el.Points[:bestSeg]...)
	pts = append(pts, pt)
	pts = append(pts, el.Points[bestSeg:]...)
	el.Points = pts
	return true
}

// distToSegment 返回点到线段的最短距离。
func distToSegment(px, py, ax, ay, bx, by float64) float64 {
	dx, dy := bx-ax, by-ay
	len2 := dx*dx + dy*dy
	if len2 == 0 {
		return math.Hypot(px-ax, py-ay)
	}
	t := ((px-ax)*dx + (py-ay)*dy) / len2
	t = math.Max(0, math.Min(1, t))
	return math.Hypot(px-(ax+t*dx), py-(ay+t*dy))
}

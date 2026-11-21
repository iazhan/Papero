package app

import (
	"math"

	"github.com/egoist/mygo/ui"

	"papero/internal/board"
)

// 手绘抖动：照着 rough.js 的思路，把每条边拆成几个点、各自偏移一点，
// 再叠一遍偏移不同的「重影」，线看起来就是手画的。
//
// 抖动必须**确定性**：同一个 seed 每次画出来一样，否则每帧都在抖，
// 看起来像在水里；同一份文件重开也一样。

// roughRand 是确定性伪随机（线性同余），返回 [-1, 1]。
type roughRand struct{ s int64 }

func newRoughRand(seed int64) *roughRand {
	if seed == 0 {
		seed = 1
	}
	return &roughRand{s: seed}
}

func (r *roughRand) next() float32 {
	r.s = r.s*6364136223846793005 + 1442695040888963407
	v := (r.s >> 33) & 0x7fffffff
	return float32(v%2001)/1000 - 1
}

// roughAmp 按 roughness 级别给出偏移幅度（屏幕像素）。
func roughAmp(roughness int, zoom float32) float32 {
	switch {
	case roughness <= 0:
		return 0
	case roughness == 1:
		return 2.2 * zoom
	default:
		return 3.6 * zoom
	}
}

// roughLine 画一条手绘感的线：两遍，第二遍更细、偏移不同。
func roughLine(p *ui.Painter, x1, y1, x2, y2, width, amp float32, c ui.Color, seed int64) {
	if amp <= 0 {
		path := (&ui.Path{}).MoveTo(x1, y1).LineTo(x2, y2)
		p.StrokePath(path, width, c)
		return
	}
	dx, dy := x2-x1, y2-y1
	for pass := 0; pass < 2; pass++ {
		r := newRoughRand(seed + int64(pass)*7919)
		path := &ui.Path{}
		path.MoveTo(x1+r.next()*amp*0.6, y1+r.next()*amp*0.6)
		// 垂直于线段的方向：让中间那一段往外鼓一点，像手腕的弧
		length := float32(math.Hypot(float64(dx), float64(dy)))
		var nx, ny float32
		if length > 0 {
			nx, ny = -dy/length, dx/length
		}
		bow := amp * 0.9 * r.next()
		const n = 3
		for i := 1; i <= n; i++ {
			t := float32(i) / n
			// 中段偏得多一点，两端收紧，像落笔与收笔
			edge := 1 - absF(t-0.5)*2
			b := bow * edge
			path.LineTo(x1+dx*t+r.next()*amp*edge+nx*b, y1+dy*t+r.next()*amp*edge+ny*b)
		}
		w := width
		if pass == 1 {
			w *= 0.65
		}
		p.StrokePath(path, w, c)
	}
}

// roughPoly 用一串顶点画手绘多边形（矩形、菱形都走这里）。
func roughPoly(p *ui.Painter, pts [][2]float32, width, amp float32, c ui.Color, seed int64, close bool) {
	n := len(pts)
	edges := n - 1
	if close {
		edges = n
	}
	for i := 0; i < edges; i++ {
		a := pts[i]
		b := pts[(i+1)%n]
		roughLine(p, a[0], a[1], b[0], b[1], width, amp, c, seed+int64(i)*131)
	}
}

// roughEllipse 把椭圆采样成若干点再抖。
func roughEllipse(p *ui.Painter, x, y, w, h, width, amp float32, c ui.Color, seed int64) {
	const n = 26
	for pass := 0; pass < 2; pass++ {
		r := newRoughRand(seed + int64(pass)*104729)
		cx, cy := x+w/2, y+h/2
		rx, ry := w/2, h/2
		path := &ui.Path{}
		for i := 0; i <= n; i++ {
			// 首尾角度略微错开，让闭合处有重叠，像真的一笔画过去
			ang := float32(i) / n * 6.2831853
			px := cx + rx*cosF(ang) + r.next()*amp
			py := cy + ry*sinF(ang) + r.next()*amp
			if i == 0 {
				path.MoveTo(px, py)
			} else {
				path.LineTo(px, py)
			}
		}
		wd := width
		if pass == 1 {
			wd *= 0.65
		}
		p.StrokePath(path, wd, c)
	}
}

// roughRect 画手绘矩形。
func roughRect(p *ui.Painter, x, y, w, h, width, amp float32, c ui.Color, seed int64) {
	roughPoly(p, [][2]float32{{x, y}, {x + w, y}, {x + w, y + h}, {x, y + h}}, width, amp, c, seed, true)
}

// roughDiamond 画手绘菱形。
func roughDiamond(p *ui.Painter, x, y, w, h, width, amp float32, c ui.Color, seed int64) {
	roughPoly(p, [][2]float32{
		{x + w/2, y}, {x + w, y + h/2}, {x + w/2, y + h}, {x, y + h/2},
	}, width, amp, c, seed, true)
}

// roughPolyPath 给填充用的规整多边形（填充不抖，抖动只在描边）。
func polyPath(pts [][2]float32) *ui.Path {
	path := &ui.Path{}
	for i, pt := range pts {
		if i == 0 {
			path.MoveTo(pt[0], pt[1])
		} else {
			path.LineTo(pt[0], pt[1])
		}
	}
	return path.Close()
}

func absF(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

// 三角函数用标准库，包一层只是为了让上面的式子读起来短。
func sinF(v float32) float32 { return float32(math.Sin(float64(v))) }
func cosF(v float32) float32 { return float32(math.Cos(float64(v))) }

// elementAmp 取一个元素的抖动幅度。
func elementAmp(el board.Element, zoom float32) float32 {
	return roughAmp(el.Roughness, zoom)
}

package app

import (
	"fmt"
	"math"
	"strings"

	"papero/internal/board"
)

// 白板导出：生成 SVG。
//
// 为什么是 SVG 而不是 PNG：MyGo 的 Painter 只能画在窗口里，没有离屏画布；
// 自己写一个光栅化器不值得。SVG 是纯文本，几何和屏幕上用的是同一套抖动算法，
// 而且 Excalidraw 自己导出的也是 SVG。

// boardSVG 把整个白板转成 SVG 文本。
func boardSVG(scene *board.Scene) string {
	minX, minY, maxX, maxY := scene.Bounds()
	if maxX <= minX || maxY <= minY {
		minX, minY, maxX, maxY = 0, 0, 800, 600
	}
	const pad = 20.0
	w := maxX - minX + pad*2
	h := maxY - minY + pad*2

	var b strings.Builder
	fmt.Fprintf(&b, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" `+
		`viewBox="%.1f %.1f %.1f %.1f">`+"\n", w, h, minX-pad, minY-pad, w, h)
	b.WriteString(`<rect x="` + fmt.Sprintf("%.1f", minX-pad) + `" y="` + fmt.Sprintf("%.1f", minY-pad) +
		`" width="` + fmt.Sprintf("%.1f", w) + `" height="` + fmt.Sprintf("%.1f", h) +
		`" fill="#fdfdfc"/>` + "\n")

	for _, el := range scene.Visible() {
		stroke := el.Stroke
		if stroke == "" {
			stroke = "#1e1e1e"
		}
		width := el.StrokeWidth
		if width <= 0 {
			width = 2
		}
		fill := el.Fill
		hasFill := fill != "" && fill != "transparent"
		amp := float64(roughAmp(el.Roughness, 1))

		switch el.Type {
		case "rectangle", "diamond", "ellipse":
			if hasFill {
				b.WriteString(shapeSVG(el, fill, "none", 0))
			}
			// 抖动线：和屏幕上是同一个算法，只是点写进 SVG 的 polyline
			for _, seg := range roughSegments(el, amp) {
				fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="%.1f" `+
					`stroke-linecap="round"/>`+"\n", seg, stroke, width)
			}
		case "arrow", "line", "freedraw":
			fmt.Fprintf(&b, `<polyline points="%s" fill="none" stroke="%s" stroke-width="%.1f" `+
				`stroke-linecap="round" stroke-linejoin="round"/>`+"\n", pointsSVG(el), stroke, width)
			if el.Type == "arrow" && len(el.Points) >= 2 {
				b.WriteString(arrowSVG(el, stroke, width))
			}
		case "image":
			if f, ok := scene.Files[el.FileID]; ok && f.DataURL != "" {
				fmt.Fprintf(&b, `<image x="%.1f" y="%.1f" width="%.1f" height="%.1f" href="%s" `+
					`preserveAspectRatio="none"/>`+"\n", el.X, el.Y, el.Width, el.Height, escapeXML(f.DataURL))
			}
		case "text":
			size := el.FontSize
			if size <= 0 {
				size = 20
			}
			lines := strings.Split(el.Text, "\n")
			if len(lines) == 1 {
				fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-family="sans-serif" font-size="%.1f" `+
					`fill="%s">%s</text>`+"\n", el.X, el.Y+size*0.9, size, stroke, escapeXML(el.Text))
				break
			}
			// 多行：一行一个 tspan，按行高往下排
			fmt.Fprintf(&b, `<text x="%.1f" y="%.1f" font-family="sans-serif" font-size="%.1f" fill="%s">`,
				el.X, el.Y+size*0.9, size, stroke)
			for i, line := range lines {
				if i == 0 {
					fmt.Fprintf(&b, `<tspan x="%.1f">%s</tspan>`, el.X, escapeXML(line))
				} else {
					fmt.Fprintf(&b, `<tspan x="%.1f" dy="%.1f">%s</tspan>`,
						el.X, size*board.LineHeight, escapeXML(line))
				}
			}
			b.WriteString("</text>\n")
		}
	}
	b.WriteString("</svg>\n")
	return b.String()
}

// shapeSVG 画填充（不抖，抖动只在描边）。
func shapeSVG(el board.Element, fill, stroke string, width float64) string {
	switch el.Type {
	case "rectangle":
		return fmt.Sprintf(`<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" rx="4" fill="%s" stroke="%s"/>`+"\n",
			el.X, el.Y, el.Width, el.Height, fill, stroke)
	case "ellipse":
		return fmt.Sprintf(`<ellipse cx="%.1f" cy="%.1f" rx="%.1f" ry="%.1f" fill="%s" stroke="%s"/>`+"\n",
			el.X+el.Width/2, el.Y+el.Height/2, el.Width/2, el.Height/2, fill, stroke)
	case "diamond":
		pts := [][2]float64{
			{el.X + el.Width/2, el.Y}, {el.X + el.Width, el.Y + el.Height/2},
			{el.X + el.Width/2, el.Y + el.Height}, {el.X, el.Y + el.Height/2},
		}
		return fmt.Sprintf(`<polygon points="%s" fill="%s" stroke="%s"/>`+"\n", polyPoints(pts), fill, stroke)
	}
	return ""
}

// pointsSVG 把元素自己的 points 拼成 polyline 的坐标串。
func pointsSVG(el board.Element) string {
	pts := make([][2]float64, 0, len(el.Points))
	for _, p := range el.Points {
		if len(p) < 2 {
			continue
		}
		pts = append(pts, [2]float64{el.X + p[0], el.Y + p[1]})
	}
	return polyPoints(pts)
}

func polyPoints(pts [][2]float64) string {
	var b strings.Builder
	for i, p := range pts {
		if i > 0 {
			b.WriteByte(' ')
		}
		fmt.Fprintf(&b, "%.1f,%.1f", p[0], p[1])
	}
	return b.String()
}

// roughSegments 用与屏幕相同的抖动算法，算出每条边的折线坐标。
func roughSegments(el board.Element, amp float64) []string {
	if amp <= 0 {
		return nil
	}
	var out []string
	edge := func(x1, y1, x2, y2 float64, seed int64) {
		for pass := 0; pass < 2; pass++ {
			r := newRoughRand(seed + int64(pass)*7919)
			dx, dy := x2-x1, y2-y1
			length := float64(0)
			if dx != 0 || dy != 0 {
				length = math.Sqrt(dx*dx + dy*dy)
			}
			nx, ny := 0.0, 0.0
			if length > 0 {
				nx, ny = -dy/length, dx/length
			}
			bow := amp * 0.9 * float64(r.next())
			pts := [][2]float64{{x1 + float64(r.next())*amp*0.6, y1 + float64(r.next())*amp*0.6}}
			const n = 3
			for i := 1; i <= n; i++ {
				t := float64(i) / n
				edgeK := 1 - absF32(float32(t-0.5))*2
				b := bow * float64(edgeK)
				pts = append(pts, [2]float64{
					x1 + dx*t + float64(r.next())*amp*float64(edgeK) + nx*b,
					y1 + dy*t + float64(r.next())*amp*float64(edgeK) + ny*b,
				})
			}
			out = append(out, polyPoints(pts))
		}
	}

	switch el.Type {
	case "rectangle":
		edge(el.X, el.Y, el.X+el.Width, el.Y, el.Seed)
		edge(el.X+el.Width, el.Y, el.X+el.Width, el.Y+el.Height, el.Seed+131)
		edge(el.X+el.Width, el.Y+el.Height, el.X, el.Y+el.Height, el.Seed+262)
		edge(el.X, el.Y+el.Height, el.X, el.Y, el.Seed+393)
	case "diamond":
		cx, cy := el.X+el.Width/2, el.Y+el.Height/2
		edge(cx, el.Y, el.X+el.Width, cy, el.Seed)
		edge(el.X+el.Width, cy, cx, el.Y+el.Height, el.Seed+131)
		edge(cx, el.Y+el.Height, el.X, cy, el.Seed+262)
		edge(el.X, cy, cx, el.Y, el.Seed+393)
	case "ellipse":
		const n = 26
		for pass := 0; pass < 2; pass++ {
			r := newRoughRand(el.Seed + int64(pass)*104729)
			cx, cy := el.X+el.Width/2, el.Y+el.Height/2
			rx, ry := el.Width/2, el.Height/2
			var pts [][2]float64
			for i := 0; i <= n; i++ {
				ang := float64(i) / n * 6.2831853
				pts = append(pts, [2]float64{
					cx + rx*math.Cos(ang) + float64(r.next())*amp,
					cy + ry*math.Sin(ang) + float64(r.next())*amp,
				})
			}
			out = append(out, polyPoints(pts))
		}
	}
	return out
}

// arrowSVG 画箭头末端的两条斜线。
func arrowSVG(el board.Element, stroke string, width float64) string {
	last := el.Points[len(el.Points)-1]
	prev := el.Points[len(el.Points)-2]
	if len(last) < 2 || len(prev) < 2 {
		return ""
	}
	x1, y1 := el.X+prev[0], el.Y+prev[1]
	x2, y2 := el.X+last[0], el.Y+last[1]
	const size, spread = 14.0, 0.45
	ang := math.Atan2(y2-y1, x2-x1)
	var b strings.Builder
	for _, d := range []float64{-spread, spread} {
		ax := x2 - size*math.Cos(ang+d)
		ay := y2 - size*math.Sin(ang+d)
		fmt.Fprintf(&b, `<line x1="%.1f" y1="%.1f" x2="%.1f" y2="%.1f" stroke="%s" stroke-width="%.1f" `+
			`stroke-linecap="round"/>`+"\n", x2, y2, ax, ay, stroke, width)
	}
	return b.String()
}

func absF32(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func escapeXML(s string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	return r.Replace(s)
}

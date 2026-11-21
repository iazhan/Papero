// Package theme 把用户能改的三个颜色（背景、前景、强调）派生成界面用到的全部颜色。
//
// 只暴露三个入口是刻意的：色板越大，用户越难调出「能看」的一套。
// 其余颜色都由这三个算出来——面板层次朝前景混、次要文字朝背景退、
// 强调色的悬停方向跟着按钮文字走。算不出来的（危险色、投影）写死。
package theme

import (
	"fmt"
	"math"

	"github.com/egoist/mygo/ui"
)

// Input 是用户能改的全部：明暗 + 三个颜色。
type Input struct {
	Dark   bool
	Paper  ui.Color // 背景（纸色）
	Ink    ui.Color // 前景（文字）
	Accent ui.Color // 强调色
}

// Tokens 是界面真正用到的语义色。
type Tokens struct {
	Dark bool

	Paper        ui.Color
	Panel        ui.Color
	PanelHover   ui.Color
	PanelPressed ui.Color

	// Line 是分隔线，LineStrong 是控件轮廓。
	Line       ui.Color
	LineStrong ui.Color

	Ink      ui.Color
	InkMuted ui.Color

	Accent        ui.Color
	AccentHover   ui.Color
	AccentPressed ui.Color
	AccentInk     ui.Color
	// AccentSoft 是「淡强调底」：选中行、激活的页签这类带一点强调色、
	// 又不抢文字的底。它是不透明的——半透明的 Accent.Alpha(k) 会跟着背后
	// 的底色变，同一个「选中」压在纸上和压在面板上是两个颜色。
	AccentSoft ui.Color

	Selection ui.Color
	Shadow    ui.Color
	Danger    ui.Color

	Canvas      ui.Color
	CanvasLine  ui.Color
	BoardSelect ui.Color
	BoardHandle ui.Color

	Page       ui.Color
	PageShadow ui.Color

	CodeBg   ui.Color
	CodeText ui.Color
}

// 派生用的比例，集中在这里方便调。
const (
	panelMix   = 0.04 // 面板比纸色深/亮一点点
	hoverMix   = 0.08
	pressedMix = 0.13
	lineMix    = 0.10
	strongMix  = 0.20
	codeMix    = 0.05
	mutedMix   = 0.42 // 次要文字朝背景退多少
	// softMix 是淡强调底里强调色占的比例。取值来自原来各处写的
	// Accent.Alpha(0.14~0.16)，压在白纸上与它们视觉一致。
	softMix = 0.16
	// 选区是强调色压到低透明度。同一个 alpha 在深色下压出来更弱
	// （底色本来就暗，能抬升的空间小），所以深色给得重一些；
	// 两档都对着「原来那层中性灰的可见度」调的，不是拍脑袋。
	selAlphaLight = 0.16
	selAlphaDark  = 0.36
	// 压出来的那一层至少要和纸色拉开这么多对比度。用户可以把强调色选成
	// 浅黄、米白、纯黑，这些颜色压到 16% 后几乎和纸一样——那就不是选区了。
	selMinLight = 1.15
	selMinDark  = 1.30
)

// Derive 是唯一一处「三个颜色变成一套界面」的地方。
func Derive(in Input) Tokens {
	paper, ink, accent := in.Paper, in.Ink, in.Accent

	t := Tokens{
		Dark:         in.Dark,
		Paper:        paper,
		Panel:        paper.Mix(ink, panelMix),
		PanelHover:   paper.Mix(ink, hoverMix),
		PanelPressed: paper.Mix(ink, pressedMix),
		Line:         paper.Mix(ink, lineMix),
		LineStrong:   paper.Mix(ink, strongMix),
		Ink:          ensureContrast(ink, paper, 4.5),
		InkMuted:     ensureContrast(ink.Mix(paper, mutedMix), paper, 3.0),
		Accent:       accent,
		AccentSoft:   paper.Mix(accent, softMix),
		Danger:       dangerColor(in.Dark),
		Canvas:       paper,
		CanvasLine:   paper.Mix(ink, lineMix),
		BoardSelect:  accent,
		BoardHandle:  paper,
		Page:         paper,
		CodeBg:       paper.Mix(ink, codeMix),
		CodeText:     ensureContrast(ink, paper.Mix(ink, codeMix), 4.5),
	}

	// 强调色上的文字：黑或白，取对比度高的那个
	t.AccentInk = readableOn(accent)
	// 悬停方向跟着按钮文字走：白字就压暗，黑字就提亮
	if t.AccentInk == ui.Hex("#ffffff") {
		t.AccentHover = accent.Mix(ui.Hex("#000000"), 0.12)
		t.AccentPressed = accent.Mix(ui.Hex("#000000"), 0.24)
	} else {
		t.AccentHover = accent.Mix(ui.Hex("#ffffff"), 0.16)
		t.AccentPressed = accent.Mix(ui.Hex("#ffffff"), 0.30)
	}

	// 选区：强调色压到低透明度。旧版是纯中性灰——选中一段正文和按钮、
	// 链接没有任何关系；现在选中的蓝就是主按钮的蓝。
	if in.Dark {
		t.Selection = selectionColor(accent, paper, selAlphaDark, selMinDark, true)
		t.Shadow = ui.Hex("#00000080")
		t.PageShadow = ui.Hex("#00000066")
	} else {
		t.Selection = selectionColor(accent, paper, selAlphaLight, selMinLight, false)
		t.Shadow = ui.Hex("#3a2a1220")
		t.PageShadow = ui.Hex("#00000022")
	}
	return t
}

// selectionColor 是选区底色：强调色的低透明度版本，但要保证压上去能看见。
//
// 不能直接写 accent.Alpha(a)：强调色是用户随便选的，浅黄或米白压在白纸上
// 合成出来是 #fffffa（对比度 1.00），选区等于消失了。所以先算「压出来的那
// 一层」和纸色差多少，不够就把强调色朝远离纸色的方向推，直到拉得开为止。
func selectionColor(accent, paper ui.Color, alpha float32, min float64, dark bool) ui.Color {
	toward := ui.Hex("#000000")
	if dark {
		toward = ui.Hex("#ffffff")
	}
	out := accent
	for i := 0; i < 12; i++ {
		if Contrast(paper.Mix(out, alpha), paper) >= min {
			break
		}
		out = out.Mix(toward, 0.12)
	}
	return out.Alpha(alpha)
}

// dangerColor 是写死的危险色：红色没法从一个任意的强调色推出来，
// 否则用户把强调色设成绿色时，"删除"也会变绿。
func dangerColor(dark bool) ui.Color {
	if dark {
		return ui.Hex("#f07178")
	}
	return ui.Hex("#c42b1c")
}

// ensureContrast 朝远离背景的方向混色，直到对比度达标。
func ensureContrast(fg, bg ui.Color, target float64) ui.Color {
	if Contrast(fg, bg) >= target {
		return fg
	}
	toward := ui.Hex("#000000")
	if Luminance(bg) < 0.35 {
		toward = ui.Hex("#ffffff")
	}
	out := fg
	for i := 0; i < 24; i++ {
		out = out.Mix(toward, 0.08)
		if Contrast(out, bg) >= target {
			return out
		}
	}
	return out
}

// readableOn 在给定底色上选黑或白。
func readableOn(bg ui.Color) ui.Color {
	black, white := ui.Hex("#000000"), ui.Hex("#ffffff")
	if Contrast(black, bg) >= Contrast(white, bg) {
		return black
	}
	return white
}

// Luminance 是 WCAG 的相对亮度。
func Luminance(c ui.Color) float64 {
	ch := func(v uint8) float64 {
		f := float64(v) / 255
		if f <= 0.03928 {
			return f / 12.92
		}
		return math.Pow((f+0.055)/1.055, 2.4)
	}
	return 0.2126*ch(c.R) + 0.7152*ch(c.G) + 0.0722*ch(c.B)
}

// Contrast 是 WCAG 的对比度（1..21）。
func Contrast(a, b ui.Color) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// Issue 是一条对比度不达标的检查结果。
type Issue struct {
	What   string
	Got    float64
	Target float64
}

// Check 检查关键配对。只查「文字压在底色上」这种真会读不清的组合。
func (t Tokens) Check() []Issue {
	pairs := []struct {
		what   string
		fg, bg ui.Color
		target float64
	}{
		{"正文 / 背景", t.Ink, t.Paper, 4.5},
		{"正文 / 面板", t.Ink, t.Panel, 4.5},
		{"次要文字 / 背景", t.InkMuted, t.Paper, 3.0},
		{"代码文字 / 代码底", t.CodeText, t.CodeBg, 4.5},
		{"按钮文字 / 强调色", t.AccentInk, t.Accent, 3.0},
		{"强调色 / 背景", t.Accent, t.Paper, 3.0},
	}
	var out []Issue
	for _, p := range pairs {
		if got := Contrast(p.fg, p.bg); got < p.target {
			out = append(out, Issue{What: p.what, Got: got, Target: p.target})
		}
	}
	return out
}

// Preset 是一套内置取值。
type Preset struct {
	Name  string
	Hint  string
	Light bool
	Input Input
}

// Presets 只有两套：浅色与深色。用户改的三个颜色会覆盖它们。
func Presets() []Preset {
	light := Input{
		Dark:   false,
		Paper:  ui.Hex("#ffffff"),
		Ink:    ui.Hex("#1f1f1e"),
		Accent: ui.Hex("#2563eb"),
	}
	dark := Input{
		Dark:   true,
		Paper:  ui.Hex("#1b1b1a"),
		Ink:    ui.Hex("#ececea"),
		Accent: ui.Hex("#6ea8fe"),
	}
	return []Preset{
		{Name: "浅色", Hint: "白纸黑字", Light: true, Input: light},
		{Name: "深色", Hint: "夜间用", Light: false, Input: dark},
	}
}

// Default 按明暗取内置的那套。
func Default(dark bool) Input {
	for _, p := range Presets() {
		if p.Input.Dark == dark {
			return p.Input
		}
	}
	return Presets()[0].Input
}

// Hex 把颜色写成 #rrggbb。
func Hex(c ui.Color) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }

// ParseColor 读 #rrggbb；空串返回 false。
func ParseColor(s string) (ui.Color, bool) {
	if len(s) != 7 || s[0] != '#' {
		return ui.Color{}, false
	}
	var r, g, b uint8
	if _, err := fmt.Sscanf(s, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return ui.Color{}, false
	}
	return ui.RGBA(r, g, b, 1), true
}

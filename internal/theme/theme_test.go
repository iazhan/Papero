package theme

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestDerivePresets 两套内置主题都能派生，且关键对比度达标。
func TestDerivePresets(t *testing.T) {
	for _, p := range Presets() {
		tk := Derive(p.Input)
		if tk.Paper != p.Input.Paper {
			t.Errorf("%s：背景被改了", p.Name)
		}
		if c := Contrast(tk.Ink, tk.Paper); c < 4.5 {
			t.Errorf("%s：正文对比度 %.2f < 4.5", p.Name, c)
		}
		if c := Contrast(tk.InkMuted, tk.Paper); c < 3.0 {
			t.Errorf("%s：次要文字对比度 %.2f < 3.0", p.Name, c)
		}
		if c := Contrast(tk.AccentInk, tk.Accent); c < 3.0 {
			t.Errorf("%s：按钮文字对比度 %.2f < 3.0", p.Name, c)
		}
	}
}

// TestPanelDirection 面板永远朝前景方向偏离背景：浅色下更深、深色下更亮。
func TestPanelDirection(t *testing.T) {
	light := Derive(Input{Paper: ui.Hex("#ffffff"), Ink: ui.Hex("#1f1f1e"), Accent: ui.Hex("#2563eb")})
	if Luminance(light.Panel) >= Luminance(light.Paper) {
		t.Error("浅色主题下面板应当比背景深")
	}
	dark := Derive(Input{Dark: true, Paper: ui.Hex("#1b1b1a"), Ink: ui.Hex("#ececea"), Accent: ui.Hex("#6ea8fe")})
	if Luminance(dark.Panel) <= Luminance(dark.Paper) {
		t.Error("深色主题下面板应当比背景亮")
	}
}

// TestBadInkCorrected 用户把前景色设得跟背景一样，正文会被修正到能读。
func TestBadInkCorrected(t *testing.T) {
	tk := Derive(Input{
		Paper:  ui.Hex("#f5f5f5"),
		Ink:    ui.Hex("#e0e0e0"),
		Accent: ui.Hex("#2563eb"),
	})
	if c := Contrast(tk.Ink, tk.Paper); c < 4.5 {
		t.Errorf("修正后正文对比度只有 %.2f", c)
	}
	if Hex(tk.Paper) != "#f5f5f5" {
		t.Errorf("背景被改成了 %s", Hex(tk.Paper))
	}
}

// TestAccentInkPicksReadable 强调色深就配白字，浅就配黑字。
func TestAccentInkPicksReadable(t *testing.T) {
	dark := Derive(Input{Paper: ui.Hex("#ffffff"), Ink: ui.Hex("#111111"), Accent: ui.Hex("#1a3a8f")})
	if Hex(dark.AccentInk) != "#ffffff" {
		t.Errorf("深强调色上的文字 = %s，想要白色", Hex(dark.AccentInk))
	}
	pale := Derive(Input{Paper: ui.Hex("#ffffff"), Ink: ui.Hex("#111111"), Accent: ui.Hex("#ffe066")})
	if Hex(pale.AccentInk) != "#000000" {
		t.Errorf("浅强调色上的文字 = %s，想要黑色", Hex(pale.AccentInk))
	}
}

// TestDangerIsFixed 危险色不从强调色来，改强调色不该影响它。
func TestDangerIsFixed(t *testing.T) {
	a := Derive(Input{Paper: ui.Hex("#ffffff"), Ink: ui.Hex("#111111"), Accent: ui.Hex("#16a34a")})
	b := Derive(Input{Paper: ui.Hex("#ffffff"), Ink: ui.Hex("#111111"), Accent: ui.Hex("#7c3aed")})
	if Hex(a.Danger) != Hex(b.Danger) {
		t.Error("危险色跟着强调色变了")
	}
}

// TestCheckReports 明显读不清的搭配要被体检抓到。
func TestCheckReports(t *testing.T) {
	tk := Derive(Input{Paper: ui.Hex("#ffffff"), Ink: ui.Hex("#111111"), Accent: ui.Hex("#ffffe0")})
	issues := tk.Check()
	found := false
	for _, is := range issues {
		if is.What == "强调色 / 背景" {
			found = true
		}
	}
	if !found {
		t.Errorf("淡黄色的强调色压在白底上应当不达标：%+v", issues)
	}
}

// TestAccentSoftIsOpaque 淡强调底是不透明的：它必须压在任何底色上都是
// 同一个颜色，否则「选中」在纸面和面板上会长得不一样。
func TestAccentSoftIsOpaque(t *testing.T) {
	for _, p := range Presets() {
		tk := Derive(p.Input)
		if tk.AccentSoft.A != 255 {
			t.Errorf("%s：淡强调底不是不透明（alpha=%d）", p.Name, tk.AccentSoft.A)
		}
		// 它是纸色朝强调色走 softMix，所以必然夹在两者之间
		want := tk.Paper.Mix(tk.Accent, softMix)
		if Hex(tk.AccentSoft) != Hex(want) {
			t.Errorf("%s：淡强调底 = %s，想要 %s", p.Name, Hex(tk.AccentSoft), Hex(want))
		}
		if Contrast(tk.AccentSoft, tk.Paper) <= 1 {
			t.Errorf("%s：淡强调底和背景没有区别", p.Name)
		}
		if Contrast(tk.AccentSoft, tk.Paper) >= Contrast(tk.Accent, tk.Paper) {
			t.Errorf("%s：淡强调底比强调色还重", p.Name)
		}
	}
}

// TestSelectionFollowsAccent 选区是强调色的低透明度版本，且压上去看得见、
// 不盖住正文。
func TestSelectionFollowsAccent(t *testing.T) {
	for _, p := range Presets() {
		tk := Derive(p.Input)
		a := float32(tk.Selection.A) / 255
		if a <= 0.05 || a >= 0.9 {
			t.Errorf("%s：选区应当半透明，alpha=%.2f", p.Name, a)
		}
		// 两套内置强调色都是蓝的，选区就得偏蓝
		if tk.Selection.B <= tk.Selection.R || tk.Selection.B <= tk.Selection.G {
			t.Errorf("%s：蓝色强调色下的选区 = %s，不带蓝", p.Name, Hex(tk.Selection))
		}
		if c := Contrast(tk.Paper.Mix(tk.Selection, a), tk.Paper); c < 1.15 {
			t.Errorf("%s：选区压在纸上只有 %.2f 的对比度", p.Name, c)
		}
		if c := Contrast(tk.Ink, tk.Paper.Mix(tk.Selection, a)); c < 4.5 {
			t.Errorf("%s：选区上的正文只有 %.2f 的对比度", p.Name, c)
		}
	}
}

// TestSelectionVisibleForAnyAccent 强调色可以被选成任何颜色，包括和纸色
// 几乎一样的浅黄、深色下的纯黑——那时不能让选区消失。
func TestSelectionVisibleForAnyAccent(t *testing.T) {
	cases := []struct {
		name  string
		input Input
		min   float64
	}{
		{"浅色·淡黄", Input{Paper: ui.Hex("#ffffff"), Ink: ui.Hex("#1f1f1e"), Accent: ui.Hex("#ffffe0")}, 1.15},
		{"浅色·白", Input{Paper: ui.Hex("#ffffff"), Ink: ui.Hex("#1f1f1e"), Accent: ui.Hex("#ffffff")}, 1.15},
		{"深色·纯黑", Input{Dark: true, Paper: ui.Hex("#1b1b1a"), Ink: ui.Hex("#ececea"), Accent: ui.Hex("#000000")}, 1.30},
		{"深色·深蓝", Input{Dark: true, Paper: ui.Hex("#1b1b1a"), Ink: ui.Hex("#ececea"), Accent: ui.Hex("#191047")}, 1.30},
	}
	for _, tc := range cases {
		tk := Derive(tc.input)
		a := float32(tk.Selection.A) / 255
		if c := Contrast(tk.Paper.Mix(tk.Selection, a), tk.Paper); c < tc.min {
			t.Errorf("%s：选区压在纸上只有 %.2f 的对比度，等于没有", tc.name, c)
		}
	}
}

// TestParseColor 色值解析。
func TestParseColor(t *testing.T) {
	if c, ok := ParseColor("#1f1f1e"); !ok || Hex(c) != "#1f1f1e" {
		t.Errorf("解析 #1f1f1e 失败：%v %v", c, ok)
	}
	if _, ok := ParseColor("1f1f1e"); ok {
		t.Error("缺 # 不该通过")
	}
	if _, ok := ParseColor("#xyz"); ok {
		t.Error("坏色值不该通过")
	}
}

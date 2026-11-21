package app

import (
	"github.com/egoist/mygo/ui"

	"papero/internal/config"
	"papero/internal/theme"
)

// 界面字体：与 WorkBuddy 一样用系统的无衬线字体，中文优先苹方 / 微软雅黑。
const fontUI = `"Segoe UI Variable Text", "Segoe UI", "PingFang SC", "Hiragino Sans GB", ` +
	`"Microsoft YaHei UI", "Microsoft YaHei", system-ui, sans-serif`

// 源码字体：等宽，优先 Windows 的 Cascadia、macOS 的 SF Mono。
const fontMono = `"Cascadia Mono", "Cascadia Code", "Consolas", "SF Mono", "Menlo", ` +
	`"DejaVu Sans Mono", monospace`

// themeInput 把配置拼成主题的输入：明暗 + 三个颜色。
func themeInput(cfg *config.Settings) theme.Input {
	if cfg == nil {
		cfg = config.Default()
	}
	dark := cfg.Theme == "dark"
	in := theme.Default(dark)
	if c, ok := theme.ParseColor(cfg.ThemePaper); ok {
		in.Paper = c
	}
	if c, ok := theme.ParseColor(cfg.ThemeInk); ok {
		in.Ink = c
	}
	if c, ok := theme.ParseColor(cfg.ThemeAccent); ok {
		in.Accent = c
	}
	return in
}

// reloadTheme 按配置重算 token（改主题后调用）。
func (a *App) reloadTheme() { a.tokens = theme.Derive(themeInput(a.cfg)) }

// paperoTheme 把 token 映射成框架自己的主题，让按钮、输入框、滚动条跟着变。
func paperoTheme(tk theme.Tokens) *ui.Theme {
	th := ui.LightTheme()
	if tk.Dark {
		th = ui.DarkTheme()
	}
	th.Font = fontUI
	th.FontSize = 13
	th.Background = tk.Paper
	th.Surface = tk.Panel
	th.SurfaceHover = tk.PanelHover
	th.SurfacePressed = tk.PanelPressed
	th.Border = tk.Line
	th.Text = tk.Ink
	th.TextMuted = tk.InkMuted
	th.Accent = tk.Accent
	th.AccentHover = tk.AccentHover
	th.AccentPressed = tk.AccentPressed
	th.AccentText = tk.AccentInk
	th.Selection = tk.Selection
	th.Danger = tk.Danger
	return th
}

// chrome 是窗口外壳（侧栏、卡片、标签、状态栏）用到的几个颜色。
//
// 全部来自主题 token；这里只决定「哪个部位用哪个 token」。
type chrome struct {
	window      ui.Color
	card        ui.Color
	cardEdge    ui.Color
	tabBar      ui.Color
	tabActive   ui.Color
	tabHover    ui.Color
	selectColor ui.Color
	text        ui.Color
	muted       ui.Color
	hairline    ui.Color
	glass       ui.Color
	shadow      ui.Color
	accent      ui.Color
	danger      ui.Color
}

// chromeFor 把主题 token 映射成外壳配色。
func chromeFor(tk theme.Tokens) chrome {
	return chrome{
		window:      tk.Paper,
		card:        tk.Paper,
		cardEdge:    tk.Line,
		tabBar:      tk.Paper,
		tabActive:   tk.Paper,
		tabHover:    tk.PanelHover,
		selectColor: tk.PanelPressed,
		text:        tk.Ink,
		muted:       tk.InkMuted,
		hairline:    tk.Line,
		glass:       tk.Paper,
		shadow:      tk.Shadow,
		accent:      tk.Accent,
		danger:      tk.Danger,
	}
}

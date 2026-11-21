package app

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"papero/internal/i18n"
)

// 缩放的口径：两个查看器共用一套上下限与步进（参考 Nexus 的 viewer/zoom.ts）。
//
// 关键是「适应窗口 / 适应宽度」不是一个状态，而是一次**动作**：按下就把算出来的倍率
// 写进 zoom，于是输入框里永远是真实百分比，不会出现「显示适应、其实是 87%」的错觉。
const (
	zoomMin  = 0.1
	zoomMax  = 8.0
	zoomStep = 0.25
)

// clampZoomValue 把倍率夹进区间；算不出来（NaN / Inf / 非正）时回到 1。
func clampZoomValue(v float32) float32 {
	if v != v || v < 0 || v > 1e6 {
		return 1
	}
	if v < zoomMin {
		return zoomMin
	}
	if v > zoomMax {
		return zoomMax
	}
	return v
}

// stepZoomValue 走一档：加法步进，保留两位小数，避免 1.2499999 这种显示。
func stepZoomValue(current, dir float32) float32 {
	next := clampZoomValue(current) + dir*zoomStep
	return clampZoomValue(float32(math.Round(float64(next)*100)) / 100)
}

// zoomPercentText 是输入框里的数字部分（不带 %）。
func zoomPercentText(v float32) string {
	return fmt.Sprintf("%.0f", math.Round(float64(clampZoomValue(v))*100))
}

// fitZoom 是「宽高都放得下」的倍率；量不出来时回到 1。
func fitZoom(naturalW, naturalH, availW, availH float32) float32 {
	if naturalW <= 0 || naturalH <= 0 || availW <= 0 || availH <= 0 {
		return 1
	}
	ratio := availW / naturalW
	if r := availH / naturalH; r < ratio {
		ratio = r
	}
	return clampZoomValue(ratio)
}

// fitWidthZoom 是「按宽度铺满」的倍率，PDF 用这个（页有固定比例）。
func fitWidthZoom(naturalW, availW float32) float32 {
	if naturalW <= 0 || availW <= 0 {
		return 1
	}
	return clampZoomValue(availW / naturalW)
}

// 图片与 PDF 查看器的工具栏，样式照 Nexus 的 `.nexus-*-toolbar`：
//
//   - 32 高、左右内边距 8、间距 2，底色与标签栏同色——三条栏连成一条「文档头带」，
//     下面一条细线划开「栏 → 内容」。
//   - 按钮一律 24×24、圆角 4、透明底，只有图标；悬停才浮底色，选中态是「底色 + 强调色」。
//   - 输入框 4ch 宽、等宽字体、居中、圆角 4，聚焦时描边变强调色。
//   - 分隔线 1×16，上下留 5 让分组读得出来。

// toolbarBar 画工具栏的外框：横条 + 下面一条细线。
func toolbarBar(c *ui.Context, pal chrome, build func()) {
	ui.Column(c).Children(func() {
		ui.Row(c).Height(32).Padding(0, 8).Gap(2).AlignItems(ui.Center).
			Background(pal.card).Children(build)
		ui.Box(c).Height(1).Background(pal.hairline)
	})
}

// toolSep 是工具栏里分隔两组控件的竖线。
func toolSep(c *ui.Context, pal chrome) {
	ui.Box(c).Width(1).Height(16).Margin(0, 5).Background(pal.hairline)
}

// toolButton 是工具栏上的图标按钮：24×24、圆角 4、透明底。
// on 为真表示当前处于这个状态（底色 + 强调色），和 Nexus 的 aria-pressed 一样。
func toolButton(c *ui.Context, pal chrome, icon *ui.SVG, label, tip string, on bool, fn func()) {
	b := ui.ButtonBase(c).Size(24, 24).Radius(4).Label(label).Tooltip(tip).Cursor(ui.CursorPointer)
	if on {
		b.Background(pal.tabHover)
	}
	b.OnClick(fn)
	b.Children(func() {
		color := pal.muted
		if on {
			color = pal.accent
		} else if b.Hovered() {
			color = pal.text
		}
		ui.Icon(c, icon).Size(14, 14).TextColor(color)
	})
}

// toolTextButton 是「1:1」这种用文字表达的按钮：尺寸与图标按钮一致，只是中间换成字。
func toolTextButton(c *ui.Context, pal chrome, label, tip string, on bool, fn func()) {
	b := ui.ButtonBase(c).Height(24).Padding(0, 6).Radius(4).Label(label).Tooltip(tip).
		Cursor(ui.CursorPointer)
	if on {
		b.Background(pal.tabHover)
	}
	b.OnClick(fn)
	b.Children(func() {
		color := pal.muted
		if on {
			color = pal.accent
		} else if b.Hovered() {
			color = pal.text
		}
		ui.Text(c, label).FontSize(10).TextColor(color)
	})
}

// numField 是可以直接敲数字的小输入框的状态：页面、缩放都用它。
//
// 平时画的是一个**居中的文字**（不是输入框）——这样上下左右都是按盒子居中的，
// 不受 TextInput 内部行高影响；点一下才换成真正的输入框并聚焦，
// 回车或失焦后换回来。只为了「居中」这件事，值得多这一个状态。
type numField struct {
	Text    string
	editing bool
	pending bool
	age     int
}

// sync 在用户没在编辑时，把显示值刷新成当前真实值。
func (f *numField) sync(current string) {
	if !f.editing {
		f.Text = current
	}
}

// view 画出这一格：显示态是居中的文字，编辑态是真正的输入框。
func (f *numField) view(c *ui.Context, pal chrome, label, tip string, apply func(string)) {
	if !f.editing {
		box := ui.ButtonBase(c).Width(46).Height(22).Radius(4).Label(label).Tooltip(tip).
			Cursor(ui.CursorPointer)
		box.Background(pal.card)
		box.Border(1, pal.hairline)
		box.OnClick(func() {
			f.editing = true
			f.pending = true
		})
		box.Children(func() {
			ui.Row(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Children(func() {
				ui.Text(c, f.Text).Font(fontMono).FontSize(11.5).TextColor(pal.text)
			})
		})
		return
	}

	el := ui.TextInput(c, &f.Text).Width(46).Height(22).Radius(4).Label(label).Tooltip(tip)
	el.Font(fontMono).FontSize(11.5)
	el.Background(pal.card)
	el.Border(1, pal.hairline)
	el.TextAlign(ui.Center)
	if f.pending {
		el.Focus()
		f.pending = false
		f.age = 0
		return
	}
	f.age++
	if el.Submitted() {
		apply(f.Text)
		f.editing = false
		return
	}
	// 点到别处就退出编辑，回到居中显示态
	if f.age > 1 && !el.Focused() {
		f.editing = false
	}
}

// zoomStepper 是「− [100] % +」这样一组缩放控件，中间那格可以直接输入。
func zoomStepper(c *ui.Context, pal chrome, field *numField, onOut, onIn func(), apply func(string)) {
	toolButton(c, pal, iconZoomOut, i18n.T("viewer.zoom.out"), i18n.T("viewer.zoom.out"), false, onOut)
	field.view(c, pal, i18n.T("viewer.zoom.level"), i18n.T("viewer.zoom.hint"), apply)
	ui.Text(c, "%").Font(fontMono).FontSize(11.5).TextColor(pal.muted).Margin(0, 2)
	toolButton(c, pal, iconZoomIn, i18n.T("viewer.zoom.in"), i18n.T("viewer.zoom.in"), false, onIn)
}

// toolInfo 是工具栏右侧的灰字信息。
func toolInfo(c *ui.Context, pal chrome, text string, maxWidth float32) {
	ui.Text(c, text).Font(fontMono).FontSize(10.5).TextColor(pal.muted).
		SingleLine().Ellipsis("…").MaxWidth(maxWidth)
}

// humanBytes 把字节数写成好读的大小。
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	value := float64(n)
	for _, suffix := range []string{"KB", "MB", "GB"} {
		value /= unit
		if value < unit {
			return fmt.Sprintf("%.1f %s", value, suffix)
		}
	}
	return fmt.Sprintf("%.0f TB", value/unit)
}

// parseZoom 解析用户输入的百分比：「150」「150%」都行；空或「适应」回到 100%。
func parseZoom(text string) (float32, bool) {
	t := strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(text), "%"))
	if t == "" || t == i18n.T("viewer.zoom.fit") {
		return 1, true
	}
	value, err := strconv.ParseFloat(t, 32)
	if err != nil || value <= 0 {
		return 0, false
	}
	return clampZoomValue(float32(value / 100)), true
}

// parsePageNumber 解析页码，超过 0 才算数。
func parsePageNumber(text string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(text))
	if err != nil || n <= 0 {
		return 0, false
	}
	return n, true
}

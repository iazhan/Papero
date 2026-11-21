package app

import (
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"papero/internal/i18n"
	"papero/internal/vault"
)

// 实时排版：在源码编辑器里把 Markdown 的标记「化掉」，让源码读起来接近渲染结果。
//
// 这是走向所见即所得的第一步，但没有引入块模型：Markdown 仍然是唯一真相，
// 我们只是借用编辑器的两个能力——
//
//	SetLineStyles    让标题行换成更大更粗的字体
//	SetReplacements  把 ** * ` ~~ [[ ]] 这些标记藏起来，把 [ ] 画成复选框
//
// 光标所在的行一律保留原始标记（否则没法编辑），离开这一行就又「渲染」。
// 只处理可见行，因此长文档也不会有额外开销。

// livePreview 更新一次实时排版。返回是否处于开启状态。
func (a *App) livePreview(tab *tab) bool {
	st := tab.state
	if st == nil {
		return false
	}
	// 源码模式要看到原样的 Markdown：标题的 #、图片地址、链接都不能藏
	on := tab.kind == vault.KindMarkdown && a.cfg.LivePreview && tab.mode != "source"
	if !on {
		if tab.liveOn {
			st.SetReplacements(nil)
			st.SetLineStyles(nil)
			tab.liveOn = false
		}
		return false
	}
	// 特别大的文稿关掉实时排版，避免每帧扫太多内容
	text := tab.text()
	if len(text) > 512*1024 {
		if tab.liveOn {
			st.SetReplacements(nil)
			st.SetLineStyles(nil)
			tab.liveOn = false
		}
		return false
	}

	first, last := st.VisibleLines()
	caretLine := st.CursorPos().Line
	lines := strings.Split(text, "\n")
	if first < 0 {
		first = 0
	}
	if last >= len(lines) {
		last = len(lines) - 1
	}

	// 行首偏移，用于把行内位置换算成缓冲区偏移
	offsets := make([]int, len(lines)+1)
	for i, line := range lines {
		offsets[i+1] = offsets[i] + len(line) + 1
	}

	var reps []editor.Replacement
	var styles []editor.LineStyle
	inFence := false
	// 围栏要从未处理的行开始判断，先快速扫一遍到 first 之前的围栏状态
	for i := 0; i < first && i < len(lines); i++ {
		if isFenceLine(lines[i]) {
			inFence = !inFence
		}
	}
	for i := first; i <= last && i < len(lines); i++ {
		line := lines[i]
		if isFenceLine(line) {
			inFence = !inFence
			continue
		}
		if inFence {
			continue
		}
		off := offsets[i]
		if level, marks := headingMarks(line); level > 0 {
			styles = append(styles, editor.LineStyle{
				Line:       i,
				Font:       ui.Font{Family: fontUI, Size: headingSize(level, st), Weight: 700},
				LineHeight: 1.45,
			})
			if i != caretLine {
				reps = append(reps, editor.Replacement{Start: off, End: off + marks})
			}
		}
		if i == caretLine {
			// 光标所在行保留原样，方便编辑
			continue
		}
		reps = append(reps, a.lineReplacements(tab, line, off)...)
	}
	st.SetLineStyles(styles)
	st.SetReplacements(reps)
	tab.liveOn = true
	return true
}

// headingMarks 返回标题级别与需要隐藏的标记长度（"## " 三个字节）。
func headingMarks(line string) (level, marks int) {
	for level < len(line) && line[level] == '#' && level < 6 {
		level++
	}
	if level == 0 || level >= len(line) || line[level] != ' ' {
		return 0, 0
	}
	return level, level + 1
}

// headingSize 按标题级别给出字号。
func headingSize(level int, st *editor.State) float32 {
	base := st.Font.Size
	if base <= 0 {
		base = 13
	}
	switch level {
	case 1:
		return base * 1.75
	case 2:
		return base * 1.45
	case 3:
		return base * 1.25
	case 4:
		return base * 1.12
	}
	return base * 1.04
}

// isFenceLine 报告这一行是不是代码围栏。
func isFenceLine(line string) bool {
	trimmed := strings.TrimLeft(line, " \t")
	return strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~")
}

// lineReplacements 算出一行里要隐藏的标记与要画的复选框。
func (a *App) lineReplacements(tab *tab, line string, off int) []editor.Replacement {
	var reps []editor.Replacement
	// 任务框：- [ ] / - [x]
	if start, checked, ok := taskBox(line); ok {
		s := off + start
		reps = append(reps, editor.Replacement{
			Start: s, End: s + 3, Width: 15, Interactive: true,
			Label: map[bool]string{true: i18n.T("task.done"), false: i18n.T("task.todo")}[checked],
			Content: func(c *ui.Context, e ui.Element, r editor.Replacement) {
				a.drawTaskBox(c, tab, r.Start, checked)
			},
		})
	}
	// 行内链接：[文字](地址) 与 [[双链]]
	reps = append(reps, linkReplacements(line, off)...)
	// 强调与行内代码的标记
	reps = append(reps, emphasisReplacements(line, off)...)
	return reps
}

// taskBox 找出任务列表的 [ ] / [x]，返回它在这一行里的位置。
func taskBox(line string) (start int, checked, ok bool) {
	trimmed := strings.TrimLeft(line, " \t")
	indent := len(line) - len(trimmed)
	if !strings.HasPrefix(trimmed, "- [") && !strings.HasPrefix(trimmed, "* [") {
		return 0, false, false
	}
	if len(trimmed) < 6 || trimmed[4] != ']' {
		return 0, false, false
	}
	switch trimmed[3] {
	case ' ':
		return indent + 2, false, true
	case 'x', 'X':
		return indent + 2, true, true
	}
	return 0, false, false
}

// drawTaskBox 画一个可点的复选框，点了就把源码里的 [ ] 改成 [x]。
func (a *App) drawTaskBox(c *ui.Context, tab *tab, start int, checked bool) {
	pal := chromeFor(a.tokens)
	b := ui.ButtonBase(c).Size(14, 14).Radius(3).Label(i18n.T("task.todo")).Cursor(ui.CursorPointer)
	b.Border(1, pal.muted)
	if checked {
		b.Background(pal.accent)
		b.Border(0, ui.Transparent)
	}
	b.OnClick(func() { a.toggleTask(tab, start) })
	b.Children(func() {
		if checked {
			ui.Icon(c, iconCheck).Size(9, 9).TextColor(ui.Hex("#ffffff"))
		}
	})
}

// toggleTask 切换复选框，写回 Markdown 源码。
func (a *App) toggleTask(tab *tab, start int) {
	if tab.buf == nil {
		return
	}
	cur := tab.buf.Slice(start, start+3)
	switch cur {
	case "[ ]":
		tab.buf.Edit(editor.Edit{Start: start, End: start + 3, Text: "[x]"})
	case "[x]", "[X]":
		tab.buf.Edit(editor.Edit{Start: start, End: start + 3, Text: "[ ]"})
	}
}

// linkReplacements 把 [文字](地址) 收成「文字」，把 [[双链]] 收成「双链」。
func linkReplacements(line string, off int) []editor.Replacement {
	var reps []editor.Replacement
	i := 0
	for i < len(line) {
		if i+1 < len(line) && line[i] == '[' && line[i+1] == '[' {
			// 双链：藏掉两对括号
			if end := strings.Index(line[i+2:], "]]"); end >= 0 {
				reps = append(reps,
					editor.Replacement{Start: off + i, End: off + i + 2},
					editor.Replacement{Start: off + i + 2 + end, End: off + i + 2 + end + 2},
				)
				i = i + 2 + end + 2
				continue
			}
			i += 2
			continue
		}
		if line[i] == '[' {
			// 行内链接：找到 ]( 与 )
			if mid := strings.Index(line[i:], "]("); mid >= 0 {
				close := strings.Index(line[i+mid:], ")")
				if close >= 0 {
					reps = append(reps,
						editor.Replacement{Start: off + i, End: off + i + 1},
						editor.Replacement{Start: off + i + mid, End: off + i + mid + close + 1},
					)
					i = i + mid + close + 1
					continue
				}
			}
		}
		i++
	}
	return reps
}

// emphasisReplacements 藏掉 **粗体**、*斜体*、~~删除线~~、`行内代码` 的标记。
func emphasisReplacements(line string, off int) []editor.Replacement {
	var reps []editor.Replacement
	markers := []string{"**", "__", "~~", "*", "_", "`"}
	i := 0
	for i < len(line) {
		matched := ""
		for _, m := range markers {
			if strings.HasPrefix(line[i:], m) {
				matched = m
				break
			}
		}
		if matched == "" {
			i++
			continue
		}
		// 找闭合标记（不能是空内容）
		rest := line[i+len(matched):]
		end := strings.Index(rest, matched)
		if end <= 0 {
			i += len(matched)
			continue
		}
		closeAt := i + len(matched) + end
		reps = append(reps,
			editor.Replacement{Start: off + i, End: off + i + len(matched)},
			editor.Replacement{Start: off + closeAt, End: off + closeAt + len(matched)},
		)
		i = closeAt + len(matched)
	}
	return reps
}

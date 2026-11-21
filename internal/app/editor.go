package app

import (
	"os"
	"path"
	"strings"

	"github.com/egoist/mygo/ui"

	rte "papero/internal/editor"
	"papero/internal/i18n"
	"papero/internal/richdoc"
	"papero/internal/vault"
)

// 富文本编辑：直接用 ArcMD 的 `internal/editor`——整篇一个缓冲区，
// 块选择、输入法、剪贴板、自动配对、斜杠菜单都是它自带的。
//
// 与 Papero 的接缝只有三处：
//   - 文档模型就是 `richdoc.Document`（`editor` 内部用的也是它，包名改成 document 而已），
//     所以大纲、链接图这些读 `tab.rich` 的旧代码不用改；
//   - 保存：`richdoc.ExportMarkdown(tab.edit.Doc)` 写回 `tab.buf`；
//   - 颜色：把 Papero 的调色板翻译成 `rte.Page`。

// editorTab 画出富文本文稿。
func (a *App) editorTab(c *ui.Context, tab *tab) {
	if tab.edit == nil {
		tab.edit = rte.New(tab.buf.String())
		tab.editRev = tab.edit.Revision()
		tab.rich = tab.edit.Doc
	}
	opts := a.editorOptions(c, tab)
	rte.View(c, tab.edit, opts).Grow(1)

	// 全文搜索跳过来：把命中词选中并滚到第一处
	if q := tab.pendingFind; q != "" {
		tab.pendingFind = ""
		tab.edit.FindText(q)
	}

	// 文档变了就导出 Markdown 写回缓冲区（保存、脏标记都靠它）
	if rev := tab.edit.Revision(); rev != tab.editRev {
		tab.editRev = rev
		tab.rich = tab.edit.Doc
		if md := richdoc.ExportMarkdown(tab.edit.Doc); md != tab.buf.String() {
			tab.buf.Set(md)
		}
	}
}

// editorOptions 把应用的调色板与回调交给编辑器。
func (a *App) editorOptions(c *ui.Context, tab *tab) rte.Options {
	pal := chromeFor(a.tokens)
	page := rte.Page{
		Marker:      pal.muted,
		Caption:     pal.muted,
		Placeholder: pal.muted.Alpha(0.75),
		TaskBorder:  pal.hairline,
		TaskDone:    pal.muted,
		QuoteBar:    pal.hairline,
		Rule:        pal.hairline,
		CodeFill:    pal.window,
		CodeBorder:  pal.hairline,
		CodeText:    pal.text,
		CodeHover:   pal.tabHover,
		InlineFill:  pal.tabHover,
		InlineText:  pal.text,
		TableLine:   pal.hairline,
		TableHeader: pal.window,
		Hover:       pal.tabHover,
		TurnActive:  pal.accent,
	}
	// 编辑器自己按 Width 排版（不给就会当成 0，正文挤成一条线）。
	// 取侧栏当前的实际宽度，不是配置值：开合的那 240ms 里两者差着一整个侧栏。
	winW, _ := c.Size()
	side := a.sidebarW * a.sidebarP
	viewW := winW - side - 8
	column := float32(a.cfg.ReadWidth)
	if column > viewW-100 {
		column = viewW - 100
	}
	if column < 200 {
		column = 200
	}
	return rte.Options{
		Theme:      c.Theme(),
		Page:       page,
		Width:      viewW,
		Column:     column,
		FontFamily: fontUI,
		FontSize:   15,
		OpenLink: func(url string) {
			if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
				c.OpenURL(url)
				return
			}
			a.open(strings.TrimPrefix(url, "./"))
		},
		OpenWiki: func(name string) {
			if rel, ok := a.resolveWikiName(name); ok {
				a.open(rel)
			} else {
				a.say(i18n.T("error.doc_not_found") + name)
			}
		},
		WikiSuggest: func(typed string) []rte.WikiItem {
			return a.wikiItems(typed, 8)
		},
		LoadImage: func(url string) rte.ImageResource {
			return a.editorImage(tab, url)
		},
	}
}

// resolveWikiName 把 [[名字]] 解析成相对路径。
func (a *App) resolveWikiName(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	for _, f := range a.allFiles() {
		if strings.TrimSuffix(baseName(f.Rel), ".md") == name {
			return f.Rel, true
		}
	}
	return "", false
}

// wikiItems 是输入 [[ 时给出的候选。
func (a *App) wikiItems(typed string, limit int) []rte.WikiItem {
	typed = strings.ToLower(strings.TrimSpace(typed))
	var out []rte.WikiItem
	for _, f := range a.allFiles() {
		if len(out) >= limit {
			break
		}
		if f.Kind != vault.KindMarkdown {
			continue
		}
		name := strings.TrimSuffix(baseName(f.Rel), ".md")
		if typed != "" && !strings.Contains(strings.ToLower(name), typed) {
			continue
		}
		out = append(out, rte.WikiItem{Name: name, Hint: f.Rel})
	}
	return out
}

// baseName 取路径最后一段。
func baseName(rel string) string {
	if i := strings.LastIndexAny(rel, "/\\"); i >= 0 {
		return rel[i+1:]
	}
	return rel
}

// editorImage 读一张文档里的图片：相对路径按文稿所在目录解析，同步解码。
func (a *App) editorImage(tab *tab, url string) rte.ImageResource {
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		return rte.ImageResource{Problem: rte.ImageRemote}
	}
	if tab.imageCache == nil {
		tab.imageCache = map[string]rte.ImageResource{}
	}
	if r, ok := tab.imageCache[url]; ok {
		return r
	}
	// 相对路径按文稿所在目录解析
	dir := ""
	if i := strings.LastIndex(tab.rel, "/"); i >= 0 {
		dir = tab.rel[:i+1]
	}
	rel := path.Clean(dir + strings.TrimPrefix(url, "./"))
	full, err := a.vault.Abs(rel)
	if err != nil {
		tab.imageCache[url] = rte.ImageResource{Problem: rte.ImageUnreadable, Local: true}
		return tab.imageCache[url]
	}
	data, err := os.ReadFile(full)
	if err != nil {
		res := rte.ImageResource{Problem: rte.ImageMissing, Local: true}
		tab.imageCache[url] = res
		return res
	}
	bmp, err := ui.DecodeBitmap(data)
	if err != nil || bmp == nil {
		res := rte.ImageResource{Problem: rte.ImageInvalid, Local: true}
		tab.imageCache[url] = res
		return res
	}
	w, h := bmp.Size()
	res := rte.ImageResource{Bitmap: bmp, Width: w, Height: h, Local: true}
	tab.imageCache[url] = res
	return res
}

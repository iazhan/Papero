package app

import (
	"sort"
	"strings"

	"github.com/egoist/mygo/ui/editor"

	"papero/internal/i18n"
	"papero/internal/vault"
)

// 输入 [[ 时的自动补全：把库里已有的文稿列出来，选中就补成 [[标题]]。
//
// 补全列表交给 ui/editor 自带的补全弹层（State.ShowCompletions）——
// 它有键盘选择、前缀过滤、在光标处定位，比我们自己画一个浮层稳得多。
// 我们只负责两件事：判断「光标是不是在未闭合的 [[ 里」，以及列出候选。

// wikiCompletionContext 判断光标是否在未闭合的双链里。
// 返回补全的起点（[[ 之后的位置）与已经输入的前缀。
func wikiCompletionContext(text string, cursor int) (start int, query string, ok bool) {
	if cursor < 2 || cursor > len(text) {
		return 0, "", false
	}
	lineStart := strings.LastIndexByte(text[:cursor], '\n') + 1
	idx := strings.LastIndex(text[lineStart:cursor], "[[")
	if idx < 0 {
		return 0, "", false
	}
	start = lineStart + idx + 2
	query = text[start:cursor]
	// 已经闭合、跨行、带别名或锚点，或者太长，都不补全
	if strings.ContainsAny(query, "[]\n|#") || len(query) > 40 {
		return 0, "", false
	}
	return start, query, true
}

// wikiCompletions 列出候选文稿：先按名字匹配，再补一条「新建」。
func (a *App) wikiCompletions(query string) []editor.Completion {
	g := a.ensureGraph()
	type candidate struct {
		title string
		rel   string
		back  int
	}
	var all []candidate
	for _, rel := range g.Order {
		note := g.Notes[rel]
		if note == nil {
			continue
		}
		all = append(all, candidate{title: note.Title, rel: rel, back: len(note.Back) + len(note.Links)})
	}
	// 名字以查询开头的排前面，其次是包含它的
	sort.SliceStable(all, func(i, j int) bool {
		pi := strings.HasPrefix(strings.ToLower(all[i].title), strings.ToLower(query))
		pj := strings.HasPrefix(strings.ToLower(all[j].title), strings.ToLower(query))
		if pi != pj {
			return pi
		}
		if all[i].back != all[j].back {
			return all[i].back > all[j].back
		}
		return all[i].title < all[j].title
	})

	var items []editor.Completion
	seen := map[string]bool{}
	for _, c := range all {
		if len(items) >= 20 {
			break
		}
		if !fuzzyMatch(c.title, query) {
			continue
		}
		seen[strings.ToLower(c.title)] = true
		items = append(items, editor.Completion{
			Label:         c.title,
			Detail:        i18n.T("complete.existing"),
			Kind:          editor.CompletionModule,
			Insert:        c.title + "]]",
			FilterText:    c.title,
			Documentation: c.rel,
		})
	}
	// 允许直接引用一篇还不存在的文稿，点进去时 openWiki 会新建它
	trimmed := strings.TrimSpace(query)
	if trimmed != "" && !seen[strings.ToLower(trimmed)] {
		items = append([]editor.Completion{{
			Label:      trimmed,
			Detail:     i18n.T("complete.new"),
			Kind:       editor.CompletionSnippet,
			Insert:     trimmed + "]]",
			FilterText: trimmed,
		}}, items...)
	}
	return items
}

// updateWikiCompletions 每帧检查一次编辑器的光标位置，决定要不要弹补全。
// 只在 Markdown 文稿、且编辑器有焦点时生效。
func (a *App) updateWikiCompletions(tab *tab) {
	st := tab.state
	if st == nil || tab.kind != vault.KindMarkdown {
		return
	}
	a.applyWikiCompletions(tab, st.Focused())
}

// applyWikiCompletions 是补全的核心：focused 为假时收起列表。
func (a *App) applyWikiCompletions(tab *tab, focused bool) {
	st := tab.state
	if st == nil {
		return
	}
	if !focused {
		if tab.wikiCompletion {
			st.ShowCompletions(-1, nil)
			tab.wikiCompletion = false
		}
		return
	}
	text := tab.text()
	start, query, ok := wikiCompletionContext(text, st.Cursor())
	if !ok {
		if tab.wikiCompletion {
			st.ShowCompletions(-1, nil)
			tab.wikiCompletion = false
			tab.wikiCompletionKey = ""
		}
		return
	}
	key := query
	if tab.wikiCompletion && key == tab.wikiCompletionKey {
		// 没变化就不重复设置，避免每帧重置选中项
		return
	}
	items := a.wikiCompletions(query)
	if len(items) == 0 {
		st.ShowCompletions(-1, nil)
		tab.wikiCompletion = false
		return
	}
	st.ShowCompletions(start, items)
	tab.wikiCompletion = true
	tab.wikiCompletionKey = key
}

package app

import (
	"fmt"
	"net/url"
	"path"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"papero/internal/i18n"
	"papero/internal/vault"
	"papero/internal/wiki"
)

// wikiScheme 是预览里双链的链接前缀。改写后的 Markdown 长这样：
//
//	[显示文字](wiki:%E5%8F%A6%E4%B8%80%E7%AF%87#%E7%AC%AC%E4%BA%8C%E8%8A%82)
//
// 这样 ui/markdown 会把双链当成普通链接渲染，点击仍然回到我们手里。
const wikiScheme = "wiki:"

// wikiDest 把一条双链编码成预览里的链接地址。
func wikiDest(l wiki.Link) string {
	dest := wikiScheme + url.QueryEscape(l.Target)
	if l.Anchor != "" {
		dest += "#" + url.QueryEscape(l.Anchor)
	}
	return dest
}

// parseWikiDest 解出双链目标与锚点。
func parseWikiDest(dest string) (target, anchor string, ok bool) {
	if !strings.HasPrefix(dest, wikiScheme) {
		return "", "", false
	}
	rest := strings.TrimPrefix(dest, wikiScheme)
	if i := strings.IndexByte(rest, '#'); i >= 0 {
		anchor = rest[i+1:]
		rest = rest[:i]
	}
	target, err := url.QueryUnescape(rest)
	if err != nil {
		return "", "", false
	}
	if anchor != "" {
		if a, err := url.QueryUnescape(anchor); err == nil {
			anchor = a
		}
	}
	return target, anchor, target != ""
}

// ensureGraph 在文件树或内容变化后重建链接图。
func (a *App) ensureGraph() *wiki.Graph {
	if a.graph != nil && a.graphStamp == a.stamp {
		return a.graph
	}
	a.graph = wiki.Build(a.tree, a.vault.Read)
	a.graphStamp = a.stamp
	return a.graph
}

// noteOf 返回某个文稿在链接图里的节点。
func (a *App) noteOf(rel string) *wiki.Note {
	if rel == "" {
		return nil
	}
	return a.ensureGraph().Notes[rel]
}

// openWiki 打开一条双链：目标存在就跳过去（带锚点），不存在就新建。
func (a *App) openWiki(target, anchor, from string) {
	g := a.ensureGraph()
	rel := g.Resolve(target, from)
	if rel == "" {
		// 还不存在：在同目录建一篇，标题就是链接的文字。
		dir := path.Dir(from)
		if dir == "." {
			dir = ""
		}
		name := strings.TrimSpace(path.Base(strings.ReplaceAll(target, "\\", "/")))
		name = strings.TrimSuffix(name, ".md")
		if name == "" {
			a.say(i18n.T("link.empty"))
			return
		}
		created := a.vault.UniqueName(dir, name, ".md")
		body := "# " + name + "\n\n"
		if err := a.vault.Create(created, []byte(body)); err != nil {
			a.say(i18n.T("error.doc_create") + err.Error())
			return
		}
		a.refresh()
		a.open(created)
		a.say(fmt.Sprintf(i18n.T("link.created"), name))
		return
	}
	a.open(rel)
	if anchor != "" {
		if tab := a.currentTab(); tab != nil && tab.kind == vault.KindMarkdown {
			if tab.mode != "split" {
				tab.mode = "split"
			}
			tab.md.Anchor = anchor
			tab.md.AnchorVersion++
		}
	}
}

// linkContext 返回某篇文稿里第一处指向 target 的那一行，用作反向链接的摘要。
func (a *App) linkContext(rel, target string) string {
	data, err := a.vault.Read(rel)
	if err != nil {
		return ""
	}
	lower := strings.ToLower(target)
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "[[") {
			continue
		}
		if strings.Contains(strings.ToLower(trimmed), lower) {
			if len([]rune(trimmed)) > 90 {
				trimmed = string([]rune(trimmed)[:90]) + "…"
			}
			return trimmed
		}
	}
	return ""
}

// linksOf 返回当前文稿的出链与反向链接，供侧栏使用。
func (a *App) linksOf() (back, out []*wiki.Note, unresolved []string) {
	tab := a.currentTab()
	if tab == nil || tab.kind != vault.KindMarkdown {
		return nil, nil, nil
	}
	g := a.ensureGraph()
	note := g.Notes[tab.rel]
	if note == nil {
		return nil, nil, nil
	}
	for _, rel := range note.Back {
		if n := g.Notes[rel]; n != nil {
			back = append(back, n)
		}
	}
	for _, rel := range note.Links {
		if n := g.Notes[rel]; n != nil {
			out = append(out, n)
		}
	}
	return back, out, note.Unresolved
}

// graphNodes 返回图谱里要画的节点与边：先放有链接的文稿，
// 再补上零散的（没有任何链接的）文稿，让图谱能看出「哪些还没连起来」。
// 零散文稿最多补到 maxGraphNodes 个，避免大库里画出一片噪音。
func (a *App) graphNodes() ([]*wiki.Note, [][2]string) {
	const maxGraphNodes = 300
	g := a.ensureGraph()
	var nodes []*wiki.Note
	seen := map[string]bool{}
	var edges [][2]string
	for _, rel := range g.Order {
		note := g.Notes[rel]
		if note == nil || (len(note.Links) == 0 && len(note.Back) == 0) {
			continue
		}
		if !seen[rel] {
			seen[rel] = true
			nodes = append(nodes, note)
		}
		for _, to := range note.Links {
			edges = append(edges, [2]string{rel, to})
			if !seen[to] {
				if n := g.Notes[to]; n != nil {
					seen[to] = true
					nodes = append(nodes, n)
				}
			}
		}
	}
	for _, rel := range g.Order {
		if len(nodes) >= maxGraphNodes {
			break
		}
		if seen[rel] {
			continue
		}
		if note := g.Notes[rel]; note != nil {
			seen[rel] = true
			nodes = append(nodes, note)
		}
	}
	return nodes, edges
}

// rewriteLinksAfterRename 在重命名之后修正全库的双链引用，避免留下断链。
//
// oldGraph 必须是重命名之前算出来的链接图：它把「当时每条链指向哪篇」记下来，
// 我们据此判断哪些链需要改。规则：
//
//	链里写了路径（含 /）   → 换成指向新位置的相对路径
//	链里只写了标题         → 换成新标题（改目录时标题没变，不用改）
//
// 别名与锚点原样保留。返回被改动的文件（相对路径）。
func (a *App) rewriteLinksAfterRename(oldGraph *wiki.Graph, oldRel, newRel string) []string {
	if oldGraph == nil {
		return nil
	}
	var changed []string
	for _, rel := range oldGraph.Order {
		data, err := a.vault.Read(rel)
		if err != nil {
			continue
		}
		src := string(data)
		fromDir := path.Dir(rel)
		if fromDir == "." {
			fromDir = ""
		}
		updated := wiki.Replace(src, func(l wiki.Link) (string, bool) {
			target := oldGraph.Resolve(l.Target, rel)
			if target == "" {
				return "", false
			}
			var newTarget string
			switch {
			case target == oldRel:
				newTarget = newRel
			case strings.HasPrefix(target, oldRel+"/"):
				newTarget = newRel + strings.TrimPrefix(target, oldRel)
			default:
				return "", false
			}
			isFile := oldGraph.Notes[oldRel] != nil
			var body string
			if strings.Contains(l.Target, "/") {
				// 路径写法：保持原来「从哪里算起」的风格
				rootStyle := strings.TrimSuffix(l.Target, ".md") == strings.TrimSuffix(target, ".md")
				if rootStyle {
					body = strings.TrimSuffix(newTarget, ".md")
				} else {
					body = relativeLink(fromDir, newTarget)
				}
			} else {
				// 只写标题的引用：只有文稿本身改名才需要动，改目录不用
				if !isFile {
					return "", false
				}
				body = strings.TrimSuffix(path.Base(newTarget), ".md")
			}
			if body == l.Target {
				return "", false
			}
			var b strings.Builder
			b.WriteString("[[" + body)
			if l.Anchor != "" {
				b.WriteString("#" + l.Anchor)
			}
			if l.Alias != "" {
				b.WriteString("|" + l.Alias)
			}
			b.WriteString("]]")
			return b.String(), true
		})
		if updated == src {
			continue
		}
		if err := a.vault.Write(rel, []byte(updated)); err != nil {
			continue
		}
		// 这篇正好开着：把编辑器里的内容也换成新的，避免下次保存又写回旧引用
		for _, t := range a.tabs {
			if t.rel == rel && t.buf != nil && t.text() != updated {
				t.buf.Set(updated)
			}
		}
		changed = append(changed, rel)
	}
	return changed
}

// relativeLink 算出 fromDir 指向 toRel 的链接文字（不带扩展名）。
func relativeLink(fromDir, toRel string) string {
	target := strings.TrimSuffix(toRel, ".md")
	if fromDir == "" {
		return target
	}
	rel, err := filepath.Rel(filepath.FromSlash(fromDir), filepath.FromSlash(target))
	if err != nil {
		return target
	}
	return filepath.ToSlash(rel)
}

// linkMenu 是预览里双链的右键菜单：打开、复制链接，目标不存在时可以直接新建。
func (a *App) linkMenu(t *tab) func(m *ui.Menu, url string) {
	return func(m *ui.Menu, url string) {
		target, anchor, ok := parseWikiDest(url)
		if !ok {
			m.EditItems()
			return
		}
		rel := a.ensureGraph().Resolve(target, t.rel)
		if rel != "" {
			if m.Item(i18n.T("common.open")).Chosen() {
				a.openWiki(target, anchor, t.rel)
			}
			if m.Item(i18n.T("link.reveal")).Chosen() {
				a.reveal(rel)
			}
		} else {
			if m.Item(i18n.T("link.create_doc")).Chosen() {
				a.openWiki(target, "", t.rel)
			}
		}
		if m.Item(i18n.T("link.copy")).Chosen() {
			a.say(i18n.T("link.copied") + target)
		}
		m.Separator()
		m.EditItems()
	}
}

// noteExcerpt 取一篇文稿的标题与开头几行，用于悬停预览。
func (a *App) noteExcerpt(rel string) (title, excerpt string) {
	title = strings.TrimSuffix(path.Base(rel), ".md")
	data, err := a.vault.Read(rel)
	if err != nil {
		return title, ""
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "```") {
			continue
		}
		trimmed = strings.NewReplacer("**", "", "__", "", "[[", "", "]]", "", "- [ ] ", "☐ ", "- [x] ", "☑ ").
			Replace(trimmed)
		lines = append(lines, trimmed)
		if len(lines) >= 4 {
			break
		}
	}
	excerpt = strings.Join(lines, " ")
	if runes := []rune(excerpt); len(runes) > 160 {
		excerpt = string(runes[:160]) + "…"
	}
	return title, excerpt
}

// createMissingLinks 把一批「还没创建」的链接建成空文稿，返回建成几篇。
func (a *App) createMissingLinks(names []string, fromRel string) int {
	dir := path.Dir(fromRel)
	if dir == "." {
		dir = ""
	}
	created := 0
	for _, name := range names {
		n := strings.TrimSpace(path.Base(strings.ReplaceAll(name, "\\", "/")))
		n = strings.TrimSuffix(n, ".md")
		if n == "" {
			continue
		}
		rel := a.vault.UniqueName(dir, n, ".md")
		if err := a.vault.Create(rel, []byte("# "+n+"\n\n")); err != nil {
			continue
		}
		created++
	}
	if created > 0 {
		a.refresh()
	}
	return created
}

// isMarkdownRel 报告一个路径是不是 Markdown 文稿。
func isMarkdownRel(rel string) bool {
	return vault.KindOf(rel) == vault.KindMarkdown
}

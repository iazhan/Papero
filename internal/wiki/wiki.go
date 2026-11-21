// Package wiki 解析 Markdown 里的 [[双链]]，并维护整个知识库的链接图。
//
// 双链写成 [[目标]]、[[目标|别名]] 或 [[目标#标题]]。目标先按相对路径找，
// 找不到再按标题（文件名去掉扩展名）找：同目录优先，其次层级最浅的那个。
// 找不到的目标不是错误——它意味着「这篇还不存在」，点击时新建。
//
// 这个包不依赖界面，也不依赖 vault 的具体实现：给它一个文件树和一个读取函数，
// 它就能算出每个文稿的出链与反向链接。
package wiki

import (
	"path"
	"sort"
	"strings"

	"papero/internal/vault"
)

// Link 是一处 [[双链]]。
type Link struct {
	// Target 是目标（去掉 #标题 与 |别名 之后的部分）。
	Target string
	// Anchor 是 # 后面的标题，可空。
	Anchor string
	// Alias 是 | 后面的显示文字，可空。
	Alias string
	// Start、End 是在原文中的字节区间，含两侧的 [[ ]]。
	Start, End int
}

// Display 返回链在正文里应该显示的文字。
func (l Link) Display() string {
	switch {
	case l.Alias != "":
		return l.Alias
	case l.Anchor != "":
		return l.Target + " › " + l.Anchor
	}
	return l.Target
}

// Parse 找出源码里的全部双链。代码围栏与行内代码里的 [[…]] 不算。
func Parse(src string) []Link {
	var out []Link
	inFence := false
	fenceMark := ""
	for i := 0; i < len(src); i++ {
		// 行首检查代码围栏：围栏那一行整行跳过（它自己的反引号不能当成行内代码）
		if i == 0 || src[i-1] == '\n' {
			lineEnd := strings.IndexByte(src[i:], '\n')
			if lineEnd < 0 {
				lineEnd = len(src) - i
			}
			trimmed := strings.TrimLeft(src[i:i+lineEnd], " \t")
			if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
				toggleFence(&inFence, &fenceMark, trimmed[:3])
				i += lineEnd
				continue
			}
			if inFence {
				i += lineEnd
				continue
			}
		}
		if src[i] == '`' {
			// 行内代码：跳到配对的 ` 之后
			if end := strings.IndexByte(src[i+1:], '`'); end >= 0 {
				i += end + 1
				continue
			}
		}
		if src[i] == '[' && i+1 < len(src) && src[i+1] == '[' {
			if link, end, ok := parseLink(src, i); ok {
				out = append(out, link)
				i = end - 1
			}
		}
	}
	return out
}

// toggleFence 维护代码围栏状态。
func toggleFence(inFence *bool, mark *string, kind string) {
	if !*inFence {
		*inFence = true
		*mark = kind
		return
	}
	if *mark == kind {
		*inFence = false
		*mark = ""
	}
}

// parseLink 从 src[start] 处的 [[ 开始解析一条链。
func parseLink(src string, start int) (Link, int, bool) {
	close := strings.Index(src[start+2:], "]]")
	if close < 0 {
		return Link{}, 0, false
	}
	body := src[start+2 : start+2+close]
	if body == "" || strings.ContainsAny(body, "[]\n") {
		return Link{}, 0, false
	}
	link := Link{Start: start, End: start + 2 + close + 2}
	target := body
	if i := strings.IndexByte(target, '|'); i >= 0 {
		link.Alias = strings.TrimSpace(target[i+1:])
		target = target[:i]
	}
	if i := strings.IndexByte(target, '#'); i >= 0 {
		link.Anchor = strings.TrimSpace(target[i+1:])
		target = target[:i]
	}
	link.Target = strings.TrimSpace(target)
	if link.Target == "" {
		return Link{}, 0, false
	}
	return link, link.End, true
}

// Rewrite 把源码里的双链换成标准 Markdown 链接，目标地址由 dest 决定。
// 这样预览就能直接复用 ui/markdown 的链接渲染与点击处理。
func Rewrite(src string, dest func(Link) string) string {
	links := Parse(src)
	if len(links) == 0 {
		return src
	}
	var b strings.Builder
	last := 0
	for _, link := range links {
		b.WriteString(src[last:link.Start])
		label := strings.NewReplacer("[", `\[`, "]", `\]`).Replace(link.Display())
		b.WriteString("[" + label + "](" + dest(link) + ")")
		last = link.End
	}
	b.WriteString(src[last:])
	return b.String()
}

// Replace 逐条改写双链：fn 返回新的原文（含 [[ ]]）与是否改动。
// 与 Rewrite 不同，它保留 [[…]] 语法，用于改名时修正引用。
func Replace(src string, fn func(Link) (string, bool)) string {
	links := Parse(src)
	if len(links) == 0 {
		return src
	}
	var b strings.Builder
	last := 0
	changed := false
	for _, link := range links {
		replacement, ok := fn(link)
		if !ok || replacement == "" {
			continue
		}
		b.WriteString(src[last:link.Start])
		b.WriteString(replacement)
		last = link.End
		changed = true
	}
	if !changed {
		return src
	}
	b.WriteString(src[last:])
	return b.String()
}

// Note 是链接图里的一个文稿。
type Note struct {
	// Rel 是相对知识库的斜杠路径。
	Rel string
	// Title 是文件名去掉扩展名。
	Title string
	// Links 是这篇文稿指向的目标（已解析成 rel）；指向不存在的文稿时为空字符串。
	Links []string
	// Back 是链接到这篇文稿的其它文稿。
	Back []string
	// Unresolved 是没能解析到任何文稿的目标名，用于提示「还不存在」。
	Unresolved []string
}

// Graph 是整个知识库的链接图。
type Graph struct {
	Notes map[string]*Note
	// Order 是文稿的相对路径，按名字排序，保证界面显示稳定。
	Order []string
}

// Build 扫描文件树，算出链接图。
func Build(tree *vault.Node, read func(rel string) ([]byte, error)) *Graph {
	g := &Graph{Notes: map[string]*Note{}}
	if tree == nil {
		return g
	}
	// 标题 → 候选文稿
	byTitle := map[string][]string{}
	var files []*vault.Node
	for _, f := range tree.Files() {
		if f.Kind != vault.KindMarkdown {
			continue
		}
		files = append(files, f)
		title := strings.ToLower(f.Title())
		byTitle[title] = append(byTitle[title], f.Rel)
		g.Notes[f.Rel] = &Note{Rel: f.Rel, Title: f.Title()}
		g.Order = append(g.Order, f.Rel)
	}
	sort.Strings(g.Order)

	for _, f := range files {
		data, err := read(f.Rel)
		if err != nil {
			continue
		}
		note := g.Notes[f.Rel]
		for _, link := range Parse(string(data)) {
			target := resolve(link.Target, f.Rel, byTitle, g.Notes)
			if target == "" {
				if !contains(note.Unresolved, link.Target) {
					note.Unresolved = append(note.Unresolved, link.Target)
				}
				continue
			}
			if target == f.Rel || contains(note.Links, target) {
				continue
			}
			note.Links = append(note.Links, target)
			if other := g.Notes[target]; other != nil && !contains(other.Back, f.Rel) {
				other.Back = append(other.Back, f.Rel)
			}
		}
	}
	for _, note := range g.Notes {
		sort.Strings(note.Links)
		sort.Strings(note.Back)
	}
	return g
}

// Resolve 把一个双链目标解析成文稿路径，找不到返回空串。
func (g *Graph) Resolve(target, from string) string {
	byTitle := map[string][]string{}
	for rel, note := range g.Notes {
		byTitle[strings.ToLower(note.Title)] = append(byTitle[strings.ToLower(note.Title)], rel)
	}
	return resolve(target, from, byTitle, g.Notes)
}

// resolve 实现目标解析：先路径，再标题（同目录优先，其次层级最浅）。
func resolve(target, from string, byTitle map[string][]string, notes map[string]*Note) string {
	if target == "" {
		return ""
	}
	clean := strings.TrimSuffix(strings.ReplaceAll(target, "\\", "/"), ".md")
	// 1) 相对路径（含目录的写法）
	if strings.Contains(clean, "/") {
		dir := path.Dir(from)
		if dir == "." {
			dir = ""
		}
		for _, candidate := range []string{path.Join(dir, clean), path.Join("/", clean)[1:]} {
			for _, try := range []string{candidate, candidate + ".md"} {
				if _, ok := notes[try]; ok {
					return try
				}
			}
		}
	}
	// 2) 按标题找
	candidates := byTitle[strings.ToLower(path.Base(clean))]
	if len(candidates) == 0 {
		return ""
	}
	dir := path.Dir(from)
	if dir == "." {
		dir = ""
	}
	best := ""
	bestDepth := 1 << 30
	for _, rel := range candidates {
		if path.Dir(rel) == dir && dir != "" {
			return rel
		}
		if depth := strings.Count(rel, "/"); depth < bestDepth {
			best, bestDepth = rel, depth
		}
	}
	return best
}

// contains 报告切片里有没有某个字符串。
func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

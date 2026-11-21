package wiki

import (
	"fmt"
	"path"
	"testing"

	"papero/internal/vault"
)

const sample = `# 首页

链到 [[另一篇]]，带别名 [[另一篇|别名的写法]]，带锚点 [[另一篇#第二节]]。

嵌套目录里的 [[notes/深层]]。

不存在的 [[还没写]]。

` + "```" + `
代码里的 [[不算]]
` + "```" + `

行内代码 ` + "`[[也不算]]`" + ` 之外还有 [[第三篇]]。
`

func TestParse(t *testing.T) {
	links := Parse(sample)
	var targets []string
	for _, l := range links {
		targets = append(targets, l.Target)
	}
	want := []string{"另一篇", "另一篇", "另一篇", "notes/深层", "还没写", "第三篇"}
	if len(targets) != len(want) {
		t.Fatalf("解析出 %d 条链，想要 %d：%v", len(targets), len(want), targets)
	}
	for i := range want {
		if targets[i] != want[i] {
			t.Errorf("第 %d 条 = %q，想要 %q", i, targets[i], want[i])
		}
	}
	// 别名与锚点
	if links[1].Alias != "别名的写法" {
		t.Errorf("别名 = %q", links[1].Alias)
	}
	if links[2].Anchor != "第二节" {
		t.Errorf("锚点 = %q", links[2].Anchor)
	}
	if links[2].Display() != "另一篇 › 第二节" {
		t.Errorf("显示文字 = %q", links[2].Display())
	}
}

func TestRewrite(t *testing.T) {
	src := "看 [[另一篇|别名]] 和 [[第三篇]]。"
	out := Rewrite(src, func(l Link) string { return "wiki:" + l.Target })
	want := "看 [别名](wiki:另一篇) 和 [第三篇](wiki:第三篇)。"
	if out != want {
		t.Errorf("改写 = %q\n想要 %q", out, want)
	}
}

func TestReplace(t *testing.T) {
	src := "看 [[甲]] 和 [[乙|别名]]，代码 `[[甲]]` 不动。"
	out := Replace(src, func(l Link) (string, bool) {
		if l.Target == "甲" {
			return "[[新甲]]", true
		}
		return "", false
	})
	want := "看 [[新甲]] 和 [[乙|别名]]，代码 `[[甲]]` 不动。"
	if out != want {
		t.Errorf("Replace = %q\n想要 %q", out, want)
	}
	// 没有改动时原样返回
	if got := Replace(src, func(Link) (string, bool) { return "", false }); got != src {
		t.Errorf("无改动时应原样返回")
	}
}

func TestParseIgnoresUnclosed(t *testing.T) {
	if links := Parse("[[没闭合 和 [[]] 以及 [[有\n换行]]"); len(links) != 0 {
		t.Errorf("应当解析不到链，得到 %+v", links)
	}
}

// buildTree 造一棵小文件树。
func buildTree(t *testing.T, files map[string]string) (*vault.Node, func(string) ([]byte, error)) {
	t.Helper()
	root := &vault.Node{Name: "vault", Dir: true}
	for rel := range files {
		root.Children = append(root.Children, &vault.Node{
			Name: path.Base(rel), Rel: rel, Kind: vault.KindMarkdown,
		})
	}
	read := func(rel string) ([]byte, error) {
		if body, ok := files[rel]; ok {
			return []byte(body), nil
		}
		return nil, fmt.Errorf("missing %s", rel)
	}
	return root, read
}

func TestBuildGraph(t *testing.T) {
	files := map[string]string{
		"首页.md":       "链到 [[另一篇]] 和 [[深层]]。",
		"另一篇.md":      "回到 [[首页]]。",
		"notes/深层.md": "只有一段话。",
	}
	tree, read := buildTree(t, files)
	g := Build(tree, read)

	home := g.Notes["首页.md"]
	if home == nil {
		t.Fatal("没有首页节点")
	}
	if len(home.Links) != 2 {
		t.Fatalf("首页出链 = %v", home.Links)
	}
	// 「深层」按标题解析到 notes/深层.md
	if home.Links[0] != "notes/深层.md" && home.Links[1] != "notes/深层.md" {
		t.Errorf("深层没有解析到：%v", home.Links)
	}
	other := g.Notes["另一篇.md"]
	if len(other.Back) != 1 || other.Back[0] != "首页.md" {
		t.Errorf("另一篇的反向链接 = %v", other.Back)
	}
	if len(home.Back) != 1 || home.Back[0] != "另一篇.md" {
		t.Errorf("首页的反向链接 = %v", home.Back)
	}
}

func TestUnresolved(t *testing.T) {
	files := map[string]string{"a.md": "[[不存在的]]"}
	tree, read := buildTree(t, files)
	g := Build(tree, read)
	if len(g.Notes["a.md"].Unresolved) != 1 {
		t.Errorf("未解析 = %v", g.Notes["a.md"].Unresolved)
	}
	if g.Resolve("不存在的", "a.md") != "" {
		t.Error("不该解析到任何文稿")
	}
}

func TestResolvePrefersSameDir(t *testing.T) {
	files := map[string]string{
		"notes/a.md":     "[[b]]",
		"notes/b.md":     "x",
		"deep/deep/b.md": "x",
	}
	tree, read := buildTree(t, files)
	g := Build(tree, read)
	if got := g.Resolve("b", "notes/a.md"); got != "notes/b.md" {
		t.Errorf("解析 = %q，想要 notes/b.md", got)
	}
	// 带路径的写法
	if got := g.Resolve("deep/deep/b", "notes/a.md"); got != "deep/deep/b.md" {
		t.Errorf("带路径解析 = %q", got)
	}
}

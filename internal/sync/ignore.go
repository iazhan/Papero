package sync

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Filter 是这次同步要跳过的路径：始终忽略的内部目录，加上用户选的两条——
// 点开头的文件和文件夹、知识库根目录的 .gitignore。
//
// 零值就是「只忽略内部目录」，与加了这两条之前的版本完全一致。
type Filter struct {
	// Dot 忽略 . 开头的文件和文件夹（.obsidian、.marking 这类工具目录）。
	Dot bool
	// Gitignore 按知识库根目录的 .gitignore 忽略。只读根目录这一个文件，
	// 不递归子目录的 .gitignore——那是 git 的行为，我们只要「哪些不同步」。
	Gitignore bool

	rules []ignoreRule
}

// NewFilter 读一次 .gitignore（开了才读，读不到就当没有）。
func NewFilter(root string, dot, gitignore bool) Filter {
	f := Filter{Dot: dot, Gitignore: gitignore}
	if !gitignore {
		return f
	}
	data, err := os.ReadFile(filepath.Join(root, ".gitignore"))
	if err != nil {
		return f
	}
	f.rules = parseGitignore(data)
	return f
}

// Skip 说这个路径要不要跳过。rel 是斜杠分隔的相对路径，isDir 说明最后一段
// 是不是目录（远端列出来的都是文件，传 false）。
//
// 祖先目录被忽略时整棵子树都跳过：这和 git 一致——被排除的目录下的文件没法
// 单独捞回来，`!` 取反也不行。
func (f Filter) Skip(rel string, isDir bool) bool {
	if rel == "" || alwaysIgnored(rel) {
		return true
	}
	if f.Dot && hasDotSegment(rel) {
		return true
	}
	if len(f.rules) == 0 {
		return false
	}
	segs := strings.Split(rel, "/")
	for i := range segs {
		p := strings.Join(segs[:i+1], "/")
		dir := i < len(segs)-1 || isDir
		if f.matchRules(p, dir) {
			return true
		}
	}
	return false
}

// matchRules 按顺序过一遍规则，最后命中的那条说了算（`!` 是取反）。
func (f Filter) matchRules(rel string, isDir bool) bool {
	skip := false
	for _, r := range f.rules {
		if r.match(rel, isDir) {
			skip = !r.negate
		}
	}
	return skip
}

// hasDotSegment 是路径里有没有以 . 开头的一段。
func hasDotSegment(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

// ignoreRule 是 .gitignore 的一条。
type ignoreRule struct {
	pattern string // 去掉 ! 和结尾的 / 之后
	negate  bool
	dirOnly bool
	rooted  bool // 带斜杠：从根目录算，否则按名字匹配任意一层
}

// parseGitignore 只认常用的那些：注释、空行、! 取反、结尾 / 只匹配目录、
// 开头 / 锚定根目录、* ? 与 **。转义（\#、\!）不认。
func parseGitignore(data []byte) []ignoreRule {
	var rules []ignoreRule
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, " \t")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var r ignoreRule
		if strings.HasPrefix(line, "!") {
			r.negate = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			r.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		r.rooted = strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		r.pattern = line
		rules = append(rules, r)
	}
	return rules
}

// match 是这条规则对某个路径（相对根目录）动不动。
func (r ignoreRule) match(rel string, isDir bool) bool {
	if r.dirOnly && !isDir {
		return false
	}
	if r.rooted {
		return matchSegments(strings.Split(r.pattern, "/"), strings.Split(rel, "/"))
	}
	// 不带斜杠的规则匹配任意一层的名字
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	return matchSegments(strings.Split(r.pattern, "/"), []string{base})
}

// matchSegments 是逐段匹配：`**` 吃掉零到多段，其余用 path.Match（`*` 不跨 /）。
func matchSegments(pat, name []string) bool {
	if len(pat) == 0 {
		return len(name) == 0
	}
	if pat[0] == "**" {
		// 结尾的 ** 至少吃掉一段：`drafts/**` 说的是 drafts 里面的东西，
		// drafts 自己不算。中间的 ** 可以吃零段（`a/**/b` 也匹配 `a/b`）。
		if len(pat) == 1 {
			return len(name) >= 1
		}
		if matchSegments(pat[1:], name) {
			return true
		}
		for i := 1; i <= len(name); i++ {
			if matchSegments(pat[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(pat[0], name[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pat[1:], name[1:])
}

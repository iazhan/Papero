package sync

import (
	"os"
	"path/filepath"
	"testing"
)

// 点开头的文件与文件夹：开了就整棵跳过，没开照旧。
func TestFilterDot(t *testing.T) {
	on := Filter{Dot: true}
	skip := []string{".nestnote/app.json", ".marking/x.png", "notes/.hidden.md", ".ok/index"}
	keep := []string{"a.md", "notes/a.md", "notes/.hidden/a.md"}

	for _, rel := range skip {
		if !on.Skip(rel, false) {
			t.Errorf("%q 应当跳过", rel)
		}
	}
	for _, rel := range keep[:2] {
		if on.Skip(rel, false) {
			t.Errorf("%q 不该跳过", rel)
		}
	}
	// 点开头目录下的普通文件名不算点开头，但目录本身会命中，整棵跳过
	if !on.Skip(keep[2], false) {
		t.Error("点开头目录下的文件应当跟着目录一起跳过")
	}

	off := Filter{}
	for _, rel := range skip {
		if off.Skip(rel, false) {
			t.Errorf("没开开关却跳过 %q", rel)
		}
	}
	// 内部目录无论如何都不碰
	for _, rel := range []string{".git/config", "sub/.git/config", ".papero/sync.json", ".trash/a.md"} {
		if !off.Skip(rel, false) {
			t.Errorf("内部路径 %q 必须跳过", rel)
		}
	}
}

// .gitignore 的常用写法：注释、*、取反、目录、锚定、**、带斜杠的路径。
func TestFilterGitignore(t *testing.T) {
	root := t.TempDir()
	rules := `# 注释
*.log
build/
!keep.log
/only-root.md
drafts/**
deep/*.txt
`
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(rules), 0o644); err != nil {
		t.Fatal(err)
	}
	f := NewFilter(root, false, true)

	cases := []struct {
		rel  string
		skip bool
	}{
		{"a.log", true},
		{"deep/b.log", true},        // 不带斜杠的规则匹配任意一层
		{"keep.log", false},         // 后面的 ! 取反
		{"build", true},             // 目录规则，目录本身命中
		{"build/x.png", true},       // 祖先目录命中，整棵跳过
		{"only-root.md", true},      // 开头 / 锚定根目录
		{"sub/only-root.md", false}, // 不是根目录下的那个
		{"drafts/a/b.md", true},     // ** 跨层
		{"drafts", false},           // drafts/** 不匹配空
		{"deep/a.txt", true},        // 带斜杠：从根目录算
		{"x/deep/a.txt", false},     // 不是根目录下的那个 deep
		{"x.log.tmp", true},         // .tmp 是永远忽略的临时文件，与规则无关
		{"notes/a.md", false},
	}
	for _, c := range cases {
		if got := f.Skip(c.rel, !isFileLike(c.rel)); got != c.skip {
			t.Errorf("%q：skip = %v，想要 %v", c.rel, got, c.skip)
		}
	}
}

// isFileLike：测试里的路径都当文件看，只有裸目录名当目录。
func isFileLike(rel string) bool {
	switch rel {
	case "build", "drafts":
		return false
	}
	return true
}

// 没开 .gitignore 时那个文件不影响任何判断。
func TestFilterGitignoreOff(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.md\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	f := NewFilter(root, false, false)
	if f.Skip("a.md", false) {
		t.Error("开关没开，却按 .gitignore 跳过了")
	}
}

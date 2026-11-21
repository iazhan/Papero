package vault

import (
	"os"
	"path/filepath"
	"testing"
)

func TestKindOf(t *testing.T) {
	cases := map[string]Kind{
		"笔记.md":         KindMarkdown,
		"a.markdown":    KindMarkdown,
		"readme.txt":    KindText,
		"main.go":       KindText,
		"photo.PNG":     KindImage,
		"论文.pdf":        KindPDF,
		"报告.docx":       KindWord,
		"画布.excalidraw": KindBoard,
		"archive.zip":   KindOther,
	}
	for name, want := range cases {
		if got := KindOf(name); got != want {
			t.Errorf("KindOf(%q) = %v，想要 %v", name, got, want)
		}
	}
}

func TestScanAndRead(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
	os.WriteFile(filepath.Join(dir, "notes", "a.md"), []byte("# 标题"), 0o644)
	os.WriteFile(filepath.Join(dir, "图片.png"), []byte("png"), 0o644)
	os.MkdirAll(filepath.Join(dir, ".git"), 0o755)
	os.WriteFile(filepath.Join(dir, ".git", "config"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(dir, ".hidden.md"), []byte("x"), 0o644)

	v, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	tree, err := v.Scan()
	if err != nil {
		t.Fatal(err)
	}
	notes, files := tree.Stats()
	if notes != 1 || files != 2 {
		t.Fatalf("统计 = %d 篇 / %d 个文件，想要 1 / 2", notes, files)
	}
	// .git 与隐藏文件不出现
	if tree.Find(".hidden.md") != nil {
		t.Error("隐藏文件不应出现在树里")
	}
	node := tree.Find("notes/a.md")
	if node == nil {
		t.Fatal("找不到 notes/a.md")
	}
	if node.Kind != KindMarkdown || node.Title() != "a" {
		t.Errorf("节点 = %+v", node)
	}
	data, err := v.Read("notes/a.md")
	if err != nil || string(data) != "# 标题" {
		t.Fatalf("读取 = %q, %v", data, err)
	}
	// 目录排在文件前面
	if len(tree.Children) < 2 || !tree.Children[0].Dir {
		t.Errorf("排序不对：%+v", tree.Children)
	}
}

func TestPathEscape(t *testing.T) {
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"../evil.md", "a/../../evil.md", "..\\evil.md"} {
		if _, err := v.Abs(bad); err == nil {
			t.Errorf("路径 %q 应当被拒绝", bad)
		}
	}
	if _, err := v.Abs("notes/ok.md"); err != nil {
		t.Errorf("正常路径被拒绝：%v", err)
	}
}

func TestWriteCreateRenameRemove(t *testing.T) {
	dir := t.TempDir()
	v, _ := Open(dir)
	if err := v.Write("a/b.md", []byte("一")); err != nil {
		t.Fatal(err)
	}
	if err := v.Create("a/b.md", []byte("二")); err == nil {
		t.Error("已存在的文件不应被 Create 覆盖")
	}
	if err := v.Rename("a/b.md", "a/c.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "c.md")); err != nil {
		t.Fatalf("重命名后文件不存在：%v", err)
	}
	if err := v.Remove("a/c.md"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "a", "c.md")); !os.IsNotExist(err) {
		t.Error("删除后文件仍然存在")
	}
	// 删除进回收站，可以恢复
	entries, _ := os.ReadDir(filepath.Join(dir, ".trash"))
	if len(entries) != 1 {
		t.Errorf("回收站里有 %d 个条目，想要 1", len(entries))
	}
}

func TestUniqueName(t *testing.T) {
	v, _ := Open(t.TempDir())
	v.Write("新文稿.md", []byte("x"))
	got := v.UniqueName("", "新文稿", ".md")
	if got != "新文稿 2.md" {
		t.Errorf("UniqueName = %q", got)
	}
}

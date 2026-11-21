// Package vault 管理一个知识库目录：扫描成树、按类型识别文稿、读写文件。
// 知识库就是普通的文件夹，文稿就是普通的 .md 文件，Papero 不引入私有格式，
// 因此它随时可以和 Obsidian、VS Code、Git 共用同一个目录。
//
// 这个包不依赖 MyGo，可以脱离窗口测试。
package vault

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// 扫描上限，避免误把巨大的目录当成知识库拖慢启动。
const (
	maxEntries = 20000
	maxDepth   = 32
)

// 扫描时跳过的目录名：版本控制、依赖与系统垃圾。
var skipDirs = map[string]bool{
	".git": true, ".hg": true, ".svn": true, "node_modules": true,
	".obsidian": true, ".trash": true, "__pycache__": true,
	".venv": true, "vendor": true, ".idea": true,
}

// ErrEscape 表示路径试图离开知识库根目录。
var ErrEscape = errors.New("vault: 路径越界")

// Vault 是一个知识库目录。
type Vault struct {
	root string
}

// Open 打开（必要时创建）一个知识库目录。
func Open(root string) (*Vault, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &Vault{root: abs}, nil
}

// Root 返回知识库的绝对路径。
func (v *Vault) Root() string { return v.root }

// Abs 把相对路径（斜杠分隔）转换成绝对路径，并拒绝越界。
func (v *Vault) Abs(rel string) (string, error) {
	rel = strings.ReplaceAll(rel, "\\", "/")
	if hasDotDot(rel) {
		return "", ErrEscape
	}
	clean := path.Clean("/" + rel)
	if clean == "/" {
		return v.root, nil
	}
	abs := filepath.Join(v.root, filepath.FromSlash(strings.TrimPrefix(clean, "/")))
	// 再做一次保险：解析后的路径必须在根目录之内。
	if r, err := filepath.Rel(v.root, abs); err != nil || strings.HasPrefix(r, "..") {
		return "", ErrEscape
	}
	return abs, nil
}

// hasDotDot 报告路径里是否有 ".." 段。
func hasDotDot(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// Read 读取文件内容。
func (v *Vault) Read(rel string) ([]byte, error) {
	abs, err := v.Abs(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(abs)
}

// Write 原子写入文件：先写同目录临时文件再改名，避免半截文件。
func (v *Vault) Write(rel string, data []byte) error {
	abs, err := v.Abs(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".papero-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, abs); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}

// Create 新建文件，目标已存在时报错，避免覆盖别人的文稿。
func (v *Vault) Create(rel string, data []byte) error {
	abs, err := v.Abs(rel)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(abs, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(data)
	return err
}

// Mkdir 新建目录。
func (v *Vault) Mkdir(rel string) error {
	abs, err := v.Abs(rel)
	if err != nil {
		return err
	}
	return os.MkdirAll(abs, 0o755)
}

// Rename 重命名或移动文件/目录，目标已存在时报错。
func (v *Vault) Rename(from, to string) error {
	a, err := v.Abs(from)
	if err != nil {
		return err
	}
	b, err := v.Abs(to)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(b); err == nil {
		return fmt.Errorf("vault: %s 已存在", to)
	}
	if err := os.MkdirAll(filepath.Dir(b), 0o755); err != nil {
		return err
	}
	return os.Rename(a, b)
}

// Remove 删除文件或目录（移入知识库的 .trash 目录，可恢复）。
func (v *Vault) Remove(rel string) error {
	abs, err := v.Abs(rel)
	if err != nil {
		return err
	}
	if abs == v.root {
		return errors.New("vault: 不能删除知识库根目录")
	}
	trash := filepath.Join(v.root, ".trash")
	if err := os.MkdirAll(trash, 0o755); err != nil {
		return err
	}
	name := strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + filepath.Base(abs)
	return os.Rename(abs, filepath.Join(trash, name))
}

// UniqueName 在 dir 下为一个新文稿找一个不冲突的名字，如「未命名 2.md」。
func (v *Vault) UniqueName(dir, base, ext string) string {
	rel := path.Join(dir, base+ext)
	for i := 2; ; i++ {
		if abs, err := v.Abs(rel); err == nil {
			if _, err := os.Stat(abs); errors.Is(err, fs.ErrNotExist) {
				return rel
			}
		}
		rel = path.Join(dir, fmt.Sprintf("%s %d%s", base, i, ext))
	}
}

// Node 是目录树上的一个节点。
type Node struct {
	Name     string
	Rel      string // 相对知识库的斜杠路径
	Dir      bool
	Kind     Kind
	Size     int64
	Mod      time.Time
	Children []*Node
}

// Title 返回节点在侧栏显示的名字：Markdown 去掉扩展名。
func (n *Node) Title() string {
	if n.Dir {
		return n.Name
	}
	if n.Kind == KindMarkdown {
		return strings.TrimSuffix(n.Name, path.Ext(n.Name))
	}
	return n.Name
}

// Scan 扫描整个知识库，返回根节点。符号链接不跟随，隐藏文件不显示。
func (v *Vault) Scan() (*Node, error) {
	info, err := os.Stat(v.root)
	if err != nil {
		return nil, err
	}
	root := &Node{Name: filepath.Base(v.root), Rel: "", Dir: true, Mod: info.ModTime()}
	count := 0
	if err := v.scan(root, v.root, 0, &count); err != nil {
		return nil, err
	}
	sortNode(root)
	return root, nil
}

func (v *Vault) scan(n *Node, dir string, depth int, count *int) error {
	if depth > maxDepth {
		return nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		// 权限问题不应该让整棵树失败，跳过这个目录。
		return nil
	}
	for _, e := range entries {
		if *count >= maxEntries {
			return nil
		}
		name := e.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		if e.IsDir() && skipDirs[name] {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		rel := name
		if n.Rel != "" {
			rel = path.Join(n.Rel, name)
		}
		child := &Node{Name: name, Rel: rel, Dir: e.IsDir(), Mod: info.ModTime(), Size: info.Size()}
		*count++
		if child.Dir {
			if err := v.scan(child, filepath.Join(dir, name), depth+1, count); err != nil {
				return err
			}
		} else {
			child.Kind = KindOf(name)
		}
		n.Children = append(n.Children, child)
	}
	return nil
}

// sortNode 让目录排在文件前面，同类按名字排序（忽略大小写）。
func sortNode(n *Node) {
	sort.SliceStable(n.Children, func(i, j int) bool {
		a, b := n.Children[i], n.Children[j]
		if a.Dir != b.Dir {
			return a.Dir
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	for _, c := range n.Children {
		if c.Dir {
			sortNode(c)
		}
	}
}

// Find 在树上按相对路径找节点。
func (n *Node) Find(rel string) *Node {
	if rel == "" || rel == n.Rel {
		return n
	}
	for _, c := range n.Children {
		if c.Rel == rel {
			return c
		}
		if c.Dir && strings.HasPrefix(rel, c.Rel+"/") {
			return c.Find(rel)
		}
	}
	return nil
}

// Files 按显示顺序遍历所有文件节点。
func (n *Node) Files() []*Node {
	var out []*Node
	var walk func(*Node)
	walk = func(x *Node) {
		for _, c := range x.Children {
			if c.Dir {
				walk(c)
			} else {
				out = append(out, c)
			}
		}
	}
	walk(n)
	return out
}

// Stamp 返回目录树的廉价指纹（路径、大小、修改时间），用来发现外部改动。
func (v *Vault) Stamp() string {
	root, err := v.Scan()
	if err != nil {
		return ""
	}
	h := fnv.New64a()
	for _, f := range root.Files() {
		fmt.Fprintf(h, "%s|%d|%d\n", f.Rel, f.Size, f.Mod.UnixNano())
	}
	return strconv.FormatUint(h.Sum64(), 16)
}

// Stats 统计知识库规模，用于状态栏。
func (n *Node) Stats() (notes, files int) {
	for _, f := range n.Files() {
		files++
		if f.Kind == KindMarkdown {
			notes++
		}
	}
	return notes, files
}

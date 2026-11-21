// Package plugin 定义 Papero 的插件清单、安装与宿主契约。
//
// 插件是一个带 papero-plugin.json 的文件夹（分发时打包成 zip），运行时是一个
// 独立可执行文件，用 JSON-RPC 2.0 over stdio 与宿主通信（像语言服务器那样）。
//
// **插件是第三方扩展，不是内置功能。** 第一方能力（Markdown、PDF、Word、白板）
// 都编译进 Papero 本体：它们要和文稿列表、标签、保存流程长在一起，做成插件
// 只是多一层没有意义的间接。插件则用来加 Papero 本体不该背的东西——AI 写作、
// 导出增强、某个特定格式的转换。
//
// 插件永远拿不到宿主画布，也不会得到 WebView：它贡献命令与面板内容（Markdown），
// 由 Papero 按自己的样式绘制。这样每个插件的观感一致，也无法伪装成 Papero 的界面。
package plugin

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Manifest 是 papero-plugin.json 的内容。
type Manifest struct {
	// ID 是反向域名，全小写，也是插件的文件夹名。
	ID          string `json:"id"`
	Name        string `json:"name"`
	Version     string `json:"version"`
	Description string `json:"description"`
	Author      string `json:"author"`
	License     string `json:"license"`
	// Engine 是要求的最低 Papero 版本。
	Engine string `json:"engine"`
	// Runtime 目前只有 process。
	Runtime string `json:"runtime"`
	// Main 是插件目录内的入口：进程插件是可执行文件，内置插件留空。
	Main string `json:"main"`
	// Permissions 是插件申请的权限，安装前展示给用户。
	Permissions []string `json:"permissions"`

	Contributes Contributes `json:"contributes"`

	// Dir 是插件在本机的位置，由 Manager 填充。
	Dir string `json:"-"`
}

// Contributes 是插件向宿主贡献的东西。
type Contributes struct {
	Commands []Command `json:"commands"`
	Panels   []Panel   `json:"panels"`
	// Viewers 让插件认领新的文件类型：Kind 为 vault.Kind 的标识，
	// 或 Exts 里的扩展名。
	Viewers []Viewer `json:"viewers"`
}

// Command 是出现在命令面板里的一个动作。
type Command struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// When 是 ""、document 或 selection。
	When string `json:"when"`
}

// Panel 是侧栏或浮层里的一个面板，内容由插件以 Markdown 返回。
type Panel struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Icon  string `json:"icon"`
}

// Viewer 声明插件能查看的文件类型。
type Viewer struct {
	Kind  string   `json:"kind"`
	Exts  []string `json:"exts"`
	Title string   `json:"title"`
}

// 权限常量，Manifest.Permissions 只能取这些值。
var Permissions = map[string]string{
	"vault:read":     "读取知识库中的文稿",
	"vault:write":    "修改知识库中的文稿",
	"document:read":  "读取当前打开的文稿",
	"document:write": "修改当前打开的文稿",
	"network":        "访问网络",
	"secrets":        "读写本机保存的密钥",
	"ui:panel":       "在侧栏显示面板",
}

// ErrInvalid 表示清单不合法。
var ErrInvalid = errors.New("插件清单不合法")

// Validate 检查清单，返回第一个问题。
func (m *Manifest) Validate() error {
	if m.ID == "" || strings.ToLower(m.ID) != m.ID {
		return fmt.Errorf("%w：id 必须是小写的反向域名", ErrInvalid)
	}
	if strings.ContainsAny(m.ID, " /\\:") {
		return fmt.Errorf("%w：id 不能包含空格或路径分隔符", ErrInvalid)
	}
	if m.Name == "" {
		return fmt.Errorf("%w：缺少 name", ErrInvalid)
	}
	if m.Version == "" {
		return fmt.Errorf("%w：缺少 version", ErrInvalid)
	}
	if m.Runtime != "process" {
		return fmt.Errorf("%w：runtime 只能是 process", ErrInvalid)
	}
	if m.Main == "" {
		return fmt.Errorf("%w：插件需要 main（可执行文件）", ErrInvalid)
	}
	for _, p := range m.Permissions {
		if _, ok := Permissions[p]; !ok {
			return fmt.Errorf("%w：未知权限 %q", ErrInvalid, p)
		}
	}
	for _, c := range m.Contributes.Commands {
		if c.ID == "" || c.Title == "" {
			return fmt.Errorf("%w：命令需要 id 与 title", ErrInvalid)
		}
		switch c.When {
		case "", "document", "selection":
		default:
			return fmt.Errorf("%w：命令 %s 的 when 只能是 document 或 selection", ErrInvalid, c.ID)
		}
	}
	for _, v := range m.Contributes.Viewers {
		if v.Kind == "" && len(v.Exts) == 0 {
			return fmt.Errorf("%w：查看器需要 kind 或 exts", ErrInvalid)
		}
	}
	return nil
}

// Has 报告插件是否申请了某个权限。
func (m *Manifest) Has(permission string) bool {
	for _, p := range m.Permissions {
		if p == permission {
			return true
		}
	}
	return false
}

// Manager 管理本机安装的插件。
type Manager struct {
	dir    string
	loaded []*Manifest
}

// NewManager 创建插件管理器，插件安装在 dir 下。
func NewManager(dir string) *Manager {
	return &Manager{dir: dir}
}

// Dir 返回插件目录。
func (m *Manager) Dir() string { return m.dir }

// Load 扫描插件目录，返回全部插件。
func (m *Manager) Load() ([]*Manifest, error) {
	out := make([]*Manifest, 0, 8)
	entries, err := os.ReadDir(m.dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return out, err
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		man, err := readManifest(filepath.Join(m.dir, e.Name()))
		if err != nil {
			continue // 坏掉的插件不影响其它插件
		}
		out = append(out, man)
	}
	m.loaded = out
	return out, nil
}

// List 返回上次 Load 的结果。
func (m *Manager) List() []*Manifest { return m.loaded }

// Find 按 id 查找插件。
func (m *Manager) Find(id string) *Manifest {
	for _, p := range m.loaded {
		if p.ID == id {
			return p
		}
	}
	return nil
}

// readManifest 读取一个插件目录的清单。
func readManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, "papero-plugin.json"))
	if err != nil {
		return nil, err
	}
	var man Manifest
	if err := json.Unmarshal(data, &man); err != nil {
		return nil, err
	}
	if err := man.Validate(); err != nil {
		return nil, err
	}
	man.Dir = dir
	return &man, nil
}

// InstallOptions 是安装插件的参数。
type InstallOptions struct {
	// SHA256 是期望的 zip 摘要；为空时不校验。
	SHA256 string
	// MaxSize 是解压后的总字节上限，0 表示 64 MiB。
	MaxSize int64
}

// Install 从 zip 数据安装插件：校验清单、拒绝越界条目、原子替换旧版本。
func (m *Manager) Install(data []byte, opts InstallOptions) (*Manifest, error) {
	if opts.SHA256 != "" {
		sum := sha256.Sum256(data)
		if !strings.EqualFold(hex.EncodeToString(sum[:]), strings.TrimSpace(opts.SHA256)) {
			return nil, errors.New("插件包的 SHA-256 与期望不一致")
		}
	}
	limit := opts.MaxSize
	if limit <= 0 {
		limit = 64 << 20
	}
	zr, err := zip.NewReader(strings.NewReader(string(data)), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("不是有效的 zip：%w", err)
	}

	staging, err := os.MkdirTemp(m.dir, ".install-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(staging)

	var total int64
	for _, f := range zr.File {
		name := strings.ReplaceAll(f.Name, "\\", "/")
		if strings.HasPrefix(name, "/") || strings.Contains(name, "..") {
			return nil, fmt.Errorf("插件包含不安全的路径：%s", f.Name)
		}
		if f.FileInfo().Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("插件包含符号链接：%s", f.Name)
		}
		total += int64(f.UncompressedSize64)
		if total > limit {
			return nil, errors.New("插件解压后超过大小上限")
		}
		target := filepath.Join(staging, filepath.FromSlash(path.Clean(name)))
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return nil, err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		out, err := os.Create(target)
		if err != nil {
			rc.Close()
			return nil, err
		}
		_, err = io.Copy(out, io.LimitReader(rc, limit))
		out.Close()
		rc.Close()
		if err != nil {
			return nil, err
		}
	}

	man, err := readManifest(staging)
	if err != nil {
		return nil, fmt.Errorf("插件清单不可用：%w", err)
	}
	dest := filepath.Join(m.dir, man.ID)
	old := ""
	if _, err := os.Stat(dest); err == nil {
		old = dest + ".old"
		os.RemoveAll(old)
		if err := os.Rename(dest, old); err != nil {
			return nil, err
		}
	}
	if err := os.Rename(staging, dest); err != nil {
		if old != "" {
			os.Rename(old, dest)
		}
		return nil, err
	}
	os.RemoveAll(old)
	man.Dir = dest
	return man, nil
}

// Uninstall 删除一个插件。
func (m *Manager) Uninstall(id string) error {
	if strings.ContainsAny(id, "/\\") || id == "" {
		return errors.New("非法的插件 id")
	}
	return os.RemoveAll(filepath.Join(m.dir, id))
}

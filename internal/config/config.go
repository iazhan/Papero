// Package config 保存 Papero 的本机设置：知识库位置、外观、同步与插件开关。
// 设置写在系统配置目录下，与知识库本身分离，方便知识库整体同步或放进 Git。
package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// SyncSettings 是一套同步远端的配置。远端类型决定哪些字段有意义：
//
//	dir     本地/网盘文件夹（iCloud、OneDrive、Syncthing 都可）
//	git     GitHub 仓库，用 git 命令行推送与拉取
type SyncSettings struct {
	Enabled bool   `json:"enabled"`
	Remote  string `json:"remote"` // dir | git | repo | webdav
	Dir     string `json:"dir"`    // remote=dir 时的目标文件夹
	Repo    string `json:"repo"`   // remote=git 时的 owner/name；remote=repo 时不用
	Branch  string `json:"branch"` // remote=git 时的分支，默认 main；remote=repo 时空 = 当前分支
	Token   string `json:"token"`  // GitHub 令牌（仓库的 Contents 读写权限）
	// remote=webdav 时的地址与账号。用户名空 = 不要登录。密码只能是原文，
	// 它要发给服务器；和令牌一样存在本机配置里。
	URL      string `json:"url,omitempty"`
	User     string `json:"user,omitempty"`
	Password string `json:"password,omitempty"`
	// IgnoreDot 不同步 . 开头的文件和文件夹（.obsidian、.marking 这类工具目录）。
	IgnoreDot bool `json:"ignoreDot,omitempty"`
	// IgnoreGitignore 按知识库根目录的 .gitignore 忽略。
	IgnoreGitignore bool `json:"ignoreGitignore,omitempty"`
	// PullOnOpen 是打开 Papero 时先从云端拉一遍（多台设备编同一个库时有用）：
	// 只下载，不上传、不删除。
	PullOnOpen bool `json:"pullOnOpen,omitempty"`
	// IntervalSec 是两次自动同步之间的间隔，0 表示只手动同步。
	IntervalSec int `json:"intervalSec"`
}

// Settings 是本机设置。
type Settings struct {
	// Vault 是知识库根目录，所有文稿都是其中普通的 .md 与附件文件。
	Vault string `json:"vault"`
	// Theme 是 light、dark 或 system。
	Theme string `json:"theme"`
	// Language 是界面语言："zh-Hans" 或 "en"。空 = 中文（界面原文就是中文）。
	//
	// 没有「跟随系统」这一档：探测系统语言要平台 API（Windows 的
	// GetUserDefaultUILanguage），做不到就不装作能做——猜错比不猜更糟。
	Language string `json:"language,omitempty"`
	// FontSize 是界面与正文的基础字号。
	FontSize float64 `json:"fontSize"`
	// SidebarWidth 是侧栏宽度，单位 DIP。
	SidebarWidth float64 `json:"sidebarWidth"`
	// Sessions 按知识库根目录记「上次打开的文稿」。每个库有自己的标签页：
	// 从 A 库切到 B 库再切回来，应当回到 A 库原来的位置，而不是它的第一篇。
	Sessions map[string]Session `json:"sessions,omitempty"`
	// LastOpen / Active 是旧版字段（只有一个库的记忆），只在迁移时读，
	// normalize 会把它们搬进 Sessions 并清空，之后不再写入。
	LastOpen []string `json:"lastOpen,omitempty"`
	Active   string   `json:"active,omitempty"`
	// Recent 是最近打开过的文稿（不含当前打开的标签），供侧栏「最近打开」使用。
	Recent []string `json:"recent"`
	// Vaults 是最近用过的知识库目录，供侧栏「工作区间」使用。
	Vaults []string `json:"vaults"`
	// ReadWidth 是阅读栏宽度，单位 DIP。
	ReadWidth float64 `json:"readWidth"`
	// Split 记住 Markdown 文稿的显示方式：blocks（块）、split（分栏）、preview、source。
	Split string `json:"split"`
	// LivePreview 决定源码编辑器里是否把 Markdown 标记「化掉」（实时排版）。
	LivePreview bool `json:"livePreview"`
	// SidebarActions 是侧栏顶部动作的顺序（键名见 app.sidebarAction）。
	// 它始终包含全部动作，不显示的那些记在 HiddenActions 里。
	SidebarActions []string `json:"sidebarActions,omitempty"`
	// HiddenActions 是侧栏里不显示的动作。
	HiddenActions []string `json:"hiddenActions,omitempty"`
	// HideRecent 决定侧栏里是否藏掉「最近打开」。
	HideRecent bool `json:"hideRecent,omitempty"`
	// NoUpdateCheck 关掉启动后的自动检查更新。默认关（也就是自动查）：
	// 用户仍然可以在「关于」里手动检查，所以关掉自动检查不是关掉更新。
	NoUpdateCheck bool `json:"noUpdateCheck,omitempty"`
	// LastVersion 是上次运行时的版本号。启动时和当前版本一比就知道这次是不是
	// 刚升级上来——是的话侧栏出一张「已更新」卡片，点开看这段的更新日志。
	// 空 = 第一次运行（或升级到这个字段之前的老配置），那时不提示。
	LastVersion string `json:"lastVersion,omitempty"`
	// ThemePaper / ThemeInk / ThemeAccent 是用户改过的三个颜色，空表示用内置值。
	ThemePaper  string `json:"themePaper,omitempty"`
	ThemeInk    string `json:"themeInk,omitempty"`
	ThemeAccent string `json:"themeAccent,omitempty"`
	// GraphMode 决定知识图谱开在哪里：embed 嵌在主窗口，window 开独立窗口。
	GraphMode string `json:"graphMode"`
	// Sync 是同步配置。
	Sync SyncSettings `json:"sync"`
	// Plugins 记录被停用的插件 id，其余默认启用。
	DisabledPlugins []string `json:"disabledPlugins"`
}

// Default 返回首次启动的设置。
func Default() *Settings {
	return &Settings{
		Theme:          "system",
		FontSize:       15,
		SidebarWidth:   250,
		ReadWidth:      760,
		Split:          "blocks",
		LivePreview:    true,
		SidebarActions: []string{"new", "search", "vault", "graph"},
		GraphMode:      "embed",
		Sync:           SyncSettings{Branch: "main"},
	}
}

// dirOverride 把设置目录临时换到别处，只有测试会设它。
var dirOverride string

// SetDir 把设置目录换到 dir，返回还原用的函数。
//
// 测试必须调它。配置路径是全局的（用户配置目录下的 Papero/），而 newTestApp
// 里的 open/close/commit 都会顺手 cfg.Save()——一次 go test 就能把真实用户的
// vault、lastOpen 改写成测试的临时目录，下次启动自然「只剩第一篇文稿」。
func SetDir(dir string) (restore func()) {
	prev := dirOverride
	dirOverride = dir
	return func() { dirOverride = prev }
}

// Session 是一个知识库里上次打开的文稿（相对路径）与当前激活的那篇。
type Session struct {
	LastOpen []string `json:"lastOpen,omitempty"`
	Active   string   `json:"active,omitempty"`
}

// SessionKey 是 Sessions 的键：知识库根目录的斜杠形式。
//
// 同一个库可能以 D:\Note\Note 或 D:/Note/Note 的形式被记下来（设置里选目录、
// 双击文件带进来的路径写法不同），键不归一就会变成两套记忆。
func SessionKey(root string) string {
	if root == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(root))
}

// Session 返回 root 这个知识库上次打开的文稿。
func (s *Settings) Session(root string) Session {
	return s.Sessions[SessionKey(root)]
}

// SetSession 记下 root 这个知识库上次打开的文稿。
func (s *Settings) SetSession(root string, sess Session) {
	key := SessionKey(root)
	if key == "" {
		return
	}
	if s.Sessions == nil {
		s.Sessions = map[string]Session{}
	}
	s.Sessions[key] = sess
}

// Dir 返回 Papero 的配置目录。
func Dir() (string, error) {
	if dirOverride != "" {
		return dirOverride, os.MkdirAll(dirOverride, 0o755)
	}
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "Papero")
	return dir, os.MkdirAll(dir, 0o755)
}

// Path 返回设置文件的位置。
func Path() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "settings.json"), nil
}

// Load 读取设置；文件缺失或损坏时返回默认值，不让应用起不来。
func Load() *Settings {
	path, err := Path()
	if err != nil {
		return Default()
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Default()
	}
	s := Default()
	if err := json.Unmarshal(data, s); err != nil {
		return Default()
	}
	s.normalize()
	return s
}

// Save 原子写入设置。
func (s *Settings) Save() error {
	path, err := Path()
	if err != nil {
		return err
	}
	s.normalize()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// normalize 把越界或未知的取值收敛到合法范围。
func (s *Settings) normalize() {
	// 旧配置只有一个库的记忆：搬进当前知识库那一格，再清掉旧字段，
	// 免得 Sessions 和 LastOpen 各说一套。
	if len(s.LastOpen) > 0 || s.Active != "" {
		if key := SessionKey(s.Vault); key != "" {
			if _, ok := s.Sessions[key]; !ok {
				s.SetSession(s.Vault, Session{LastOpen: s.LastOpen, Active: s.Active})
			}
		}
		s.LastOpen, s.Active = nil, ""
	}
	// Vaults 去重：同一个库可能以 D:\Note\Note 与 D:/Note/Note 两种写法进来，
	// 工作区间列表里就会列两遍同一个库。归一后按 SessionKey 判重。
	seenVault := map[string]bool{}
	keptVaults := make([]string, 0, len(s.Vaults))
	for _, v := range s.Vaults {
		k := SessionKey(v)
		if k == "" || seenVault[k] {
			continue
		}
		seenVault[k] = true
		keptVaults = append(keptVaults, v)
	}
	s.Vaults = keptVaults
	switch s.Theme {
	case "light", "dark", "system":
	default:
		s.Theme = "system"
	}
	// 认不出的语言当没设过：回落中文，别把配置文件里的脏值带进界面。
	// 前缀判断和 i18n.SetLanguage 一致，"en-US" 也算英文。
	switch {
	case strings.HasPrefix(s.Language, "en"):
		s.Language = "en"
	default:
		s.Language = "" // 中文是界面原文，用空串表示
	}
	if s.FontSize < 12 || s.FontSize > 24 {
		s.FontSize = 15
	}
	if s.SidebarWidth < 180 || s.SidebarWidth > 520 {
		s.SidebarWidth = 250
	}
	if s.ReadWidth < 480 || s.ReadWidth > 1200 {
		s.ReadWidth = 760
	}
	switch s.Split {
	case "blocks", "split", "preview", "source":
	default:
		s.Split = "blocks"
	}
	// 侧栏动作：SidebarActions 只记顺序，且必须包含全部动作；
	// 老配置里没写过的动作（或压根没有这个字段）算作隐藏。
	known := []string{"new", "search", "vault", "graph"}
	seen := map[string]bool{}
	ordered := make([]string, 0, len(known))
	for _, k := range s.SidebarActions {
		for _, kn := range known {
			if k == kn && !seen[k] {
				seen[k] = true
				ordered = append(ordered, k)
			}
		}
	}
	hidden := map[string]bool{}
	for _, k := range s.HiddenActions {
		hidden[k] = true
	}
	legacy := s.SidebarActions == nil
	for _, k := range known {
		if !seen[k] {
			ordered = append(ordered, k)
			if !legacy {
				// 旧版本里「不在列表里」就表示不显示
				hidden[k] = true
			}
		}
	}
	s.SidebarActions = ordered
	kept := make([]string, 0, len(hidden))
	for _, k := range ordered {
		if hidden[k] {
			kept = append(kept, k)
		}
	}
	s.HiddenActions = kept
	switch s.GraphMode {
	case "", "embed", "window":
	default:
		s.GraphMode = "embed"
	}
	switch s.Sync.Remote {
	case "", "dir", "git", "repo", "webdav":
	default:
		s.Sync.Remote = "dir"
	}
	if s.Sync.Branch == "" {
		s.Sync.Branch = "main"
	}
	if s.Sync.IntervalSec < 0 {
		s.Sync.IntervalSec = 0
	}
}

// Dark 报告设置是否要求深色（system 交给窗口主题决定）。
func (s *Settings) Dark() bool { return s.Theme == "dark" }

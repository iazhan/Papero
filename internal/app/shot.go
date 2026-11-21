package app

import (
	"fmt"
	"image/png"
	"os"
	"path/filepath"

	"github.com/egoist/mygo/ui"

	"papero/internal/config"
	"papero/internal/vault"
)

// shotLang / shotTheme 是截图用的界面语言与明暗，空 = 用默认。
// 由 main 的 -lang 与 -theme 设置。
//
// 官网要给中英文、深浅色各一套截图，而截图走的是 config.Default()——它读不到
// 用户设置，也就没法从外面切。放在这里比给三个 Shot 函数各加两个参数据省事。
var (
	shotLang  string
	shotTheme string
)

// SetShotLang 设置截图时的界面语言（"zh" / "en"）。
func SetShotLang(l string) { shotLang = l }

// SetShotTheme 设置截图时的明暗（"light" / "dark"）。
func SetShotTheme(t string) { shotTheme = t }

// shotConfig 是截图用的默认配置：临时知识库 + 指定的语言与明暗。
func shotConfig() *config.Settings {
	cfg := config.Default()
	cfg.Language = shotLang
	if shotTheme != "" {
		cfg.Theme = shotTheme
	}
	return cfg
}

// isolateConfig 把设置目录指到临时目录，返回还原函数。
//
// 截图是开发辅助，但它在临时知识库里 open 文稿时会走到 remember() →
// cfg.Save()。不隔离的话，一次 `go run . -shot` 就把用户真实的 vault
// 改成那个临时示例库，下次启动整个知识库都不见了。
func isolateConfig() func() {
	dir, err := os.MkdirTemp("", "papero-cfg-*")
	if err != nil {
		return func() {}
	}
	restore := config.SetDir(dir)
	return func() {
		restore()
		os.RemoveAll(dir)
	}
}

// ShotFile 打开知识库外的一个真实文件并渲染成 PNG，用来检查各类查看器。
func ShotFile(out, file string, width, height int, scale float32) error {
	restore := isolateConfig()
	defer restore()
	abs, err := filepath.Abs(file)
	if err != nil {
		return err
	}
	dir := filepath.Dir(abs)
	v, err := vault.Open(dir)
	if err != nil {
		return err
	}
	tree, err := v.Scan()
	if err != nil {
		return err
	}
	cfg := shotConfig()
	cfg.Vault = dir
	a := New(cfg, v, tree)
	a.open(filepath.Base(abs))
	img := ui.Render(a.view, width, height, scale)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Printf("已渲染 %s（%dx%d @%.1fx）\n", out, width, height, scale)
	return nil
}

// ShotGraph 渲染独立图谱窗口的内容，用来检查图谱窗口的样子。
func ShotGraph(out string, width, height int, scale float32) error {
	restore := isolateConfig()
	defer restore()
	dir, err := os.MkdirTemp("", "papero-graph-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
	notes := map[string]string{
		"欢迎使用 Papero.md": "# 欢迎使用 Papero\n\n先看 [[设计笔记]] 与 [[读书清单]]。\n",
		"设计笔记.md":        "# 设计笔记\n\n分栏写作的取舍，见 [[分栏写作]]。\n",
		"分栏写作.md":        "# 分栏写作\n\n为什么第一版不做所见即所得。\n",
		"读书清单.md":        "# 读书清单\n\n- [ ] 整理成 [[设计笔记]]\n- [ ] 读完 [[论文笔记]]\n",
		"论文笔记.md":        "# 论文笔记\n\n[[设计笔记]] 的延伸。\n",
		"随手记.md":         "# 随手记\n\n还没有链接的零散想法。\n",
	}

	for name, body := range notes {
		os.WriteFile(filepath.Join(dir, "notes", name), []byte(body), 0o644)
	}
	os.MkdirAll(filepath.Join(dir, "读书"), 0o755)
	for name, body := range map[string]string{
		"如何阅读一本书.md": "# 如何阅读一本书\n\n[[论文笔记]] 提到的方法。\n",
		"笔记法.md":     "# 笔记法\n\n和 [[读书清单]] 配套。\n",
	} {
		os.WriteFile(filepath.Join(dir, "读书", name), []byte(body), 0o644)
	}
	v, err := vault.Open(dir)
	if err != nil {
		return err
	}
	tree, err := v.Scan()
	if err != nil {
		return err
	}
	cfg := shotConfig()
	cfg.Vault = dir
	cfg.GraphMode = "window"
	a := New(cfg, v, tree)
	a.open("notes/欢迎使用 Papero.md")
	// 图谱有入场动画，截图时关掉动效，让节点一次全部出现。
	tt := ui.NewTesterWith(a.graphWindowView, width, height, ui.TesterOptions{
		Preferences: ui.Preferences{ReduceMotion: true},
	})
	tt.Frame()
	tt.Frame()
	img := tt.Image()
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Printf("已渲染 %s（%dx%d @%.1fx）\n", out, width, height, scale)
	return nil
}

// Shot 把窗口的一帧渲染成 PNG，用于无窗口地检查界面（开发辅助）。
// 它在一个临时知识库里放几篇示例文稿，打开第一篇，然后按给定尺寸渲染。
// tabs 大于 1 时会打开多篇，用来检查标签栏溢出。
func Shot(out string, width, height int, scale float32, tabs int) error {
	restore := isolateConfig()
	defer restore()
	dir, err := os.MkdirTemp("", "papero-shot-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)

	os.MkdirAll(filepath.Join(dir, "notes"), 0o755)
	os.WriteFile(filepath.Join(dir, "notes", "欢迎使用 Papero.md"), []byte(welcome), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "设计笔记.md"),
		[]byte("# 设计笔记\n\n分栏写作的取舍，见 [[欢迎使用 Papero]]。\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes", "读书清单.md"),
		[]byte("# 读书清单\n\n- [x] 读完\n- [ ] 整理成 [[设计笔记]]\n"), 0o644)
	for i := 1; i <= 10; i++ {
		os.WriteFile(filepath.Join(dir, "notes", fmt.Sprintf("示例笔记 %02d.md", i)), []byte(fmt.Sprintf("# 示例笔记 %02d\n\n正文。\n", i)), 0o644)
	}

	v, err := vault.Open(dir)
	if err != nil {
		return err
	}
	tree, err := v.Scan()
	if err != nil {
		return err
	}
	cfg := shotConfig()
	cfg.Vault = dir
	a := New(cfg, v, tree)
	a.open("notes/欢迎使用 Papero.md")
	if tabs > 1 {
		for _, f := range a.allFiles() {
			if len(a.tabs) >= tabs {
				break
			}
			a.open(f.Rel)
		}
		a.active = 0
	}

	img := ui.Render(a.view, width, height, scale)
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return err
	}
	fmt.Printf("已渲染 %s（%dx%d @%.1fx）\n", out, width, height, scale)
	return nil
}

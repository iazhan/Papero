# AGENTS.md

给在 Papero 里写代码的人（和 AI）看的操作手册。讲**怎么做**，不讲**为什么这样设计**——
后者在 `docs/architecture.md`。

## 这是什么

Go 写单二进制：MyGo 的 `ui` 包在 GPU 上直接画，没有 WebView、没有 JS、没有 cgo。
界面是**立即模式**——`App.view(c)` 每帧从状态重建整个窗口，画完即丢。

```
Go 1.27+   module: papero   UI: github.com/egoist/mygo v0.10.0
```

## 常用命令

```bash
make build     # = go build -trimpath -ldflags="-H windowsgui -s -w" -o build/papero.exe .
go test ./...  # 全量测试：145 个用例，约 3900 行
go vet ./...   # 提交前必跑
gofmt -l .     # 提交前必跑，输出应为空（ref/ 除外）

go run . -shot 预览.png            # 把主界面渲染成 PNG 后退出
go run . -shot 图.png -open a.md   # 指定打开的文件
go run . -shot 图.png -tabs 8      # 开多个标签，检查标签栏溢出
```

机器上没有 `make` 时，`make build` 换成它等价的直接命令（Git Bash 里常常没有 make）：

```bash
go build -trimpath -ldflags="-H windowsgui -s -w" -o build/papero.exe .
```

**Windows 上必须带 `-H windowsgui`**，否则双击启动会先弹一个黑色终端窗口。
验证方式：PE 头的 subsystem 应为 2（GUI），不是 3（控制台）。

## 发版

**只有推 `v*` 标签才会编译**，`.github/workflows/release.yml` 一条链走完：

1. `check` —— `gofmt` / `go vet` / `go test`，红了就停，不往下编。
2. `build` —— windows-amd64、linux-amd64、darwin-arm64 各编一份（全在 windows-latest
   交叉编译，mygo 不用 cgo）。Windows 那份读 PE 头确认 subsystem = 2：ldflags 漏了
   `-H windowsgui` 也能编出来，这一步专逮它。
3. `release` —— 三份产物 + `SHA256SUMS.txt` 挂到同名 Release 上。

平时推分支不跑，省额度。要验流水线用 Actions 页面的 Run workflow 手动跑——手动跑
只编译不发布。Go 版本从 `go.mod` 读，不写死在 workflow 里。

发版就是：`git tag v0.2.1 && git push origin v0.2.1`。

**版本号要四处一致**：`mygo.json`、`internal/app/settings.go` 的 `appVersion`、推的 tag、
两份 `CHANGELOG`（`CHANGELOG.md` 中文、`CHANGELOG.en.md` 英文）里那一节的标题。客户端自更新拿 `appVersion` 和 tag 比（`internal/update`），
对不上就永远「已是最新」或者反复提示同一版；CHANGELOG 少了那一节，release 正文会退回
GitHub 自动生成的提交列表——应用里的「更新日志」浮层显示的就是它，那就成了流水账。
`appVersion` 是 `const`，ldflags 注入不了，只能手改。

发版正文是**中英双语拼起来的**：英文在上，`<!-- lang:zh -->` 一行之下是中文
（`release.yml` 干这事，客户端 `internal/update.InLang` 按界面语言切）。两份日志都要
写这一版——少写英文那份，英文界面就只能看到中文。

**产物名不要改**：客户端按 `papero-<goos>-<goarch>[.exe]` 认文件，按 `SHA256SUMS.txt`
认摘要（`internal/update`）。改了名字等于把老版本的更新通道切断——老版本会报
「这个平台没有对应的包」。

## 代码地图

| 包 | 职责 | 能 import `ui` 吗 |
| --- | --- | --- |
| `internal/app` | 窗口、视图、交互、状态 | 能 |
| `internal/editor` `internal/richdoc` | 块编辑器与文档模型 | 能 |
| `internal/theme` | 三个颜色 → 全部 token | 能（返回 `ui.Color`） |
| `internal/vault` | 知识库扫描、类型、安全读写 | **不能** |
| `internal/config` | 本机设置与默认值 | **不能** |
| `internal/{board,docx,pdfview,plugin,sync,wiki,wikitext,tex,mermaid}` | 领域逻辑 | **不能** |

领域包不许 import `ui`——它们要能脱离窗口做单元测试，也要能被将来的 CLI 或服务端复用。

## 硬性规则

1. **颜色只能来自 `theme.Tokens`。** 界面里不写裸 `ui.Hex("#...")`。
   `theme.Derive` 是唯一的派生入口，`chromeFor` 只做「部位 → token」的映射。
   新加一处颜色，先问：它属于哪个已有 token？确实没有，再往 `Tokens` 里加字段。
2. **视图不在构建过程中改状态。** 在 `Children`/`Draw`/`List` 的回调里改配置、
   改切片长度，会让同一帧后续的布局越界。要改就记下来，等这一帧画完再改。
3. **配置新增字段必须处理老配置。** 空值 = 用默认，在 `normalize()` 里兜底；
   新增开关要考虑「从没写过」和「用户关了」是两种状态。
4. **注释写「为什么」，中文，不用 emoji。** 不写「本文件定义了……」，直接说这行为什么反直觉。
5. **改 UI 前先看 `docs/roadmap.md` 的既有决定**，别推翻已经定过的事（比如「只留三个颜色」）。

## 测试怎么写

```go
a, dir := newTestApp(t)          // 临时知识库 + 已装配的 App，不建真窗口
a.open("notes/欢迎.md")
tt := ui.NewTester(a.view, 1100, 760)
tt.Frame(); tt.Frame()           // 第一帧只做布局，断言前至少两帧

tt.Click("明暗 深色")             // 按无障碍名字点，不是坐标
tt.RightClick("notes/示例.md")    // 行有 Label 用 Label，别点内部文字的中心
if !tt.HasText("恢复默认") { ... }
```

- 断言**文案**时要接受文案会变；断言**状态**更稳（`a.cfg.Theme`、`tab.mode`）。
- 反向断言也要写：删掉的功能要断言它**不在**（如侧栏里的「发现」）。
- 视觉改动要出图验证：临时写 `internal/app/zz_xxx_test.go` 渲染 PNG 到 `tmp/shots/`，
  用看图工具确认后**删掉临时 test**。截图留在 `tmp/shots/` 就行（已 gitignore）。

## 踩过的坑

| 现象 | 原因 | 做法 |
| --- | --- | --- |
| 控件压到隔壁的文字上 | 对组合控件用 `.Size()` 只是改外框，内部按 `Space()` 画的色片不会缩 | 组合控件别硬设尺寸，或用定宽容器包住 |
| 自己画的路径少了一段边 | `ui.Path` 是值拷贝，`*b = *a` 共享底层切片，`Close()` 会污染原路径 | 填充和描边各建一条独立路径 |
| 点勾选框闪退（index out of range） | 在 `ui.List` 渲染回调里把配置数组改短了 | 先记 pending，`List` 返回后再改 |
| 第一帧就把默认值写死了 | 比较基准用了配置里的空串（空 = 用默认） | 基准用「这一帧生效的值」 |
| 深色色块和文字糊成一片 | 色块紧贴文字，而文字色恰好等于某个色板值 | 色块在前、留足间距，标签用正文色 |
| 测试里右键菜单打不开 | 点在行的文字子元素中心，事件到不了行本身 | 按行的 `Label` 用 `tt.RightClick(name)` |
| 截图里没有菜单/浮层 | `Tester.Image()` 不抓 popover 层 | 用 `tt.Texts()` 断言，别指望截图 |

## 文件放哪

| 目录 | 放什么 |
| --- | --- |
| `build/` | 只有构建产物：`papero.exe`、`papero-setup-*.exe`、`windows-amd64/` |
| `tmp/shots/` | 截图、对比图、诊断渲染。gitignore，随便丢 |
| `docs/images/` | 文档引用的图，命名 `screenshot-*.png` |
| `design/icons/` | 图标源：当前这套（3 个 SVG + `png-flat/`），进版本库 |
| `design/icons/_old/` | 图标的历史版本，本地留档，不进版本库 |
| `site/` | 官网（纯静态，无构建）。发到 GitHub Pages / Vercel |

截图**不要**再往 `build/` 里放——那是产物目录，一次调试就能堆两百张。

## 官网

`site/` 是一份没有构建步骤的静态页面：`index.html` + `style.css` + `app.js` + `img/`。
配色不另挑，直接抄 `internal/theme/theme.go` 的 Presets 与 Derive 比例（页首注释里有对应关系）。

**线上地址 https://papero.azhan.me/**：GitHub Pages 托管，由 `.github/workflows/pages.yml`
发布（推 main 上 `site/**` 的改动才跑）。`site/CNAME` 里写着域名，别删——Actions 部署靠
它带上自定义域名。DNS 是 `papero.azhan.me CNAME iazhan.github.io`。

`site/robots.txt` 与 `sitemap.xml`、`index.html` 里的 `canonical` / `og:url` / `og:image`
都写死了这个域名。换域名时这四处要一起改（`og:image` 必须是绝对地址，否则分享卡片没图）。

首屏那张截图画的是真程序：中英 × 浅深共四张，切语言或切明暗时换图
（`data-src-en` / `data-src-dark` / `data-src-en-dark`）。重画：

```bash
go run . -shot site/img/hero.png         -open "tmp/site-demo/zh/周记.md"            -width 1400 -height 940 -scale 2 -lang zh
go run . -shot site/img/hero-dark.png    -open "tmp/site-demo/zh/周记.md"            -width 1400 -height 940 -scale 2 -lang zh -theme dark
go run . -shot site/img/hero-en.png      -open "tmp/site-demo/en/Week in review.md" -width 1400 -height 940 -scale 2 -lang en
go run . -shot site/img/hero-en-dark.png -open "tmp/site-demo/en/Week in review.md" -width 1400 -height 940 -scale 2 -lang en -theme dark
```

`tmp/site-demo/` 是给截图用的演示知识库（gitignore）。

下载按钮不写死版本：地址是 `/releases/latest/download/<产物名>`，永远指向最新一版，
所以**只要不改产物名就不用动链接**。页面上显示的版本号会先从 Releases API 现取
（限流时自动退回 HTML 里那份），发版时仍要把 `hero.meta` 与 `site/app.js` 顶部的
`version` 兜底值一起改成新版本。

## 打包

**只有用户说「发版」才打包。** 平时改完只跑测试，不产出
`build/papero-setup-*.exe`——设计没定稿时打的包必然是废的。

`mygo.json` 的 `resources` 列了 `LICENSE` 与 `NOTICE.md`：`mygo build` 会把它们拷进
安装目录（日志里那句 `Copied 2 resources next to the executable`）。MIT 与 ISC 要求
分发时保留声明，所以这两个文件是产物的一部分，别从 `resources` 里删。

## 交付前

```bash
gofmt -l . | grep -v '^ref/'   # 必须为空
go vet ./...                   # 必须无输出
go test ./...                  # 必须全绿
make build                     # 或上面那条 go build，GUI 版能编出来
```

改动分小步提交，一步一件事；说明里写清「改了什么、为什么、怎么验证的」。

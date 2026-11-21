# 第三方代码与资源

Papero 使用 MIT 许可发布。下面是随附的第三方内容与它们的许可。

## 依赖

| 组件 | 用途 | 许可 |
| --- | --- | --- |
| [MyGo](https://github.com/egoist/mygo) | 原生窗口、GPU 绘制、文本与输入法、代码编辑器（`ui/editor`）、Markdown 渲染（`ui/markdown`）、对话框、文件监视 | MIT |
| [Onevra](https://github.com/oneo-me/onevra) | 知识图谱（`components/graph` 关系图谱组件） | MIT |
| [ArcMD](https://github.com/Ciao1019/ArcMD) | Markdown 富文档模型与导入导出（`internal/richdoc`，改编自其 `internal/document` 与 `internal/editor/markdown.go`）；**富文本编辑器本体**（`internal/editor`，整体移植）；`internal/i18n`、`internal/mermaid`、`internal/wikitext` | MIT |
| [go-text/typesetting](https://github.com/go-text/typesetting) | 文本整形（MyGo 的依赖） | Unlicense / BSD-3-Clause |
| [golang.org/x/text](https://pkg.go.dev/golang.org/x/text) | 中文 GBK 解码（PDF 文字还原） | BSD-3-Clause |
| [golang.org/x/image](https://pkg.go.dev/golang.org/x/image) | 图片处理（MyGo 的依赖） | BSD-3-Clause |
| [folio](https://github.com/gen2brain/folio) | 纯 Go 的文档渲染器（PDF 查看器） | Apache-2.0 |

## 图标

目前有两套，按包分开，不混用：

| 位置 | 来源 | 许可 | 规格 |
| --- | --- | --- | --- |
| `internal/app/icons.go`（62 个） | [IconMCP](https://iconmcp.com) | MIT | 16 格网格，描边 1.05，圆头圆角 |
| `internal/editor/icons.go`（70 个） | [Lucide](https://lucide.dev) | ISC | 24 格网格，描边 2，圆头圆角 |

窗口外壳（标题栏、侧栏、工具条）用 16 格那套；编辑器浮动工具条那 70 个是随
ArcMD 的编辑器一起带过来的，沿用 Lucide，许可声明写在文件头。

两套的网格与线宽本来就不同（16 格 1.05 对 24 格 2），所以不做混用：
同一个区域里只出现一套，否则线条粗细会打架。

## 参考过的开源项目

Papero 的设计参考了三个同类的 Go + MyGo 应用（均未直接复制代码，
但借鉴了它们的架构取舍与产品思路）：

- **ArcMD**（MIT）—— 空间与原生 Markdown 编辑器的取舍、插件清单与安装流程；
- **Leafmark**（MIT）—— 原生编辑器与 Markdown 原文保留的思路；
- **Celestine**（MIT）—— 文件级三方同步的算法与冲突处理。

## 文件格式

- `.excalidraw` 的 JSON 结构来自 [Excalidraw](https://github.com/excalidraw/excalidraw)（MIT）。
  Papero 只读写其中的常用字段，保留其余字段以便互通。

// Package richdoc 是 Markdown 的富文档模型与导入导出。
//
// 这一包改编自 ArcMD（github.com/Ciao1019/ArcMD，MIT）的 internal/document
// 与 internal/editor/markdown.go：ArcMD 用这套模型做真正的所见即所得，
// 它把 Markdown 读成带样式的块与行（run），再原样写回，未编辑的块保持作者
// 原来的写法。
//
// 与 papero/internal/mdedit 的分工：
//
//	mdedit    只有「标记 + 内容 + 末尾空行」，够块编辑器用，简单但只认
//	          标题/列表/任务/引用/代码/分隔线
//	richdoc   认识表格、callout、行内与块级公式、图片、脚注、嵌套列表、
//	          行内样式（粗体/斜体/删除线/代码/高亮/颜色）
//
// ArcMD 的编辑器（布局、光标、输入法、自动配对，约三万行）没有被带过来：
// Papero 用的是 MyGo 的 ui/editor，那是另一条路。这一包只负责模型与格式转换。
package richdoc

package richdoc

import "testing"

// 用一份「什么都有」的 Markdown 检验导入导出是否无损。
const rich = `# 标题

段落里带 **粗体**、*斜体*、~~删除线~~、` + "`行内代码`" + ` 和 [[双链]]。

- 列表项
  - 嵌套项
- [ ] 待办
- [x] 已完成

1. 有序
2. 有序二

> 引用
> 第二行

| 列一 | 列二 |
| --- | --- |
| 值   | 值二 |

` + "```go\nfunc main() {}\n```" + `

行内公式 $a+b$ 与块级公式：

$$
E = mc^2
$$

![图片](images/a.png)

---

结尾。
`

func TestImportExportRich(t *testing.T) {
	doc := ImportMarkdown(rich)
	if doc == nil {
		t.Fatal("导入失败")
	}
	out := ExportMarkdown(doc)
	if out != rich {
		t.Errorf("往返不一致：\n--- 得到 ---\n%s\n--- 想要 ---\n%s", out, rich)
	}
}

func TestImportCounts(t *testing.T) {
	doc := ImportMarkdown(rich)
	if len(doc.Blocks) < 8 {
		t.Errorf("块数 = %d，太少了", len(doc.Blocks))
	}
}

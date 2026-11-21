package vault

import (
	"path"
	"strings"

	"papero/internal/i18n"
)

// Kind 是 Papero 认得的文稿类型。查看器与插件按 Kind 认领文件。
type Kind int

const (
	KindOther Kind = iota
	KindMarkdown
	KindText
	KindImage
	KindPDF
	KindWord
	KindBoard // .excalidraw 白板，由内置白板插件认领
)

// String 返回 Kind 的稳定标识，用于设置文件与插件清单。
func (k Kind) String() string {
	switch k {
	case KindMarkdown:
		return "markdown"
	case KindText:
		return "text"
	case KindImage:
		return "image"
	case KindPDF:
		return "pdf"
	case KindWord:
		return "word"
	case KindBoard:
		return "board"
	}
	return "other"
}

// KindOf 按扩展名判断文件类型。
func KindOf(name string) Kind {
	ext := strings.ToLower(path.Ext(name))
	switch ext {
	case ".md", ".markdown", ".mdx", ".mdown", ".mkd":
		return KindMarkdown
	case ".txt", ".text", ".log", ".csv", ".tsv", ".json", ".yaml", ".yml",
		".toml", ".ini", ".conf", ".go", ".js", ".ts", ".py", ".rs", ".c", ".h",
		".cpp", ".hpp", ".java", ".sh", ".ps1", ".html", ".htm", ".css", ".sql",
		".xml", ".tex", ".bib", ".rst", ".org":
		return KindText
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp", ".svg", ".avif":
		return KindImage
	case ".pdf":
		return KindPDF
	case ".docx":
		return KindWord
	case ".excalidraw":
		return KindBoard
	}
	return KindOther
}

// Text 报告该类型能否在源码编辑器里以文本打开。
func (k Kind) Text() bool {
	switch k {
	case KindMarkdown, KindText:
		return true
	}
	return false
}

// Label 返回类型名，用于界面提示。这块是文案，所以过一遍 i18n——
// 领域包不碰 ui，但 i18n 是不依赖 UI 的纯查表，可以进来。
func (k Kind) Label() string {
	switch k {
	case KindMarkdown:
		return "Markdown"
	case KindText:
		return i18n.T("kind.text")
	case KindImage:
		return i18n.T("kind.image")
	case KindPDF:
		return "PDF"
	case KindWord:
		return "Word"
	case KindBoard:
		return i18n.T("kind.board")
	}
	return i18n.T("kind.file")
}

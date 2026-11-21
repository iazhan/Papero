package plugin

// Request 是宿主调用插件时传入的参数。v1 只有这三种调用。
type Request struct {
	// Command 是命令 id；为空表示在打开 Panel 或转换文档。
	Command string `json:"command,omitempty"`
	// Panel 是面板 id。
	Panel string `json:"panel,omitempty"`
	// Document 是当前文稿的相对路径与正文。
	Document Document `json:"document"`
	// Selection 是选中的文字，仅当命令的 when 为 selection。
	Selection string `json:"selection,omitempty"`
	// Input 是命令或面板的参数（例如用户在面板里填的查询）。
	Input string `json:"input,omitempty"`
}

// Document 是交给插件的文稿快照。
type Document struct {
	// Path 是相对知识库的斜杠路径。
	Path string `json:"path"`
	// Text 是 Markdown 原文。
	Text string `json:"text"`
}

// Response 是插件返回的结果。三种字段互斥，宿主按非空者处理。
type Response struct {
	// Edits 是对当前文稿的替换，按顺序应用，算作一步撤销。
	Edits []Edit `json:"edits,omitempty"`
	// Markdown 是要显示在面板或浮层里的内容。
	Markdown string `json:"markdown,omitempty"`
	// Toast 是一条短提示。
	Toast string `json:"toast,omitempty"`
	// Error 让宿主提示失败原因。
	Error string `json:"error,omitempty"`
}

// Edit 是一处文本替换，偏移量按 UTF-8 字节计。
type Edit struct {
	Start int    `json:"start"`
	End   int    `json:"end"`
	Text  string `json:"text"`
}

// Host 是插件与宿主之间的全部契约。内置插件直接实现它；
// 进程插件由宿主的 JSON-RPC 客户端转发成同样的调用。
type Host interface {
	// Invoke 执行一次命令或打开一个面板。
	Invoke(req Request) (Response, error)
	// Close 在插件被停用或应用退出时释放资源。
	Close() error
}

// HostFunc 把函数适配成 Host，方便内置插件只写一个闭包。
type HostFunc func(Request) (Response, error)

// Invoke 调用 f。
func (f HostFunc) Invoke(req Request) (Response, error) { return f(req) }

// Close 什么也不做。
func (HostFunc) Close() error { return nil }

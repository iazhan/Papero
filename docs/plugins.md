# 插件

> 状态：清单、安装、权限校验与宿主契约已实现（`internal/plugin`）；
> 进程插件的 JSON-RPC 传输与插件市场还没做。

核心做「打开、显示、编辑、同步」，以及所有第一方能力：Markdown、PDF、Word、白板。
插件留给**第三方扩展**——AI 写作、翻译、导出增强、某种特定格式的转换。

这条线是明确的：**凡是要和文稿列表、标签、保存流程长在一起，或者需要自己画界面的，
都编译进 Papero 本体**（白板就是这样从「内置插件」改回核心功能的）；
插件则是一个独立的进程，通过协议贡献命令与面板内容。

## 一个插件

插件是一个文件夹（分发时打包成 zip），根目录有 `papero-plugin.json`：

```json
{
  "id": "dev.example.ai-writer",
  "name": "AI 写作",
  "version": "0.1.0",
  "description": "改写、摘要、续写",
  "author": "Example",
  "license": "MIT",
  "engine": "0.1.0",
  "runtime": "process",
  "main": "ai-writer.exe",
  "permissions": ["document:read", "document:write", "network"],
  "contributes": {
    "commands": [
      { "id": "rewrite", "title": "AI：改写选中内容", "when": "selection" },
      { "id": "summarize", "title": "AI：摘要全文", "when": "document" }
    ],
    "panels": [{ "id": "chat", "title": "对话", "icon": "✦" }],
    "viewers": [{ "exts": ["mmd"], "title": "思维导图" }]
  }
}
```

| 字段 | 规则 |
| --- | --- |
| `id` | 反向域名，全小写，不能含空格或路径分隔符；也是安装后的文件夹名 |
| `version` / `engine` | 语义化版本；`engine` 是要求的最低 Papero 版本 |
| `runtime` | 目前只有 `process`（独立可执行文件） |
| `main` | 插件目录内的入口可执行文件，必填 |
| `permissions` | 只能取 `Permissions` 表里的值，安装前展示给用户 |
| `contributes.commands` | 出现在命令面板；`when` 为 `""`、`document` 或 `selection` |
| `contributes.panels` | 面板内容由插件以 Markdown 返回 |
| `contributes.viewers` | 声明能查看的文件类型（`kind` 或 `exts`） |

## 运行方式

插件是独立可执行文件，用 JSON-RPC 2.0 over stdio 与宿主通信（像语言服务器那样）。
它跑在用户权限下，因此市场会给它打标记、要求审查。

宿主与插件之间的全部协议面是：

```go
type Host interface {
    Invoke(req Request) (Response, error)
    Close() error
}
```

`Request` 带着命令 id、当前文稿（路径与 Markdown 原文）、选中的文字；
`Response` 三选一：`Edits`（对文稿的替换，算作一步撤销）、
`Markdown`（显示在面板里）、`Toast`（一条短提示）。

**传输层尚未实现**：`plugin.Host` 的接口已经定型，将来加一个
`plugin.NewProcess(manifest)` 把 stdio 上的 JSON-RPC 适配成同一个 `Host` 即可，
调用方（命令面板）不需要改。

## 插件不画界面

外部插件永远拿不到画布，也不会得到 WebView。它们**贡献**命令和面板，
Papero 用**自己的样式**绘制，插件只回答内容（Markdown）。这样：

- 每个插件的观感与 Papero 一致，不会各带一套 UI；
- 插件无法伪装成 Papero 的界面去骗取输入；
- 插件的产物天然可选中、可复制、可无障碍朗读。

## 安装

`Manager.Install(data, opts)` 的检查顺序（全部有测试）：

1. 如果给了 `SHA256`，先校验整包摘要；
2. 必须是合法 zip；
3. 拒绝绝对路径、`..`、反斜杠越界与符号链接；
4. 解压总量不超过上限（默认 64 MiB）；
5. 清单必须解析、校验通过；
6. 先解到临时目录，再原子替换旧版本（失败时回滚）。

插件装在 `<配置目录>/Papero/plugins/<id>/`。启停状态记在设置的
`disabledPlugins` 里——升级插件不会丢掉用户的选择。

## 权限

| 权限 | 含义 |
| --- | --- |
| `vault:read` | 读取知识库中的文稿 |
| `vault:write` | 修改知识库中的文稿 |
| `document:read` | 读取当前打开的文稿 |
| `document:write` | 修改当前打开的文稿 |
| `network` | 访问网络 |
| `secrets` | 读写本机保存的密钥 |
| `ui:panel` | 在侧栏显示面板 |

清单里写了未知权限会直接拒绝安装——宁可装不上，也不要装上一个权限含义不明的插件。

## 路线

1. 进程插件的 JSON-RPC 传输与超时、崩溃隔离；
2. 安装前的权限确认弹窗与插件管理界面（现在只列出与启停）；
3. 静态 `index.json` 注册表 + 市场浏览；
4. 首批第三方插件：AI 写作、导出增强、格式转换；
5. 发布签名（minisign / sigstore），叠加在注册表摘要之上。

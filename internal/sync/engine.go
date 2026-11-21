// Package sync 在本地知识库与一个远端之间做文件级的三方同步。
//
// 同步单位就是文件：Markdown、图片、PDF 都是普通文件，因此远端可以是
// 任何能存文件的地方。远端实现两种：
//
//	dir   本地或网盘文件夹（iCloud、OneDrive、Syncthing 都可以）
//	git   GitHub 仓库，交给系统的 git 命令推送与拉取
//
// 冲突不丢数据：远端版本成为正式版本，本地改动另存为
// 「名字 (冲突 2026-11-17 23-00-00).md」，下一次同步再传上去。
// 第一次接上一个已经有文件的远端是例外：那不算冲突，本地版本整批收进
// .papero/join-<时间>/，文稿树里不留副本。
package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Entry 是远端一个文件的样子。
//
// Version 是远端的版本令牌，只用来判断「还是上次那个文件」：文件夹远端就是内容
// 摘要，WebDAV 是服务器给的 ETag。Hash 是内容摘要，远端给不出时为空——ETag
// 不是内容摘要（S3 是 MD5，WebDAV 由服务器自己定），不能拿来跟本地的 SHA 比。
type Entry struct {
	Version string
	Hash    string
}

// ErrVersionChanged 是一次条件写被拒绝：文件在 List 之后被另一台设备改了。
// 这次同步不落状态，下一次会重新比对。
var ErrVersionChanged = errors.New("文件在同步过程中被另一台设备改了，再同步一次")

// Remote 是远端的抽象。
type Remote interface {
	// Kind 返回远端类型：dir、git、webdav。
	Kind() string
	// Descriptor 说明状态属于哪一个远端：类型加上地址（dir 的目录、git 的
	// 仓库与分支、webdav 的地址与账号）。Kind 只说「哪个文件夹」不够——换一个
	// 文件夹时，旧状态里的「远端还有这些文件」会把新远端读成「文件全被删了」。
	Descriptor() string
	// List 返回远端的全部文件：相对路径 → 版本与摘要。
	List(ctx context.Context) (map[string]Entry, error)
	// Get 读取一个文件。
	Get(ctx context.Context, rel string) ([]byte, error)
	// Put 写入一个文件，base 是 List 给的那个版本：非空时按条件写，服务器上
	// 已经不是这一版就返回 ErrVersionChanged，而不是悄悄盖掉别人的改动。
	// 返回写入后的版本。
	Put(ctx context.Context, rel string, data []byte, base string) (Entry, error)
	// Delete 删除一个文件。
	Delete(ctx context.Context, rel string) error
}

// LocalFilter 是能说「这些本地文件不用同步」的远端。
//
// 本库仓库模式用得上：被 .gitignore 忽略的文件永远进不了仓库，也就永远到不了
// 远端。硬要同步只有两种结局——每轮提交都报错，或者被当成「远端删了」挪进
// 回收站。所以扫描本地时就把它们去掉。
type LocalFilter interface {
	FilterLocal(ctx context.Context, local map[string]string) (map[string]string, error)
}

// FileState 是某个文件上次同步后的两端摘要。
type FileState struct {
	Local  string `json:"local"`
	Remote string `json:"remote"`
}

// State 是同步状态，保存在知识库内的 .papero/sync.json。
type State struct {
	// Remote 是 Remote.Descriptor：类型加地址，换远端时重新开始比对。
	Remote string               `json:"remote"`
	Files  map[string]FileState `json:"files"`
}

// Failure 是一个没同步成的文件。一个文件失败不该拖住整个库：记下来，接着走。
type Failure struct {
	Path   string
	Reason string
}

// Result 是一次同步的结果。
type Result struct {
	Uploaded   int
	Downloaded int
	Deleted    int
	// Joined 是第一次接上这个远端时，两端都有、内容又不同的文件数：远端版本
	// 成为正式版本，本地那一版存进 JoinDir。
	Joined int
	// JoinDir 是那次加入保存本地版本的位置，相对知识库根目录；没有加入时为空。
	JoinDir string
	// Failed 是这次没同步成的文件，连原因。下一轮会重新试。
	Failed []Failure
	// Pending 是「两边都改过、拉取不动它」的文件数：留给一次完整同步去合并。
	Pending int
	// Updated 是这次从云端写下来的路径（拉取用）：界面据此重载打开着的文稿。
	Updated []string
	// Stopped 是这一轮为什么提前停下（服务器限速）：已经做完的都算数，
	// 过一会儿再同步会接着来。
	Stopped   string
	Conflicts []string
	// Skipped 是远端一次消失太多文件时被推迟的删除。
	Skipped int
}

// Summary 返回一句可以直接放在状态栏里的说明。
func (r Result) Summary() string {
	var parts []string
	if r.Uploaded > 0 {
		parts = append(parts, fmt.Sprintf("上传 %d", r.Uploaded))
	}
	if r.Downloaded > 0 {
		parts = append(parts, fmt.Sprintf("下载 %d", r.Downloaded))
	}
	if r.Deleted > 0 {
		parts = append(parts, fmt.Sprintf("删除 %d", r.Deleted))
	}
	if r.Joined > 0 {
		parts = append(parts, fmt.Sprintf("加入 %d", r.Joined))
	}
	if len(r.Conflicts) > 0 {
		parts = append(parts, fmt.Sprintf("冲突 %d", len(r.Conflicts)))
	}
	if len(r.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("失败 %d", len(r.Failed)))
	}
	if len(parts) == 0 {
		if r.Stopped != "" {
			return r.Stopped
		}
		return "已是最新"
	}
	return strings.Join(parts, " · ")
}

// Engine 执行一次同步。
type Engine struct {
	// Root 是本地知识库根目录。
	Root string
	// Remote 是远端。
	Remote Remote
	// Now 用于生成冲突副本的时间戳，测试可替换。
	Now func() time.Time
	// Ignore 是用户选的额外忽略：点开头的文件/文件夹、.gitignore。
	// 零值等于「只忽略内部目录」。
	Ignore Filter
}

// statePath 是同步状态文件的位置。
func statePath(root string) string {
	return filepath.Join(root, ".papero", "sync.json")
}

// alwaysIgnored 是无论用户怎么设置都不同步的路径：版本控制、回收站、同步状态
// 与临时文件。用户选的忽略（点开头、.gitignore）在 Filter 里另算。
//
// 按**任意一段**判断，不只看首段：`sub/.git/config` 这种嵌套仓库以前会被当成
// 普通文件，把别人仓库的 objects、hooks 一路传上去。
func alwaysIgnored(rel string) bool {
	if rel == "" {
		return true
	}
	for _, seg := range strings.Split(rel, "/") {
		switch seg {
		case ".git", ".trash", ".papero", "node_modules", ".obsidian":
			return true
		}
	}
	base := path.Base(rel)
	if strings.HasPrefix(base, ".papero-") || strings.HasSuffix(base, ".tmp") {
		return true
	}
	return false
}

// Run 执行一次同步。
func (e *Engine) Run(ctx context.Context) (Result, error) {
	if e.Remote == nil {
		return Result{}, errors.New("同步：还没有配置远端")
	}
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	local, err := scanLocal(e.Root, e.Ignore)
	if err != nil {
		return Result{}, err
	}
	// 远端可能不认某些本地文件（本库仓库模式：被 .gitignore 忽略的）
	if f, ok := e.Remote.(LocalFilter); ok {
		if local, err = f.FilterLocal(ctx, local); err != nil {
			return Result{}, fmt.Errorf("同步：整理本地文件失败：%w", err)
		}
	}
	remote, err := e.Remote.List(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("同步：读取远端失败：%w", err)
	}
	// 远端可能存着本该忽略的路径（旧版本同步上去的 .git、node_modules）：
	// 认它们的话，每一轮都会「下载」一个被忽略的文件，写回工作区。
	for rel := range remote {
		if e.Ignore.Skip(rel, false) {
			delete(remote, rel)
		}
	}
	// first 是「这个远端从来没同步过」：这时两端都有、内容又不同的文件不是
	// 冲突——是加入一个已经有文件的远端，见下面那一段。
	state, first := e.loadState()

	// joinName 是这次加入保存本地版本的分目录，用到时才定名字。
	joinName := ""

	var result Result
	// 远端一次少了很多文件时，多半是服务端出问题而不是用户删了东西：
	// 推迟删除，避免把本地也清空。
	remoteMissing := 0
	for rel := range state.Files {
		if _, ok := remote[rel]; !ok {
			remoteMissing++
		}
	}
	deferDeletes := remoteMissing > 20 && remoteMissing > len(state.Files)/10

	names := map[string]bool{}
	for rel := range local {
		names[rel] = true
	}
	for rel := range remote {
		names[rel] = true
	}
	for rel := range state.Files {
		names[rel] = true
	}
	ordered := make([]string, 0, len(names))
	for rel := range names {
		ordered = append(ordered, rel)
	}
	sort.Strings(ordered)

	// one 处理一个文件。返回的错误由下面决定：限速就整轮停下（再问也是 503），
	// 其余只记下这一个、接着走下一个——一个坏文件不该让整个库同步不了。
	one := func(rel string) error {
		lh := local[rel]
		rh := remote[rel].Version
		base := state.Files[rel]
		switch {
		case lh == base.Local && rh == base.Remote:
			// 两端都没动。
		case lh != "" && (lh == rh || lh == remote[rel].Hash):
			// 已经一致（例如两边都改了同样的内容）。远端给的摘要与本地相同
			// 就够了：WebDAV 只给 ETag，那边不会走到这里，但也不影响正确性。
			state.Files[rel] = FileState{Local: lh, Remote: rh}
		case lh == "" && rh == "":
			// 两边都没了：条目删掉，否则状态文件只增不减。
			delete(state.Files, rel)
		case lh != base.Local && rh == base.Remote:
			// 只有本地变了：上传，本地删掉则远端也删。
			if lh == "" {
				if rh != "" {
					if deferDeletes {
						result.Skipped++
						return nil
					}
					if err := e.Remote.Delete(ctx, rel); err != nil {
						return fmt.Errorf("删除 %s 失败：%w", rel, err)
					}
					result.Deleted++
				}
				delete(state.Files, rel)
				return nil
			}
			data, err := os.ReadFile(filepath.Join(e.Root, filepath.FromSlash(rel)))
			if err != nil {
				return fmt.Errorf("读取本地文件 %s 失败：%w", rel, err)
			}
			entry, err := e.Remote.Put(ctx, rel, data, base.Remote)
			if err != nil {
				return fmt.Errorf("上传 %s 失败：%w", rel, err)
			}
			state.Files[rel] = FileState{Local: lh, Remote: versionOr(entry, lh)}
			result.Uploaded++
		case lh == base.Local && rh != base.Remote:
			// 只有远端变了：下载，远端删掉则本地移入回收站。
			if rh == "" {
				if lh != "" {
					if err := e.trashLocal(rel, now()); err != nil {
						return fmt.Errorf("把 %s 移进回收站失败：%w", rel, err)
					}
					result.Deleted++
				}
				delete(state.Files, rel)
				return nil
			}
			data, err := e.Remote.Get(ctx, rel)
			if err != nil {
				return fmt.Errorf("下载 %s 失败：%w", rel, err)
			}
			if err := writeLocal(e.Root, rel, data); err != nil {
				return fmt.Errorf("写入本地 %s 失败：%w", rel, err)
			}
			state.Files[rel] = FileState{Local: hash(data), Remote: rh}
			result.Downloaded++
		default:
			// 第一次接上这个远端，两端都有这个文件：这不是冲突，是加入。
			// 远端为准，但本地这一版不进文稿树（否则接一个已有笔记的网盘，
			// 整库会散出几百个「(冲突 …)」），而是整批收进一个目录。
			if first && lh != "" && rh != "" {
				data, err := e.Remote.Get(ctx, rel)
				if err != nil {
					return fmt.Errorf("下载 %s 失败：%w", rel, err)
				}
				// 内容一样就不算冲突，也不用存档：远端的版本令牌未必是内容摘要
				// （WebDAV 是 ETag、git 是 blob SHA），光比令牌看不出「相等」。
				if hash(data) == lh {
					state.Files[rel] = FileState{Local: lh, Remote: rh}
					return nil
				}
				if joinName == "" {
					joinName = "join-" + now().Format("2006-01-02-150405")
					result.JoinDir = path.Join(".papero", joinName)
				}
				old, err := os.ReadFile(filepath.Join(e.Root, filepath.FromSlash(rel)))
				if err != nil {
					return fmt.Errorf("读取本地文件 %s 失败：%w", rel, err)
				}
				if err := writeLocal(e.Root, path.Join(".papero", joinName, rel), old); err != nil {
					return fmt.Errorf("保存本地版本 %s 失败：%w", rel, err)
				}
				if err := writeLocal(e.Root, rel, data); err != nil {
					return fmt.Errorf("写入本地 %s 失败：%w", rel, err)
				}
				state.Files[rel] = FileState{Local: hash(data), Remote: rh}
				result.Joined++
				return nil
			}
			// 两端都变了：远端优先，本地另存冲突副本。
			if rh == "" {
				// 远端删了、本地改了：本地留下并重新上传。
				data, err := os.ReadFile(filepath.Join(e.Root, filepath.FromSlash(rel)))
				if err != nil {
					return fmt.Errorf("读取本地文件 %s 失败：%w", rel, err)
				}
				entry, err := e.Remote.Put(ctx, rel, data, "")
				if err != nil {
					return fmt.Errorf("上传 %s 失败：%w", rel, err)
				}
				state.Files[rel] = FileState{Local: lh, Remote: versionOr(entry, lh)}
				result.Uploaded++
				return nil
			}
			data, err := e.Remote.Get(ctx, rel)
			if err != nil {
				return fmt.Errorf("下载 %s 失败：%w", rel, err)
			}
			if hash(data) == lh {
				state.Files[rel] = FileState{Local: lh, Remote: rh}
				return nil
			}
			copyRel := conflictName(rel, now())
			if old, err := os.ReadFile(filepath.Join(e.Root, filepath.FromSlash(rel))); err == nil {
				if err := writeLocal(e.Root, copyRel, old); err != nil {
					return fmt.Errorf("写冲突副本 %s 失败：%w", copyRel, err)
				}
			}
			if err := writeLocal(e.Root, rel, data); err != nil {
				return fmt.Errorf("写入本地 %s 失败：%w", rel, err)
			}
			result.Conflicts = append(result.Conflicts, copyRel)
			state.Files[rel] = FileState{Local: hash(data), Remote: rh}
		}
		return nil
	}

	for _, rel := range ordered {
		if e.Ignore.Skip(rel, false) {
			continue
		}
		if ctx.Err() != nil {
			result.Stopped = "同步被中断（超时或取消）：已经做完的都留着，再同步一次会接着来"
			break
		}
		err := one(rel)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			// 取消不是这个文件的错
			result.Stopped = "同步被中断（超时或取消）：已经做完的都留着，再同步一次会接着来"
			break
		}
		// 服务器限速（429/503）：再发也是同样的回答，整轮停下，保住已完成的
		var limited *rateLimited
		if errors.As(err, &limited) {
			result.Stopped = limited.Error()
			break
		}
		result.Failed = append(result.Failed, Failure{Path: rel, Reason: err.Error()})
	}

	if err := flush(ctx, e.Remote); err != nil {
		// 推送失败时不能落状态：git 的实现在副本里写文件、到这里才提交，
		// 提交没成功就等于什么都没发出去；状态要是记成「已同步」，下一次
		// 副本被重置回远端版本，本地这个文件就会被当成「远端删了」。
		return result, fmt.Errorf("同步：提交远端改动失败：%w", err)
	}
	e.saveState(state)
	return result, nil
}

// Pull 只从云端往下拿：本地没有的、远端更新过的都下载下来。
//
// 三条边界：不上传任何东西、不删除本地文件、两边都改过的文件不动（留给一次完整
// 同步去合并）。覆盖本地之前，本地那一版存进 `.papero/before-pull-<时间>/`。
//
// 「打开时先拉一遍」用的就是它：多台设备编同一个库，先拿到别人的改动再动手。
func (e *Engine) Pull(ctx context.Context) (Result, error) {
	if e.Remote == nil {
		return Result{}, errors.New("同步：还没有配置远端")
	}
	now := time.Now
	if e.Now != nil {
		now = e.Now
	}
	local, err := scanLocal(e.Root, e.Ignore)
	if err != nil {
		return Result{}, err
	}
	if f, ok := e.Remote.(LocalFilter); ok {
		if local, err = f.FilterLocal(ctx, local); err != nil {
			return Result{}, fmt.Errorf("同步：整理本地文件失败：%w", err)
		}
	}
	remote, err := e.Remote.List(ctx)
	if err != nil {
		return Result{}, fmt.Errorf("同步：读取云端失败：%w", err)
	}

	state, first := e.loadState()

	names := make([]string, 0, len(remote))
	for rel := range remote {
		if !e.Ignore.Skip(rel, false) {
			names = append(names, rel)
		}
	}
	sort.Strings(names)

	var result Result
	keepName := "" // 覆盖本地前的存档目录，用到才定名字

	for _, rel := range names {
		if ctx.Err() != nil {
			result.Stopped = "拉取被中断（超时或取消）：已经下来的都留着，再拉一次会接着来"
			break
		}
		rh := remote[rel].Version
		lh := local[rel]
		base := state.Files[rel]

		// 远端这一版就是上次同步时那一版：没有可拉的
		if !first && base.Remote == rh {
			continue
		}
		// 本地也改过：拉取不碰它，留给一次完整同步去合并（那边会保留本地副本）
		if !first && lh != "" && base.Local != lh {
			result.Pending++
			continue
		}
		data, err := e.Remote.Get(ctx, rel)
		if err != nil {
			var limited *rateLimited
			if errors.As(err, &limited) {
				result.Stopped = limited.Error()
				break
			}
			result.Failed = append(result.Failed, Failure{Path: rel, Reason: fmt.Sprintf("下载 %s 失败：%s", rel, err)})
			continue
		}
		if h := hash(data); h == lh {
			// 内容一样（例如上次部分上传过）：只补记状态，不写盘
			state.Files[rel] = FileState{Local: lh, Remote: rh}
			continue
		}
		if lh != "" {
			if keepName == "" {
				keepName = "before-pull-" + now().Format("2006-01-02-150405")
			}
			old, err := os.ReadFile(filepath.Join(e.Root, filepath.FromSlash(rel)))
			if err != nil {
				result.Failed = append(result.Failed, Failure{Path: rel, Reason: fmt.Sprintf("读取本地文件 %s 失败：%s", rel, err)})
				continue
			}
			if err := writeLocal(e.Root, path.Join(".papero", keepName, rel), old); err != nil {
				result.Failed = append(result.Failed, Failure{Path: rel, Reason: fmt.Sprintf("保存本地版本 %s 失败：%s", rel, err)})
				continue
			}
		}
		if err := writeLocal(e.Root, rel, data); err != nil {
			result.Failed = append(result.Failed, Failure{Path: rel, Reason: fmt.Sprintf("写入本地 %s 失败：%s", rel, err)})
			continue
		}
		state.Files[rel] = FileState{Local: hash(data), Remote: rh}
		result.Downloaded++
		result.Updated = append(result.Updated, rel)
	}

	e.saveState(state)
	return result, nil
}

// scanLocal 返回本地全部文件的摘要。filter 说哪些不扫（目录会被整棵跳过）。
func scanLocal(root string, filter Filter) (map[string]string, error) {
	out := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if d.IsDir() {
			if filter.Skip(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if filter.Skip(rel, false) {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil
		}
		out[rel] = hash(data)
		return nil
	})
	return out, err
}

// hash 返回内容的 SHA-256 摘要。
func hash(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// versionOr 是写入后的版本；服务器没给版本时用本地摘要顶上，下一次 List
// 会给出真值——那之前它只是「上次同步时远端就是这份内容」的意思。
func versionOr(e Entry, fallback string) string {
	if e.Version != "" {
		return e.Version
	}
	return fallback
}

// writeLocal 原子写入本地文件。
func writeLocal(root, rel string, data []byte) error {
	clean, err := safeRel(rel)
	if err != nil {
		return err
	}
	abs := filepath.Join(root, filepath.FromSlash(clean))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".papero-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	return os.Rename(name, abs)
}

// trashLocal 把本地文件移到知识库的 .trash 目录。
func (e *Engine) trashLocal(rel string, now time.Time) error {
	abs := filepath.Join(e.Root, filepath.FromSlash(rel))
	if _, err := os.Stat(abs); err != nil {
		return nil
	}
	dir := filepath.Join(e.Root, ".trash")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	name := now.Format("20060102-150405") + "-" + filepath.Base(abs)
	return os.Rename(abs, filepath.Join(dir, name))
}

// conflictName 生成冲突副本的文件名。
func conflictName(rel string, now time.Time) string {
	dir := path.Dir(rel)
	base := path.Base(rel)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	name := fmt.Sprintf("%s (冲突 %s)%s", stem, now.Format("2006-01-02 15-04-05"), ext)
	if dir == "." {
		return name
	}
	return path.Join(dir, name)
}

// loadState 读取同步状态，并说明这是不是第一次接上这个远端。
//
// 状态按远端的描述记（类型 + 地址 + 分支），不只记类型：换文件夹或换仓库时，
// 旧状态里「远端还有这些文件」会把新远端读成「文件全被删了」，本地整库被搬进
// 回收站。旧版本只记了「dir」「git」，按类型认下来，不把老用户的记录清零。
func (e *Engine) loadState() (*State, bool) {
	state := &State{Files: map[string]FileState{}}
	data, err := os.ReadFile(statePath(e.Root))
	if err != nil {
		return state, true
	}
	if err := json.Unmarshal(data, state); err != nil {
		return &State{Files: map[string]FileState{}}, true
	}
	if state.Files == nil {
		state.Files = map[string]FileState{}
	}
	if e.Remote != nil && state.Remote != e.Remote.Descriptor() && state.Remote != e.Remote.Kind() {
		// 换了远端：重新比对，按「加入」处理。
		return &State{Files: map[string]FileState{}}, true
	}
	return state, state.Remote == ""
}

// saveState 写入同步状态。
func (e *Engine) saveState(state *State) {
	path := statePath(e.Root)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	if e.Remote != nil {
		state.Remote = e.Remote.Descriptor()
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return
	}
	os.WriteFile(path, data, 0o644)
}

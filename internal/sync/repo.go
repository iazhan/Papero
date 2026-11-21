package sync

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	stdsync "sync"
)

// RepoRemote 直接用知识库自己的 git 仓库：库根就是仓库根，远端是 origin。
//
// 和 GitRemote 的区别是根本性的：GitRemote 另建一份副本（`.papero/git`）当远端，
// 要填仓库、分支、令牌；这里读的是 `origin/<分支>` 那棵树，写的是工作区里那几个
// 文件，最后只提交这些路径。所以什么都不用填——仓库从 origin 读，分支从 HEAD 读，
// 凭据用系统里已经配好的（credential helper 或 SSH key）。
//
// 三条硬规矩：
//
//   - 只提交本轮同步碰过的路径（`commit -- <paths>`），不碰你 index 里其它东西；
//   - 从不 reset、不 stash、不 checkout、不切分支——工作区是你的；
//   - 被 .gitignore 忽略的文件不同步：它们进不了仓库，也就到不了远端。
type RepoRemote struct {
	// Root 是知识库根目录，必须就是仓库根。
	Root string
	// Branch 空 = 当前分支。
	Branch string

	mu      stdsync.Mutex
	ready   bool
	branch  string // 用起来的分支，ensure 之后有值
	repo    string // origin 的地址，报错和「测试连接」里用
	filters bool   // 提交时会不会改行尾（core.autocrlf / core.eol）
	touched map[string]bool
}

// Kind 返回 repo。
func (r *RepoRemote) Kind() string { return "repo" }

// Descriptor 是库根加分支：换了分支就是换了远端。
func (r *RepoRemote) Descriptor() string {
	root, err := filepath.Abs(r.Root)
	if err != nil {
		root = r.Root
	}
	r.mu.Lock()
	branch := r.branch
	r.mu.Unlock()
	return "repo:" + filepath.Clean(root) + "#" + branch
}

// branchName 是实际用的分支。
func (r *RepoRemote) branchName() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.branch
}

// ensure 确认库根就是仓库根、有 origin、有分支。只做一次。
func (r *RepoRemote) ensure(ctx context.Context) error {
	r.mu.Lock()
	done := r.ready
	r.mu.Unlock()
	if done {
		return nil
	}

	root, err := filepath.Abs(r.Root)
	if err != nil {
		root = r.Root
	}
	out, err := r.git(ctx, "rev-parse", "--show-toplevel")
	if err != nil {
		return fmt.Errorf("%s 不是一个 git 仓库：%w", root, err)
	}
	if top := strings.TrimSpace(out); !samePath(top, root) {
		// 两个路径都印出来：真出问题时（另一台机器上的写法差异）只看一个没法判断
		return fmt.Errorf("知识库是仓库 %s 的子目录（库根 %s）：这种模式要求库根本身就是仓库根", top, root)
	}
	url, err := r.git(ctx, "remote", "get-url", "origin")
	if err != nil {
		return errors.New("这个仓库没有 origin：先 git remote add origin <地址>，再同步")
	}

	branch := strings.TrimSpace(r.Branch)
	if branch == "" {
		// 用 symbolic-ref 而不是 rev-parse：仓库还没有第一个提交时 HEAD 指向
		// 一个不存在的分支，rev-parse 会直接失败。
		b, err := r.git(ctx, "symbolic-ref", "--short", "HEAD")
		if err != nil {
			return errors.New("仓库在一个游离的提交上（detached HEAD）：先切回一个分支再同步")
		}
		branch = strings.TrimSpace(b)
	}
	if branch == "" {
		return errors.New("看不出当前分支：先 git checkout 到一个分支再同步")
	}
	// 提交要作者身份。没有的话 git 自己的报错很长，这里先说清楚。
	if _, err := r.git(ctx, "var", "GIT_AUTHOR_IDENT"); err != nil {
		return errors.New("这个仓库还没有配置提交身份：先 git config user.name 与 user.email（或设到全局）再同步")
	}

	r.mu.Lock()
	r.ready, r.branch, r.repo, r.filters = true, branch, strings.TrimSpace(url), r.filtersOn(ctx)
	r.mu.Unlock()
	return nil
}

// filtersOn 是仓库会不会在提交时转换行尾。
//
// 开着的话内容摘要不能自己算：工作区是 CRLF、仓库里存的是 LF，自己按工作区
// 算出来的 SHA 和 ls-tree 报的对不上，每轮同步都会把文件重下一遍（还会把
// 文件的行尾悄悄改成 LF）。Windows 上 git 的默认配置就是 true。
func (r *RepoRemote) filtersOn(ctx context.Context) bool {
	out, code, err := r.run(ctx, nil, "config", "--get", "core.autocrlf")
	if err == nil && code == 0 {
		if v := strings.ToLower(strings.TrimSpace(string(out))); v != "" && v != "false" {
			return true
		}
	}
	out, code, err = r.run(ctx, nil, "config", "--get", "core.eol")
	if err == nil && code == 0 && strings.EqualFold(strings.TrimSpace(string(out)), "crlf") {
		return true
	}
	return false
}

// samePath 报告两个路径是不是同一个目录。
//
// 先比文件本身，再退回比字符串：Windows 上同一个目录有很多种写法——大小写、正反斜杠、
// 8.3 短名（RUNNER~1）、junction。GitHub 的 runner 上 `git rev-parse --show-toplevel`
// 回的常常不是 Go 那一侧拿到的写法，纯字符串比较会把「库根就是仓库根」误判成
// 「库是仓库的子目录」，同步直接报错退出。
func samePath(a, b string) bool {
	if sa, err := os.Stat(a); err == nil {
		if sb, err := os.Stat(b); err == nil {
			return os.SameFile(sa, sb)
		}
	}
	ca, err := filepath.Abs(a)
	if err != nil {
		return false
	}
	cb, err := filepath.Abs(b)
	if err != nil {
		return false
	}
	ca, cb = filepath.Clean(ca), filepath.Clean(cb)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(ca, cb)
	}
	return ca == cb
}

// git 在库根里跑一条 git 命令，返回输出。
func (r *RepoRemote) git(ctx context.Context, args ...string) (string, error) {
	out, code, err := r.run(ctx, nil, args...)
	if err != nil {
		return string(out), err
	}
	if code != 0 {
		return string(out), fmt.Errorf("git %s：%s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// run 跑一条 git 命令，把退出码单独交出来：有几处「非零但正常」（check-ignore
// 没命中是 1）。
func (r *RepoRemote) run(ctx context.Context, stdin []byte, args ...string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = r.Root
	// 没有凭据时直接失败，不要卡在一个等密码的提示上
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	if err == nil {
		return buf.Bytes(), 0, nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return buf.Bytes(), exit.ExitCode(), nil
	}
	return buf.Bytes(), -1, err
}

// FilterLocal 去掉被 .gitignore 忽略的本地文件。
//
// 这些文件永远进不了仓库，硬要同步只会每轮提交失败，或者被当成「远端删了」
// 挪进回收站。一次调用一条 git 命令（`check-ignore --stdin`），不是每个文件
// 起一个进程。
func (r *RepoRemote) FilterLocal(ctx context.Context, local map[string]string) (map[string]string, error) {
	if len(local) == 0 {
		return local, nil
	}
	if err := r.ensure(ctx); err != nil {
		return nil, err
	}
	names := make([]string, 0, len(local))
	for rel := range local {
		names = append(names, rel)
	}
	sort.Strings(names)

	out, code, err := r.run(ctx, []byte(strings.Join(names, "\x00")+"\x00"), "check-ignore", "--stdin", "-z")
	if err != nil {
		return nil, err
	}
	switch code {
	case 0: // 有命中的
	case 1: // 一个都没命中
		return local, nil
	default:
		return nil, fmt.Errorf("git check-ignore：%s", strings.TrimSpace(string(out)))
	}
	kept := make(map[string]string, len(local))
	for rel, h := range local {
		kept[rel] = h
	}
	for _, rel := range strings.Split(string(out), "\x00") {
		if rel != "" {
			delete(kept, rel)
		}
	}
	return kept, nil
}

// List 是 origin/<分支> 那棵树，不是工作区：读工作区的话两端永远一样，什么都
// 同步不了。
func (r *RepoRemote) List(ctx context.Context) (map[string]Entry, error) {
	if err := r.ensure(ctx); err != nil {
		return nil, err
	}
	branch := r.branchName()
	if err := r.fetch(ctx, branch); err != nil {
		return nil, err
	}
	out, code, err := r.run(ctx, nil, "ls-tree", "-r", "-z", "origin/"+branch)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		// 远端还没有这个分支（空仓库、或第一次同步）
		if ok, err := r.remoteHasBranch(ctx, branch); err == nil && !ok {
			return map[string]Entry{}, nil
		}
		return nil, fmt.Errorf("git ls-tree origin/%s：%s", branch, strings.TrimSpace(string(out)))
	}
	entries := map[string]Entry{}
	for _, rec := range strings.Split(string(out), "\x00") {
		if rec == "" {
			continue
		}
		meta, rel, ok := strings.Cut(rec, "\t")
		if !ok || rel == "" {
			continue
		}
		f := strings.Fields(meta)
		// 只认 blob：子模块在树里是 commit，同步不了它的内容
		if len(f) < 3 || f[1] != "blob" {
			continue
		}
		entries[rel] = Entry{Version: f[2]}
	}
	return entries, nil
}

// fetch 把 origin/<分支> 拉到本地引用上。拉失败要分清「远端还没有这个分支」
// 和「真的连不上」——后者当成空远端会拿旧数据去覆盖。
func (r *RepoRemote) fetch(ctx context.Context, branch string) error {
	out, code, err := r.run(ctx, nil, "fetch", "--quiet", "origin", branch)
	if err != nil {
		return err
	}
	if code == 0 {
		return nil
	}
	ok, err := r.remoteHasBranch(ctx, branch)
	if err != nil {
		return fmt.Errorf("取 %s 失败：%s", branch, strings.TrimSpace(string(out)))
	}
	if !ok {
		return nil // 远端还没有这个分支：第一次同步
	}
	return fmt.Errorf("fetch origin %s 失败：%s", branch, strings.TrimSpace(string(out)))
}

// remoteHasBranch 是远端有没有这个分支。
func (r *RepoRemote) remoteHasBranch(ctx context.Context, branch string) (bool, error) {
	out, code, err := r.run(ctx, nil, "ls-remote", "--heads", "origin", branch)
	if err != nil {
		return false, err
	}
	if code != 0 {
		return false, fmt.Errorf("ls-remote origin %s：%s", branch, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)) != "", nil
}

// Get 读 origin/<分支> 里的那个文件。
func (r *RepoRemote) Get(ctx context.Context, rel string) ([]byte, error) {
	if err := r.ensure(ctx); err != nil {
		return nil, err
	}
	out, code, err := r.run(ctx, nil, "show", "origin/"+r.branchName()+":"+rel)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("读取 %s：%s", rel, strings.TrimSpace(string(out)))
	}
	return out, nil
}

// Put 把内容写进工作区并记下路径，Flush 时才提交。
func (r *RepoRemote) Put(ctx context.Context, rel string, data []byte, base string) (Entry, error) {
	if err := r.ensure(ctx); err != nil {
		return Entry{}, err
	}
	if err := writeLocal(r.Root, rel, data); err != nil {
		return Entry{}, err
	}
	r.mark(rel)

	ver := blobSHA(data)
	r.mu.Lock()
	filters := r.filters
	r.mu.Unlock()
	if filters {
		// 让 git 按这个路径的转换规则算摘要，否则和 ls-tree 报的永远差一截
		if out, code, err := r.run(ctx, data, "hash-object", "--path="+rel, "--stdin"); err == nil && code == 0 {
			if s := strings.TrimSpace(string(out)); s != "" {
				ver = s
			}
		}
	}
	return Entry{Version: ver}, nil
}

// Delete 记下路径。文件已经从工作区里删掉了（引擎是先删本地再叫这里的），
// 提交时 `add -A -- 路径` 会把删除也带上。
func (r *RepoRemote) Delete(ctx context.Context, rel string) error {
	if err := r.ensure(ctx); err != nil {
		return err
	}
	r.mark(rel)
	return nil
}

// Flush 提交并推送本轮碰过的路径。
func (r *RepoRemote) Flush(ctx context.Context) error {
	paths := r.taken()
	if len(paths) == 0 {
		return nil
	}
	// 只关心这些路径有没有变：你在别处改的、暂存的都不算
	out, code, err := r.run(ctx, nil, append([]string{"status", "--porcelain", "-z", "--"}, paths...)...)
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("git status：%s", strings.TrimSpace(string(out)))
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return nil // 内容和远端一样，没有要提交的
	}

	// add 只加这些路径（新文件必须 add 才进得了提交）
	if out, code, err := r.run(ctx, nil, append([]string{"add", "-A", "--"}, paths...)...); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("git add：%s", strings.TrimSpace(string(out)))
	}
	// commit 也带 pathspec：不把你 index 里别的东西一起提交
	args := append([]string{"commit", "-m", "papero: 同步", "--"}, paths...)
	if out, code, err := r.run(ctx, nil, args...); err != nil {
		return err
	} else if code != 0 {
		return fmt.Errorf("git commit：%s", strings.TrimSpace(string(out)))
	}

	branch := r.branchName()
	out, code, err = r.run(ctx, nil, "push", "origin", "HEAD:"+branch)
	if err != nil {
		return err
	}
	if code != 0 {
		msg := string(out)
		if strings.Contains(msg, "non-fast-forward") || strings.Contains(msg, "[rejected]") || strings.Contains(msg, "fetch first") {
			return fmt.Errorf("远端分支 %s 有新提交，这次的改动没有发出去：再同步一次", branch)
		}
		return fmt.Errorf("git push：%s", strings.TrimSpace(msg))
	}
	return nil
}

// Probe 验仓库、origin、分支都在，并报出是哪个仓库。
func (r *RepoRemote) Probe(ctx context.Context) (ProbeResult, error) {
	if err := r.ensure(ctx); err != nil {
		return ProbeResult{}, err
	}
	branch := r.branchName()
	r.mu.Lock()
	repo := r.repo
	r.mu.Unlock()
	ok, err := r.remoteHasBranch(ctx, branch)
	if err != nil {
		return ProbeResult{}, err
	}
	detail := repo + " · " + branch
	if !ok {
		return ProbeResult{New: true, Detail: detail}, nil
	}
	return ProbeResult{Files: -1, Detail: detail}, nil
}

// mark 记下本轮碰过的路径。
func (r *RepoRemote) mark(rel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.touched == nil {
		r.touched = map[string]bool{}
	}
	r.touched[rel] = true
}

// taken 取出这些路径（排好序，命令行参数稳定）。
func (r *RepoRemote) taken() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.touched))
	for rel := range r.touched {
		out = append(out, rel)
	}
	sort.Strings(out)
	return out
}

// blobSHA 是 git 给这份内容算的 blob SHA，和 ls-tree 报的版本是同一个东西。
func blobSHA(data []byte) string {
	h := sha1.New()
	fmt.Fprintf(h, "blob %d\x00", len(data))
	h.Write(data)
	return hex.EncodeToString(h.Sum(nil))
}

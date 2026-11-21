package sync

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// GitRemote 用 GitHub 仓库同步：Papero 在本地维护一份仓库副本，
// 文件操作写在副本里，Flush 时一次性提交并推送。它依赖系统里装了 git。
type GitRemote struct {
	// Repo 是 owner/name。
	Repo string
	// Branch 是分支，默认 main。
	Branch string
	// Token 是 GitHub 令牌（需要 repo 权限）。
	Token string
	// Dir 是本地副本目录。
	Dir string
}

// Kind 返回 git。
func (g *GitRemote) Kind() string { return "git" }

// Descriptor 是仓库加分支：换仓库或换分支就是换了个远端，旧状态作废。
func (g *GitRemote) Descriptor() string { return "git:" + g.Repo + "#" + g.branch() }

// branch 是实际用的分支。
func (g *GitRemote) branch() string {
	if g.Branch == "" {
		return "main"
	}
	return g.Branch
}

// remoteURL 是不带令牌的仓库地址。
func (g *GitRemote) remoteURL() string { return "https://github.com/" + g.Repo + ".git" }

// auth 把令牌放进这次请求的头里，而不是拼进地址。
//
// 拼进地址（https://x-access-token:<令牌>@…）时，git 会把它原样写进副本的
// .git/config，令牌就留在磁盘上了——而副本就在知识库里，整个文件夹都同步出去
// 的话令牌也跟着走。extraheader 只作用于这一条命令，不落盘。
func (g *GitRemote) auth() []string {
	if g.Token == "" {
		return nil
	}
	basic := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + g.Token))
	return []string{"-c", "http.extraheader=Authorization: Basic " + basic}
}

// git 在副本目录里运行一条 git 命令。
func (g *GitRemote) git(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append(g.auth(), args...)...)
	cmd.Dir = g.Dir
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=Papero", "GIT_AUTHOR_EMAIL=papero@localhost",
		"GIT_COMMITTER_NAME=Papero", "GIT_COMMITTER_EMAIL=papero@localhost",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return string(out), fmt.Errorf("git %s：%s", strings.Join(args, " "), strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// ensure 准备本地副本：没有就克隆，有就拉取最新。
func (g *GitRemote) ensure(ctx context.Context) error {
	branch := g.branch()
	if _, err := os.Stat(filepath.Join(g.Dir, ".git")); err != nil {
		if err := os.MkdirAll(filepath.Dir(g.Dir), 0o755); err != nil {
			return err
		}
		os.RemoveAll(g.Dir)
		args := append(g.auth(), "clone", "--depth", "1", "--branch", branch, g.remoteURL(), g.Dir)
		cmd := exec.CommandContext(ctx, "git", args...)
		cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
		if out, err := cmd.CombinedOutput(); err != nil {
			// 空仓库没有分支，改为初始化后关联远端。
			if err := g.initEmpty(ctx, branch); err != nil {
				return fmt.Errorf("克隆仓库失败：%s", strings.TrimSpace(string(out)))
			}
		}
		return nil
	}
	if _, err := g.git(ctx, "fetch", "--depth", "1", "origin", branch); err != nil {
		return err
	}
	// 老版本把令牌拼在地址里，副本的 .git/config 里还留着：换成干净地址。
	// 只在真发现令牌时才跑一条命令，平时不多一次进程。
	if b, err := os.ReadFile(filepath.Join(g.Dir, ".git", "config")); err == nil && strings.Contains(string(b), "x-access-token:") {
		if _, err := g.git(ctx, "remote", "set-url", "origin", g.remoteURL()); err != nil {
			return err
		}
	}
	g.git(ctx, "reset", "--hard", "origin/"+branch)
	return nil
}

// initEmpty 处理一个还没有提交的仓库。
func (g *GitRemote) initEmpty(ctx context.Context, branch string) error {
	os.RemoveAll(g.Dir)
	if err := os.MkdirAll(g.Dir, 0o755); err != nil {
		return err
	}
	if _, err := g.git(ctx, "init"); err != nil {
		return err
	}
	if _, err := g.git(ctx, "remote", "add", "origin", g.remoteURL()); err != nil {
		return err
	}
	g.git(ctx, "checkout", "-b", branch)
	return nil
}

// List 返回仓库里的文件。
func (g *GitRemote) List(ctx context.Context) (map[string]Entry, error) {
	if err := g.ensure(ctx); err != nil {
		return nil, err
	}
	hashes, err := scanLocal(g.Dir, Filter{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]Entry, len(hashes))
	for rel, h := range hashes {
		out[rel] = Entry{Version: h, Hash: h}
	}
	return out, nil
}

// Get 读取仓库副本里的文件。
func (g *GitRemote) Get(ctx context.Context, rel string) ([]byte, error) {
	return os.ReadFile(filepath.Join(g.Dir, filepath.FromSlash(rel)))
}

// Put 写入仓库副本里的文件，Flush 时才提交。
//
// base 不用：改动是整批提交的，一次推送要么整个成功要么被拒，落单的文件不会
// 出现「版本对不上」这种情形。
func (g *GitRemote) Put(ctx context.Context, rel string, data []byte, base string) (Entry, error) {
	if err := writeLocal(g.Dir, rel, data); err != nil {
		return Entry{}, err
	}
	h := hash(data)
	return Entry{Version: h, Hash: h}, nil
}

// Delete 删除仓库副本里的文件。
func (g *GitRemote) Delete(ctx context.Context, rel string) error {
	err := os.Remove(filepath.Join(g.Dir, filepath.FromSlash(rel)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Probe 验仓库和分支能不能访问。用 ls-remote：只问不拉，比克隆快得多，也不动
// 本地副本。分支还没有时 ls-remote 也是成功的、只是没输出，那就是「空的」。
func (g *GitRemote) Probe(ctx context.Context) (ProbeResult, error) {
	// git 在副本目录里跑，目录得先在
	if err := os.MkdirAll(g.Dir, 0o755); err != nil {
		return ProbeResult{}, err
	}
	out, err := g.git(ctx, "ls-remote", "--heads", g.remoteURL(), g.branch())
	if err != nil {
		return ProbeResult{}, err
	}
	if strings.TrimSpace(out) == "" {
		return ProbeResult{New: true, Detail: g.Repo + " · " + g.branch()}, nil
	}
	// 没数文件：数一遍要先把仓库拉下来，那是同步本身干的事
	return ProbeResult{Files: -1, Detail: g.Repo + " · " + g.branch()}, nil
}

// Flush 提交并推送改动。
func (g *GitRemote) Flush(ctx context.Context) error {
	if _, err := g.git(ctx, "add", "-A"); err != nil {
		return err
	}
	if out, err := g.git(ctx, "status", "--porcelain"); err == nil && strings.TrimSpace(out) == "" {
		return nil
	}
	if _, err := g.git(ctx, "commit", "-m", "papero: 同步"); err != nil {
		return err
	}
	branch := g.branch()
	out, err := g.git(ctx, "push", "origin", "HEAD:"+branch)
	if err != nil {
		// 另一台设备在这一次同步的过程中提交了：本地副本已经过期，推送被拒。
		// 说清楚要再同步一次，别把 git 的原始输出直接丢给用户。
		switch {
		case strings.Contains(out, "non-fast-forward"),
			strings.Contains(out, "fetch first"),
			strings.Contains(out, "[rejected]"):
			return fmt.Errorf("远端分支 %s 有新提交，这次的改动没有发出去：再同步一次即可", branch)
		}
		return err
	}
	return nil
}

// flusher 是可选的「收尾」接口：像 git 这样需要批量提交的远端实现它。
type flusher interface {
	Flush(ctx context.Context) error
}

// flush 在同步结束时调用远端的收尾动作。
func flush(ctx context.Context, r Remote) error {
	if f, ok := r.(flusher); ok {
		return f.Flush(ctx)
	}
	return nil
}

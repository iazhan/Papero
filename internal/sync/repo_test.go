package sync

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// 本库仓库模式的测试要真的 git：远端用本地 bare 仓库，不碰网络。

func needGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("没有 git")
	}
}

// runGit 在 dir 里跑一条 git，配置隔离到临时 HOME，不受本机 gitconfig 影响。
func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"HOME="+t.TempDir(), "USERPROFILE="+t.TempDir(),
		"GIT_CONFIG_NOSYSTEM=1", "GIT_TERMINAL_PROMPT=0",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s：%v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

// newRepoVault 建一个「库根就是仓库根」的知识库，origin 指向一个本地 bare 仓库。
func newRepoVault(t *testing.T) (vault, bare string) {
	t.Helper()
	needGit(t)
	bare = t.TempDir()
	runGit(t, bare, "init", "--bare", "-q")
	runGit(t, bare, "symbolic-ref", "HEAD", "refs/heads/main")
	// 断言里要看得到中文路径：默认 ls-tree 会把非 ASCII 转义成八进制
	runGit(t, bare, "config", "core.quotePath", "false")

	vault = t.TempDir()
	runGit(t, vault, "init", "-q")
	runGit(t, vault, "remote", "add", "origin", bare)
	runGit(t, vault, "symbolic-ref", "HEAD", "refs/heads/main")
	// 提交身份：真实仓库都有（本地或全局），这里给个仓库级的
	runGit(t, vault, "config", "user.name", "test")
	runGit(t, vault, "config", "user.email", "test@example.com")
	return vault, bare
}

// 一轮完整往返：上传、远端真的收到、第二次无事可做、另一台克隆下来不用重下。
func TestRepoRemoteRoundTrip(t *testing.T) {
	vault, bare := newRepoVault(t)
	writeFile(t, vault, "a.md", "一")
	writeFile(t, vault, "notes/我的 笔记.md", "中文与空格")

	res, err := (&Engine{Root: vault, Remote: &RepoRemote{Root: vault}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 2 {
		t.Fatalf("上传 %d 个，想要 2（%s）", res.Uploaded, res.Summary())
	}
	tree := runGit(t, bare, "ls-tree", "-r", "--name-only", "main")
	for _, want := range []string{"a.md", "notes/我的 笔记.md"} {
		if !strings.Contains(tree, want) {
			t.Errorf("远端树里没有 %q：\n%s", want, tree)
		}
	}

	// 内容已经落地：第二次不该有动作（这一条同时盯住「版本令牌与远端对不上」
	// 的病——那样每轮都会重传或重下）
	res, err = (&Engine{Root: vault, Remote: &RepoRemote{Root: vault}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded+res.Downloaded+res.Deleted != 0 {
		t.Fatalf("第二次仍有动作：%s", res.Summary())
	}

	// 另一台：克隆下来，内容一样 → 既不下载也不产生冲突副本
	other := t.TempDir()
	runGit(t, other, "clone", "-q", bare, ".")
	runGit(t, other, "config", "user.name", "test")
	runGit(t, other, "config", "user.email", "test@example.com")
	res, err = (&Engine{Root: other, Remote: &RepoRemote{Root: other}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 0 || res.Joined != 0 || len(res.Conflicts) != 0 {
		t.Fatalf("内容相同却动了：%s", res.Summary())
	}

	// 这一台改一个文件：照常上传
	writeFile(t, other, "b.md", "二")
	res, err = (&Engine{Root: other, Remote: &RepoRemote{Root: other}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 {
		t.Fatalf("上传 %d 个，想要 1（%s）", res.Uploaded, res.Summary())
	}
	if tree := runGit(t, bare, "ls-tree", "-r", "--name-only", "main"); !strings.Contains(tree, "b.md") {
		t.Errorf("远端没有收到 b.md：\n%s", tree)
	}
}

// 被 .gitignore 忽略的文件不同步，也不能因此被当成「远端删了」挪走。
func TestRepoRemoteSkipsGitignored(t *testing.T) {
	vault, bare := newRepoVault(t)
	writeFile(t, vault, ".gitignore", "*.tmp\n")
	writeFile(t, vault, "a.md", "一")
	writeFile(t, vault, "scratch.tmp", "临时")

	res, err := (&Engine{Root: vault, Remote: &RepoRemote{Root: vault}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 2 { // .gitignore 与 a.md
		t.Fatalf("上传 %d 个，想要 2（%s）", res.Uploaded, res.Summary())
	}
	if tree := runGit(t, bare, "ls-tree", "-r", "--name-only", "main"); strings.Contains(tree, "scratch.tmp") {
		t.Errorf("被忽略的文件上传了：\n%s", tree)
	}

	res, err = (&Engine{Root: vault, Remote: &RepoRemote{Root: vault}}).Run(context.Background())
	if err != nil {
		t.Fatalf("第二轮报错：%v", err)
	}
	if res.Uploaded+res.Downloaded+res.Deleted != 0 {
		t.Errorf("第二轮仍有动作：%s", res.Summary())
	}
	if _, err := os.Stat(filepath.Join(vault, "scratch.tmp")); err != nil {
		t.Error("本地临时文件被删了")
	}
}

// 只提交本轮碰过的路径：用户自己暂存到 .papero 里的东西不该跟着走。
func TestRepoRemoteOnlyCommitsItsPaths(t *testing.T) {
	vault, bare := newRepoVault(t)
	writeFile(t, vault, "a.md", "一")

	// 用户在 .papero 里暂存了一个文件（引擎忽略这个目录）
	writeFile(t, vault, ".papero/draft.txt", "草稿")
	runGit(t, vault, "add", ".papero/draft.txt")

	if _, err := (&Engine{Root: vault, Remote: &RepoRemote{Root: vault}}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	if tree := runGit(t, bare, "ls-tree", "-r", "--name-only", "main"); strings.Contains(tree, ".papero") {
		t.Errorf(".papero 被提交了：\n%s", tree)
	}
	// 用户暂存的东西还在 index 里，没被顺手提交
	if st := runGit(t, vault, "status", "--porcelain"); !strings.Contains(st, ".papero/draft.txt") {
		t.Errorf("用户的暂存被动了：\n%s", st)
	}
}

// autocrlf 开着时，版本令牌必须按 git 转换后的算：否则每轮同步都会把文件
// 重下一遍，行尾还被悄悄改成 LF。
func TestRepoRemoteWithAutocrlf(t *testing.T) {
	vault, bare := newRepoVault(t)
	runGit(t, vault, "config", "core.autocrlf", "true")
	writeFile(t, vault, "a.md", "第一行\r\n第二行\r\n")

	if _, err := (&Engine{Root: vault, Remote: &RepoRemote{Root: vault}}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Root: vault, Remote: &RepoRemote{Root: vault}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded+res.Downloaded != 0 {
		t.Errorf("开了 autocrlf 后第二轮仍在动：%s", res.Summary())
	}
	if got, err := os.ReadFile(filepath.Join(vault, "a.md")); err != nil || !strings.Contains(string(got), "\r\n") {
		t.Errorf("工作区的行尾被改了：%q", got)
	}
	_ = bare
}

// 不是仓库、或者库只是仓库的一部分：说清楚原因，不要瞎同步。
func TestRepoRemoteRefusesWrongLayout(t *testing.T) {
	needGit(t)
	plain := t.TempDir()
	if _, err := (&RepoRemote{Root: plain}).List(context.Background()); err == nil {
		t.Fatal("不是仓库时应当报错")
	} else if !strings.Contains(err.Error(), "不是一个 git 仓库") {
		t.Errorf("错误没说清：%v", err)
	}

	// 库是仓库的子目录
	repo := t.TempDir()
	runGit(t, repo, "init", "-q")
	sub := filepath.Join(repo, "notes")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := (&RepoRemote{Root: sub}).List(context.Background()); err == nil {
		t.Fatal("子目录时应当报错")
	} else if !strings.Contains(err.Error(), "子目录") {
		t.Errorf("错误没说清：%v", err)
	}
}

// TestSamePathAcrossSpellings 同一个目录换个写法也要认出来：Windows 上
// junction、8.3 短名、正反斜杠都会让「库根 = 仓库根」看起来是两个路径。
//
// GitHub 的 runner 就是这么翻车的：git rev-parse 回的 toplevel 和 Go 拿到的
// 临时目录写法不一样，四个 TestRepoRemote* 全报「库是仓库的子目录」。
func TestSamePathAcrossSpellings(t *testing.T) {
	dir := t.TempDir()
	if !samePath(dir, dir) {
		t.Fatal("自己和自己都不等")
	}
	// 正反斜杠与大小写
	if !samePath(filepath.ToSlash(dir), dir) {
		t.Error("正斜杠写法没认出来")
	}
	if runtime.GOOS == "windows" && !samePath(strings.ToUpper(dir), dir) {
		t.Error("全大写写法没认出来")
	}
	if samePath(dir, filepath.Join(dir, "sub")) {
		t.Error("父目录和子目录不该相等")
	}

	// junction：同一个目录的另一个名字（mklink /J 不需要管理员）
	if runtime.GOOS != "windows" {
		return
	}
	link := filepath.Join(t.TempDir(), "link")
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, dir).CombinedOutput(); err != nil {
		t.Skipf("建不了 junction，跳过：%v %s", err, out)
	}
	if !samePath(link, dir) {
		t.Error("junction 与它指向的目录没认出来是同一处")
	}
}

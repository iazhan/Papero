package sync

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newPair 建一对本地/远端目录。
func newPair(t *testing.T) (local, remote string) {
	t.Helper()
	local = t.TempDir()
	remote = t.TempDir()
	return
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, root, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("读取 %s: %v", rel, err)
	}
	return string(data)
}

func engine(local, remote string) *Engine {
	return &Engine{Root: local, Remote: &DirRemote{Root: remote}, Now: func() time.Time {
		return time.Date(2026, 11, 17, 23, 0, 0, 0, time.Local)
	}}
}

func TestUploadAndDownload(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, "笔记/a.md", "本地内容")

	res, err := engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 {
		t.Fatalf("上传 %d 个，想要 1（%s）", res.Uploaded, res.Summary())
	}
	if got := readFile(t, remote, "笔记/a.md"); got != "本地内容" {
		t.Errorf("远端内容 = %q", got)
	}

	// 再跑一次应当无事可做
	res, err = engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded+res.Downloaded+res.Deleted != 0 {
		t.Errorf("第二次同步仍有动作：%s", res.Summary())
	}

	// 另一台设备（新的本地目录）从远端拉取
	other := t.TempDir()
	res, err = engine(other, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 1 {
		t.Fatalf("下载 %d 个，想要 1", res.Downloaded)
	}
	if got := readFile(t, other, "笔记/a.md"); got != "本地内容" {
		t.Errorf("下载内容 = %q", got)
	}
}

func TestConflictKeepsBoth(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, "a.md", "原始")
	if _, err := engine(local, remote).Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	// 两端各改一次
	writeFile(t, local, "a.md", "本地改的")
	writeFile(t, remote, "a.md", "远端改的")

	res, err := engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Conflicts) != 1 {
		t.Fatalf("冲突 %d 个，想要 1", len(res.Conflicts))
	}
	if got := readFile(t, local, "a.md"); got != "远端改的" {
		t.Errorf("正式文件应为远端版本，实际 %q", got)
	}
	copyRel := res.Conflicts[0]
	if !strings.Contains(copyRel, "冲突") {
		t.Errorf("冲突副本名字不对：%q", copyRel)
	}
	if got := readFile(t, local, copyRel); got != "本地改的" {
		t.Errorf("冲突副本内容 = %q", got)
	}
}

func TestDeletePropagates(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, "a.md", "一")
	writeFile(t, local, "b.md", "二")
	if _, err := engine(local, remote).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(local, "a.md")); err != nil {
		t.Fatal(err)
	}
	res, err := engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 {
		t.Fatalf("删除 %d 个，想要 1（%s）", res.Deleted, res.Summary())
	}
	if _, err := os.Stat(filepath.Join(remote, "a.md")); !os.IsNotExist(err) {
		t.Error("远端文件应已删除")
	}
}

func TestRemoteDeleteGoesToTrash(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, "a.md", "一")
	if _, err := engine(local, remote).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	os.Remove(filepath.Join(remote, "a.md"))
	res, err := engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 {
		t.Fatalf("删除 %d 个，想要 1", res.Deleted)
	}
	if _, err := os.Stat(filepath.Join(local, "a.md")); !os.IsNotExist(err) {
		t.Error("本地文件应已移走")
	}
	entries, _ := os.ReadDir(filepath.Join(local, ".trash"))
	if len(entries) != 1 {
		t.Errorf("回收站里有 %d 个条目，想要 1", len(entries))
	}
}

func TestIgnoreInternalFiles(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, ".papero/sync.json", "{}")
	writeFile(t, local, ".git/config", "x")
	writeFile(t, local, ".trash/old.md", "x")
	writeFile(t, local, "keep.md", "x")
	res, err := engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 {
		t.Fatalf("上传 %d 个，想要 1", res.Uploaded)
	}
}

func TestConflictName(t *testing.T) {
	now := time.Date(2026, 11, 17, 10, 0, 0, 0, time.Local)
	got := conflictName("notes/a.md", now)
	want := "notes/a (冲突 2026-11-17 10-00-00).md"
	if got != want {
		t.Errorf("conflictName = %q，想要 %q", got, want)
	}
}

func TestSafeRel(t *testing.T) {
	if _, err := safeRel("../x"); err == nil {
		t.Error("越界路径应当被拒绝")
	}
	if got, err := safeRel("a/b.md"); err != nil || got != "a/b.md" {
		t.Errorf("safeRel = %q, %v", got, err)
	}
}

// 换一个同步文件夹，本地文件不能被当成「远端删了」搬进回收站。
//
// 状态以前只记远端类型（dir），换文件夹时两份状态被认为是同一个，新文件夹是空的
// 就等价于「远端把文件全删了」——整个库被搬进 .trash。
func TestSwitchRemoteKeepsVault(t *testing.T) {
	local, first := newPair(t)
	writeFile(t, local, "笔记/a.md", "本地内容")
	if _, err := engine(local, first).Run(context.Background()); err != nil {
		t.Fatal(err)
	}

	second := t.TempDir()
	res, err := engine(local, second).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || res.Deleted != 0 {
		t.Fatalf("换文件夹后应当重新上传：%s", res.Summary())
	}
	if got := readFile(t, local, "笔记/a.md"); got != "本地内容" {
		t.Errorf("本地文件被改动了：%q", got)
	}
	if got := readFile(t, second, "笔记/a.md"); got != "本地内容" {
		t.Errorf("新远端没收到文件：%q", got)
	}
}

// 第一次接上一个已经有文件的远端：远端为准，本地那一版收进一个目录，
// 不在文稿树里散出「(冲突 …)」副本。
func TestJoinKeepsLocalVersionAside(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, remote, "a.md", "远端的")
	writeFile(t, remote, "same.md", "一样")
	writeFile(t, local, "a.md", "本地的")
	writeFile(t, local, "same.md", "一样")

	res, err := engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Joined != 1 {
		t.Fatalf("加入 %d 个，想要 1（%s）", res.Joined, res.Summary())
	}
	if len(res.Conflicts) != 0 {
		t.Errorf("首次加入不该产生冲突副本：%v", res.Conflicts)
	}
	if res.JoinDir == "" {
		t.Fatal("没有说本地版本放在哪")
	}
	if got := readFile(t, local, "a.md"); got != "远端的" {
		t.Errorf("正式文件应为远端版本，实际 %q", got)
	}
	if got := readFile(t, local, res.JoinDir+"/a.md"); got != "本地的" {
		t.Errorf("本地版本没保住：%q", got)
	}
	// 内容相同的那一份不算加入，也不该进存档目录
	if _, err := os.Stat(filepath.Join(local, filepath.FromSlash(res.JoinDir), "same.md")); !os.IsNotExist(err) {
		t.Error("内容相同的文件也被存了一份")
	}
	entries, _ := os.ReadDir(local)
	for _, e := range entries {
		if strings.Contains(e.Name(), "冲突") {
			t.Errorf("文稿树里出现了冲突副本：%s", e.Name())
		}
	}

	// 再来一次应当无事可做
	res, err = engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded+res.Downloaded+res.Deleted+res.Joined != 0 {
		t.Errorf("第二次同步仍有动作：%s", res.Summary())
	}
}

// 两端都删掉的文件要从状态里去掉，否则状态文件只增不减。
func TestStateDropsDeletedEntries(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, "a.md", "一")
	if _, err := engine(local, remote).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(local, "a.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(remote, "a.md")); err != nil {
		t.Fatal(err)
	}
	if _, err := engine(local, remote).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	var st State
	data, err := os.ReadFile(statePath(local))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	if len(st.Files) != 0 {
		t.Errorf("状态里还留着 %d 条：%v", len(st.Files), st.Files)
	}
}

// 「测试连接」对文件夹远端：不存在不算错，存在就数文件。
func TestDirProbe(t *testing.T) {
	root := filepath.Join(t.TempDir(), "remote")
	d := &DirRemote{Root: root}

	res, err := d.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if !res.New || res.Files != 0 {
		t.Errorf("文件夹还不存在时：%+v", res)
	}

	writeFile(t, root, "a.md", "一")
	writeFile(t, root, "d/b.md", "二")
	res, err = d.Probe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.New || res.Files != 2 {
		t.Errorf("探到 %+v，想要 2 个文件", res)
	}
}

// 嵌套仓库的 .git 目录以前会被当成普通文件（ignored 只看首段），一路传上去。
func TestNestedGitFolderIgnored(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, "sub/.git/config", "x")
	writeFile(t, local, "sub/note.md", "一")
	res, err := engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 {
		t.Fatalf("上传 %d 个，想要 1（%s）", res.Uploaded, res.Summary())
	}
	if _, err := os.Stat(filepath.Join(remote, "sub", ".git", "config")); !os.IsNotExist(err) {
		t.Error("嵌套的 .git 被上传了")
	}
}

// rawRemote 直接给出一张文件表：试引擎对「远端里有该忽略的路径」怎么处理。
// 真实的远端里只有 WebDAV 与本库仓库会这样（旧版本同步上去的 .git、node_modules）。
type rawRemote struct{ files map[string][]byte }

func (r *rawRemote) Kind() string       { return "raw" }
func (r *rawRemote) Descriptor() string { return "raw:test" }

func (r *rawRemote) List(ctx context.Context) (map[string]Entry, error) {
	out := map[string]Entry{}
	for rel, data := range r.files {
		out[rel] = Entry{Version: hash(data), Hash: hash(data)}
	}
	return out, nil
}

func (r *rawRemote) Get(ctx context.Context, rel string) ([]byte, error) {
	return r.files[rel], nil
}

func (r *rawRemote) Put(ctx context.Context, rel string, data []byte, base string) (Entry, error) {
	r.files[rel] = data
	h := hash(data)
	return Entry{Version: h, Hash: h}, nil
}

func (r *rawRemote) Delete(ctx context.Context, rel string) error {
	delete(r.files, rel)
	return nil
}

// 远端有该忽略的路径时既不下载、也不当成「远端删了」：不然每一轮都会把
// 一个 .git/config 写到工作区里。
func TestRemoteIgnoredPathsAreNotDownloaded(t *testing.T) {
	local := t.TempDir()
	writeFile(t, local, "sub/.git/config", "本地的")
	r := &rawRemote{files: map[string][]byte{
		"sub/.git/config": []byte("远端的"),
		"a.md":            []byte("一"),
	}}
	res, err := (&Engine{Root: local, Remote: r}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 1 {
		t.Fatalf("下载 %d 个，想要 1（%s）", res.Downloaded, res.Summary())
	}
	if got := readFile(t, local, "sub/.git/config"); got != "本地的" {
		t.Errorf("本地 .git/config 被远端覆盖了：%q", got)
	}
}

// failingFlush 是一个「写进去了但提交失败」的远端：git 就是这个样子——
// 文件先落在副本里，推送成功才算数。
type failingFlush struct{ *DirRemote }

func (f failingFlush) Flush(context.Context) error { return errors.New("推送失败") }

func TestFlushFailureLeavesStateBehind(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, "a.md", "一")

	e := engine(local, remote)
	e.Remote = failingFlush{&DirRemote{Root: remote}}
	if _, err := e.Run(context.Background()); err == nil {
		t.Fatal("推送失败应当报错")
	}

	// 状态不能记成「已同步」：推送没成功，下次副本会被重置回远端版本，
	// 本地这个文件就会被当成「远端删了」。
	var st State
	if data, err := os.ReadFile(statePath(local)); err == nil {
		if err := json.Unmarshal(data, &st); err != nil {
			t.Fatal(err)
		}
		if len(st.Files) != 0 {
			t.Errorf("推送失败后状态前进了：%v", st.Files)
		}
	}

	// 换回正常的远端，再同步一次应当收敛
	res, err := engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if got := readFile(t, local, "a.md"); got != "一" {
		t.Errorf("文件被动了：%q", got)
	}
	if res.Deleted != 0 {
		t.Errorf("不该删除任何文件：%s", res.Summary())
	}
}

// pickyRemote 对指定路径的写入报错，其余照常：一个坏文件。
type pickyRemote struct {
	*rawRemote
	bad map[string]error
}

func (p *pickyRemote) Put(ctx context.Context, rel string, data []byte, base string) (Entry, error) {
	if err, ok := p.bad[rel]; ok {
		return Entry{}, err
	}
	return p.rawRemote.Put(ctx, rel, data, base)
}

func readState(t *testing.T, root string) State {
	t.Helper()
	var st State
	data, err := os.ReadFile(statePath(root))
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, &st); err != nil {
		t.Fatal(err)
	}
	return st
}

// 一个文件失败不能拖住整个库：其余照常同步、状态照常留下，下一轮只重试它。
func TestOneBadFileDoesNotStopSync(t *testing.T) {
	local := t.TempDir()
	writeFile(t, local, "a.md", "一")
	writeFile(t, local, "b.md", "二")
	writeFile(t, local, "c.md", "三")

	r := &pickyRemote{
		rawRemote: &rawRemote{files: map[string][]byte{}},
		bad:       map[string]error{"b.md": errors.New("WebDAV 写入 b.md 失败：HTTP 403")},
	}
	res, err := (&Engine{Root: local, Remote: r}).Run(context.Background())
	if err != nil {
		t.Fatalf("一个文件失败不该让整轮变成错误：%v", err)
	}
	if res.Uploaded != 2 {
		t.Fatalf("上传 %d 个，想要 2（%s）", res.Uploaded, res.Summary())
	}
	if len(res.Failed) != 1 || res.Failed[0].Path != "b.md" {
		t.Fatalf("失败记录不对：%+v", res.Failed)
	}
	if !strings.Contains(res.Failed[0].Reason, "403") {
		t.Errorf("原因没留下：%q", res.Failed[0].Reason)
	}
	if st := readState(t, local); len(st.Files) != 2 {
		t.Errorf("状态里有 %d 条，想要 2：%v", len(st.Files), st.Files)
	}

	// 第二轮：只重试 b.md，a/c 不重传
	r.bad = nil
	res, err = (&Engine{Root: local, Remote: r}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || len(res.Failed) != 0 {
		t.Fatalf("第二轮应当只补传一个：%s", res.Summary())
	}
}

// limitedRemote 写到第 allow 个文件之后开始限速（坚果云那种 503）。
type limitedRemote struct {
	*rawRemote
	allow int
	puts  int
}

func (l *limitedRemote) Put(ctx context.Context, rel string, data []byte, base string) (Entry, error) {
	l.puts++
	if l.puts > l.allow {
		return Entry{}, &rateLimited{Status: 503, Host: "dav.jianguoyun.com"}
	}
	return l.rawRemote.Put(ctx, rel, data, base)
}

// 被限速时整轮停下，但已经做完的要留下：下一轮接着传，不从零开始。
func TestRateLimitStopsAndKeepsProgress(t *testing.T) {
	local := t.TempDir()
	for _, name := range []string{"a.md", "b.md", "c.md", "d.md"} {
		writeFile(t, local, name, name)
	}
	r := &limitedRemote{rawRemote: &rawRemote{files: map[string][]byte{}}, allow: 2}

	res, err := (&Engine{Root: local, Remote: r}).Run(context.Background())
	if err != nil {
		t.Fatalf("限速不应当当成硬错误：%v", err)
	}
	if res.Uploaded != 2 {
		t.Fatalf("上传 %d 个，想要 2（%s）", res.Uploaded, res.Summary())
	}
	if res.Stopped == "" {
		t.Fatal("没说为什么停下")
	}
	if !strings.Contains(res.Stopped, "坚果云") {
		t.Errorf("限速原因没说清：%q", res.Stopped)
	}
	if st := readState(t, local); len(st.Files) != 2 {
		t.Errorf("进度没留下：状态里有 %d 条", len(st.Files))
	}

	// 再来一次：限额恢复，只补剩下的两个
	r.allow = 100
	res, err = (&Engine{Root: local, Remote: r}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 2 || res.Stopped != "" {
		t.Fatalf("第二轮应当补完剩下的：%s", res.Summary())
	}
}

// 拉取只往下拿：云端多的、改过的下载下来，本地独有的不动也不上传。
func TestPullDownloadsOnly(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, remote, "a.md", "远端的 a")
	writeFile(t, remote, "b.md", "远端的 b")
	writeFile(t, local, "b.md", "旧的 b")
	writeFile(t, local, "c.md", "只在本地")

	res, err := (&Engine{Root: local, Remote: &DirRemote{Root: remote}}).Pull(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 2 {
		t.Fatalf("下载 %d 个，想要 2（%s）", res.Downloaded, res.Summary())
	}
	if got := readFile(t, local, "b.md"); got != "远端的 b" {
		t.Errorf("本地旧版没被云端覆盖：%q", got)
	}
	// 被覆盖的那一版存进了存档目录
	if _, err := os.Stat(filepath.Join(local, ".papero")); err != nil {
		t.Fatal("没有存档目录")
	}
	matches, _ := filepath.Glob(filepath.Join(local, ".papero", "before-pull-*", "b.md"))
	if len(matches) != 1 {
		t.Fatalf("没找到被覆盖的旧版，实际 %v", matches)
	}
	if got := readFile(t, local, mustRel(t, local, matches[0])); got != "旧的 b" {
		t.Errorf("旧版内容 = %q", got)
	}
	// 不上传：本地独有的 c.md 不该出现在云端
	if _, err := os.Stat(filepath.Join(remote, "c.md")); !os.IsNotExist(err) {
		t.Error("拉取把本地文件传上去了")
	}
	if got := readFile(t, local, "c.md"); got != "只在本地" {
		t.Errorf("本地文件被动了：%q", got)
	}

	// 之后一次完整同步：只上传 c.md，已拉下来的不重传
	res, err = engine(local, remote).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || res.Downloaded != 0 {
		t.Fatalf("同步时应当只传 c.md：%s", res.Summary())
	}
}

func mustRel(t *testing.T, root, abs string) string {
	t.Helper()
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}

// 两边都改过的文件，拉取不碰它：留给一次完整同步去合并（那边会留本地副本）。
func TestPullLeavesBothChangedAlone(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, remote, "a.md", "v1")
	if _, err := engine(local, remote).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	writeFile(t, local, "a.md", "本地改的")
	writeFile(t, remote, "a.md", "远端改的")

	res, err := engine(local, remote).Pull(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 0 || res.Pending != 1 {
		t.Fatalf("应当什么都不拉、记一个待合并：%s", res.Summary())
	}
	if got := readFile(t, local, "a.md"); got != "本地改的" {
		t.Errorf("本地改动被覆盖了：%q", got)
	}
}

// limitedGet 读第 allow 个文件之后开始限速。
type limitedGet struct {
	*DirRemote
	allow int
	gets  int
}

func (l *limitedGet) Get(ctx context.Context, rel string) ([]byte, error) {
	l.gets++
	if l.gets > l.allow {
		return nil, &rateLimited{Status: 503}
	}
	return l.DirRemote.Get(ctx, rel)
}

// 拉取遇到限速：停下、保住已下载的，下一轮接着拉。
func TestPullStopsAtRateLimit(t *testing.T) {
	local, remote := newPair(t)
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		writeFile(t, remote, name, name)
	}
	r := &limitedGet{DirRemote: &DirRemote{Root: remote}, allow: 1}

	res, err := (&Engine{Root: local, Remote: r}).Pull(context.Background())
	if err != nil {
		t.Fatalf("限速不该当成硬错误：%v", err)
	}
	if res.Downloaded != 1 || res.Stopped == "" {
		t.Fatalf("拉取 %d 个，Stopped=%q", res.Downloaded, res.Stopped)
	}

	res, err = (&Engine{Root: local, Remote: &DirRemote{Root: remote}}).Pull(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 2 {
		t.Fatalf("第二轮应当只拉剩下的：%s", res.Summary())
	}
}

// 开了「忽略点开头」：本地的工具目录不传、云端的也不下来。
func TestSyncSkipsDotPaths(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, "a.md", "一")
	writeFile(t, local, ".nestnote/app.json", "{}")
	writeFile(t, remote, ".marking/note.txt", "远端的工具目录")

	e := &Engine{Root: local, Remote: &DirRemote{Root: remote}, Ignore: Filter{Dot: true}}
	res, err := e.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 || res.Downloaded != 0 {
		t.Fatalf("上传 %d、下载 %d（%s）", res.Uploaded, res.Downloaded, res.Summary())
	}
	if _, err := os.Stat(filepath.Join(remote, ".nestnote")); !os.IsNotExist(err) {
		t.Error("点开头的目录被上传了")
	}
	if _, err := os.Stat(filepath.Join(local, ".marking")); !os.IsNotExist(err) {
		t.Error("云端的点开头目录被下载了")
	}
}

// 没开开关时维持原样：点开头的照样同步。
func TestSyncKeepsDotPathsByDefault(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, ".nestnote/app.json", "{}")
	res, err := (&Engine{Root: local, Remote: &DirRemote{Root: remote}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 1 {
		t.Fatalf("默认应当照常同步点开头的文件：%s", res.Summary())
	}
}

// 按 .gitignore 忽略：规则里的不传，.gitignore 自己还是普通文件。
func TestSyncFollowsGitignore(t *testing.T) {
	local, remote := newPair(t)
	writeFile(t, local, ".gitignore", "*.log\n")
	writeFile(t, local, "a.md", "一")
	writeFile(t, local, "run.log", "日志")

	e := &Engine{Root: local, Remote: &DirRemote{Root: remote}, Ignore: NewFilter(local, false, true)}
	res, err := e.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 2 { // .gitignore 与 a.md
		t.Fatalf("上传 %d 个，想要 2（%s）", res.Uploaded, res.Summary())
	}
	if _, err := os.Stat(filepath.Join(remote, "run.log")); !os.IsNotExist(err) {
		t.Error("被 .gitignore 列出的文件上传了")
	}
}

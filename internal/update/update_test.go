package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.2.0", "0.2.1", -1},
		{"0.2.1", "0.2.1", 0},
		{"v0.3.0", "0.2.9", 1},
		{"0.10.0", "0.9.9", 1},        // 数字比，不是字符串比
		{"0.3.0-beta.1", "0.3.0", -1}, // 预发布排在正式版前
		{"0.3.0-beta.2", "0.3.0-beta.10", -1},
		{"0.3.0+build.7", "0.3.0", 0}, // 构建元数据不参与
		{"dev", "0.0.1", -1},          // 不认识的版本当最小
		{"0.2", "0.2.0", -1},
	}
	for _, c := range cases {
		if got := Compare(c.a, c.b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d，想要 %d", c.a, c.b, got, c.want)
		}
	}
	if !Newer("0.2.1", "0.2.0") {
		t.Error("0.2.1 应该比 0.2.0 新")
	}
	if Newer("0.2.0", "0.2.0") {
		t.Error("同版本不算新")
	}
}

// TestCheck 走一遍「问 GitHub 要最新版」的四种答案。
func TestCheck(t *testing.T) {
	var body string
	status := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" {
			t.Error("没带 User-Agent，GitHub 会拒")
		}
		if !strings.HasSuffix(r.URL.Path, "/releases/latest") {
			t.Errorf("请求了 %s", r.URL.Path)
		}
		w.WriteHeader(status)
		io.WriteString(w, body)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	body = `{"tag_name":"v0.2.1","body":"改了什么","html_url":"https://example.com/v0.2.1",
		"assets":[{"name":"papero-windows-amd64.exe","size":123,"browser_download_url":"https://example.com/a.exe"},
		          {"name":"SHA256SUMS.txt","size":64,"browser_download_url":"https://example.com/sums"}]}`

	rel, err := check(context.Background(), http.DefaultClient, Repo, "0.2.0", "windows", "amd64")
	if err != nil {
		t.Fatalf("查更新：%v", err)
	}
	if rel == nil {
		t.Fatal("有新版本却返回了空")
	}
	if rel.Version != "0.2.1" || rel.Tag != "v0.2.1" {
		t.Errorf("版本 = %q / %q", rel.Version, rel.Tag)
	}
	if rel.File.Name != "papero-windows-amd64.exe" || rel.File.Size != 123 {
		t.Errorf("文件 = %+v", rel.File)
	}
	if rel.Sums == "" {
		t.Error("没找到 SHA256SUMS.txt")
	}
	if rel.Notes != "改了什么" {
		t.Errorf("正文 = %q", rel.Notes)
	}

	// 同版本：没有更新
	if rel, err := check(context.Background(), http.DefaultClient, Repo, "0.2.1", "windows", "amd64"); err != nil || rel != nil {
		t.Errorf("同版本应返回空：%+v %v", rel, err)
	}

	// 这个平台没有包：说清楚，别静默当「已最新」
	_, err = check(context.Background(), http.DefaultClient, Repo, "0.2.0", "plan9", "mips")
	if err == nil || !strings.Contains(err.Error(), "papero-plan9-mips") {
		t.Errorf("缺包时的错误 = %v", err)
	}

	// 一个 tag 都没打过
	status, body = http.StatusNotFound, `{"message":"Not Found"}`
	if _, err := check(context.Background(), http.DefaultClient, Repo, "0.2.0", "windows", "amd64"); !errors.Is(err, ErrNoReleases) {
		t.Errorf("404 应返回 ErrNoReleases，实际 %v", err)
	}
}

func TestParseSums(t *testing.T) {
	hexA, hexB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	sums, err := parseSums(strings.NewReader(
		hexA + "  papero-windows-amd64.exe\n" +
			"not a checksum line\n" +
			hexB + " *papero-linux-amd64\n"))
	if err != nil {
		t.Fatalf("解析：%v", err)
	}
	if len(sums) != 2 {
		t.Errorf("解析出 %d 条：%v", len(sums), sums)
	}
	if sums["papero-linux-amd64"] != hexB {
		t.Errorf("二进制模式的 * 没去掉：%v", sums)
	}
	if _, err := parseSums(strings.NewReader("nonsense\n")); err == nil {
		t.Error("空表应当报错")
	}
}

// TestFetch 下载 → 校验。摘要不对、字节数不对都必须拒绝，且不留下半截文件。
func TestFetch(t *testing.T) {
	data := []byte("新版本的文件内容")
	sum := sha256.Sum256(data)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(data)
	}))
	defer srv.Close()

	dir := t.TempDir()
	rel := &Release{
		Version: "0.2.1",
		File:    Asset{Name: "papero-windows-amd64.exe", URL: srv.URL, Size: int64(len(data))},
	}
	sums := map[string]string{rel.File.Name: hex.EncodeToString(sum[:])}

	var lastDone, lastTotal int64
	path, err := Fetch(context.Background(), srv.Client(), rel, dir, sums, func(done, total int64) {
		lastDone, lastTotal = done, total
	})
	if err != nil {
		t.Fatalf("下载：%v", err)
	}
	if filepath.Base(path) != rel.File.Name+".new" {
		t.Errorf("落到 %s", path)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != string(data) {
		t.Errorf("文件内容不对：%q %v", got, err)
	}
	if lastDone != int64(len(data)) || lastTotal != int64(len(data)) {
		t.Errorf("进度 = %d/%d", lastDone, lastTotal)
	}

	// 摘要不对：拒绝并删掉半截文件
	bad := map[string]string{rel.File.Name: strings.Repeat("0", 64)}
	if _, err := Fetch(context.Background(), srv.Client(), rel, dir, bad, nil); err == nil {
		t.Error("摘要不对却通过了")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("失败后留下了文件")
	}

	// 字节数不对（大小写错）：同样拒绝
	short := *rel
	short.File.Size = int64(len(data)) + 10
	if _, err := Fetch(context.Background(), srv.Client(), &short, dir, nil, nil); err == nil {
		t.Error("字节数不对却通过了")
	}
}

// TestInstallSwap 换文件：内容换过去，旧文件让位，Cleanup 收拾干净。
func TestInstallSwap(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "papero.exe")
	fresh := filepath.Join(dir, "papero.exe.new")
	os.WriteFile(exe, []byte("旧"), 0o644)
	os.WriteFile(fresh, []byte("新"), 0o644)

	if err := Install(fresh, exe); err != nil {
		t.Fatalf("换文件：%v", err)
	}
	got, _ := os.ReadFile(exe)
	if string(got) != "新" {
		t.Errorf("换完内容是 %q", got)
	}
	olds, _ := filepath.Glob(filepath.Join(dir, ".papero.exe.old-*"))
	if len(olds) != 1 {
		t.Fatalf("让位出来的旧文件有 %d 个", len(olds))
	}
	Cleanup(exe)
	olds, _ = filepath.Glob(filepath.Join(dir, ".papero.exe.old-*"))
	if len(olds) != 0 {
		t.Errorf("Cleanup 之后还剩 %v", olds)
	}
}

// TestNotes 取「中间所有版本」的日志：范围、排序、跳过草稿与预发布、超量只留最近的。
func TestNotes(t *testing.T) {
	page := `[
	 {"tag_name":"v0.2.3","body":"三","html_url":"u3"},
	 {"tag_name":"v0.2.2","body":"二","html_url":"u2","prerelease":true},
	 {"tag_name":"v0.2.1","body":"一","html_url":"u1"},
	 {"tag_name":"v0.2.0","body":"零","html_url":"u0"},
	 {"tag_name":"v0.1.9","body":"旧","html_url":"u9","draft":true}
	]`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/releases") {
			t.Errorf("请求了 %s", r.URL.Path)
		}
		io.WriteString(w, page)
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	// 0.2.0 是当前版本：只该拿到 0.2.3 与 0.2.1（0.2.2 是预发布）
	notes, total, err := Notes(context.Background(), http.DefaultClient, Repo, "0.2.0", "0.2.3")
	if err != nil {
		t.Fatalf("取日志：%v", err)
	}
	if total != 2 {
		t.Errorf("总数 = %d，想要 2", total)
	}
	if len(notes) != 2 || notes[0].Version != "0.2.3" || notes[1].Version != "0.2.1" {
		t.Fatalf("日志 = %+v", notes)
	}
	if notes[0].Notes != "三" || notes[0].URL != "u3" {
		t.Errorf("正文/地址不对：%+v", notes[0])
	}

	// after 为空：只要 upto 自己的
	notes, _, err = Notes(context.Background(), http.DefaultClient, Repo, "", "0.2.1")
	if err != nil {
		t.Fatalf("取单个版本：%v", err)
	}
	if len(notes) != 1 || notes[0].Version != "0.2.1" {
		t.Errorf("单版本 = %+v", notes)
	}
}

// TestNotesCap 超过 MaxNotes 时只留最近的，但总数照实报。
func TestNotesCap(t *testing.T) {
	var b strings.Builder
	b.WriteString("[")
	for i := 0; i < MaxNotes+5; i++ {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"tag_name":"v0.2.%d","body":"n%d"}`, 100-i, i)
	}
	b.WriteString("]")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, b.String())
	}))
	defer srv.Close()
	old := apiBase
	apiBase = srv.URL
	defer func() { apiBase = old }()

	notes, total, err := Notes(context.Background(), http.DefaultClient, Repo, "0.2.0", "0.2.100")
	if err != nil {
		t.Fatalf("取日志：%v", err)
	}
	if total != MaxNotes+5 {
		t.Errorf("总数 = %d，想要 %d", total, MaxNotes+5)
	}
	if len(notes) != MaxNotes {
		t.Errorf("留下 %d 条，想要 %d", len(notes), MaxNotes)
	}
	if notes[0].Version != "0.2.100" {
		t.Errorf("第一条是 %s", notes[0].Version)
	}
}

package sync

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	stdsync "sync"
	"testing"
)

// fakeDAV 是一个够用的 WebDAV 服务器：列目录、读写、删、建目录，ETag 每次写入
// 自增。同步只用到这几样，多写的部分只会掩盖驱动的问题。
type fakeDAV struct {
	mu     stdsync.Mutex
	prefix string // 例如 "/dav"
	user   string
	pass   string
	files  map[string][]byte
	etags  map[string]string
	dirs   map[string]bool
	seq    int
	puts   int  // 带 If-Match 的写入次数
	stales int  // 因此被拒（412）的次数
	block  bool // 打开后所有请求回 503：坚果云限速的样子
}

func newFakeDAV() *fakeDAV {
	return &fakeDAV{
		prefix: "/dav",
		files:  map[string][]byte{},
		etags:  map[string]string{},
		dirs:   map[string]bool{"": true},
	}
}

func parentOf(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}

func (f *fakeDAV) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if f.user != "" {
		u, p, ok := r.BasicAuth()
		if !ok || u != f.user || p != f.pass {
			w.Header().Set("WWW-Authenticate", `Basic realm="dav"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
	}
	rel := strings.Trim(strings.TrimPrefix(r.URL.Path, f.prefix), "/")
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.block {
		w.Header().Set("Content-Type", "application/xml")
		w.WriteHeader(http.StatusServiceUnavailable)
		io.WriteString(w, `<?xml version="1.0"?><d:error xmlns:d="DAV:" xmlns:s="http://ns.jianguoyun.com"><s:exception>BlockedTemporarily</s:exception><s:message>Too many requests are received recently</s:message></d:error>`)
		return
	}
	switch r.Method {
	case "PROPFIND":
		f.propfind(w, r, rel)
	case http.MethodGet:
		data, ok := f.files[rel]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("ETag", f.etags[rel])
		w.Write(data)
	case http.MethodPut:
		if !f.dirs[parentOf(rel)] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		if im := r.Header.Get("If-Match"); im != "" {
			f.puts++
			if f.etags[rel] != im {
				f.stales++
				w.WriteHeader(http.StatusPreconditionFailed)
				return
			}
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.seq++
		f.files[rel] = body
		f.etags[rel] = fmt.Sprintf(`"v%d"`, f.seq)
		w.Header().Set("ETag", f.etags[rel])
		w.WriteHeader(http.StatusCreated)
	case "MKCOL":
		if f.dirs[rel] {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !f.dirs[parentOf(rel)] {
			w.WriteHeader(http.StatusConflict)
			return
		}
		f.dirs[rel] = true
		w.WriteHeader(http.StatusCreated)
	case http.MethodDelete:
		if _, ok := f.files[rel]; !ok {
			http.NotFound(w, r)
			return
		}
		delete(f.files, rel)
		delete(f.etags, rel)
		w.WriteHeader(http.StatusNoContent)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (f *fakeDAV) propfind(w http.ResponseWriter, r *http.Request, rel string) {
	if !f.dirs[rel] {
		http.NotFound(w, r)
		return
	}
	type ent struct {
		rel  string
		dir  bool
		etag string
	}
	ents := []ent{{rel: rel, dir: true}}
	if r.Header.Get("Depth") != "0" {
		for d := range f.dirs {
			if d != "" && parentOf(d) == rel {
				ents = append(ents, ent{rel: d, dir: true})
			}
		}
		for p := range f.files {
			if parentOf(p) == rel {
				ents = append(ents, ent{rel: p, etag: f.etags[p]})
			}
		}
	}
	sort.Slice(ents, func(i, j int) bool { return ents[i].rel < ents[j].rel })

	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?><D:multistatus xmlns:D="DAV:">`)
	for _, e := range ents {
		href := (&url.URL{Path: f.prefix + "/" + e.rel}).EscapedPath()
		if e.dir && !strings.HasSuffix(href, "/") {
			href += "/"
		}
		b.WriteString("<D:response><D:href>")
		xml.EscapeText(&b, []byte(href))
		b.WriteString("</D:href><D:propstat><D:prop><D:resourcetype>")
		if e.dir {
			b.WriteString("<D:collection/>")
		}
		b.WriteString("</D:resourcetype>")
		if !e.dir {
			b.WriteString("<D:getetag>")
			xml.EscapeText(&b, []byte(e.etag))
			b.WriteString("</D:getetag>")
		}
		b.WriteString("</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>")
	}
	b.WriteString("</D:multistatus>")
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.WriteHeader(http.StatusMultiStatus)
	io.WriteString(w, b.String())
}

// newDAV 起一个假服务器，返回它的地址（已经指向 /dav/Papero）。
func newDAV(t *testing.T) (*fakeDAV, string) {
	t.Helper()
	f := newFakeDAV()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv.URL + "/dav/Papero"
}

// 整个引擎跑在 WebDAV 上：建目录、上传、再同步无事可做、另一台设备拉下来。
func TestWebDAVEngineRoundTrip(t *testing.T) {
	f, base := newDAV(t)
	local := t.TempDir()
	writeFile(t, local, "a.md", "一")
	writeFile(t, local, "笔记/我的 笔记.md", "空格与中文都要能过")

	res, err := (&Engine{Root: local, Remote: &WebDAVRemote{Base: base}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded != 2 {
		t.Fatalf("上传 %d 个，想要 2（%s）", res.Uploaded, res.Summary())
	}
	if got := string(f.files["Papero/笔记/我的 笔记.md"]); got != "空格与中文都要能过" {
		t.Errorf("远端内容 = %q", got)
	}

	// 第二次：状态里的 ETag 与列目录给的一致，应当无事可做
	res, err = (&Engine{Root: local, Remote: &WebDAVRemote{Base: base}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Uploaded+res.Downloaded+res.Deleted != 0 {
		t.Errorf("第二次同步仍有动作：%s", res.Summary())
	}

	// 另一台设备
	other := t.TempDir()
	res, err = (&Engine{Root: other, Remote: &WebDAVRemote{Base: base}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Downloaded != 2 {
		t.Fatalf("下载 %d 个，想要 2（%s）", res.Downloaded, res.Summary())
	}
	if got := readFile(t, other, "笔记/我的 笔记.md"); got != "空格与中文都要能过" {
		t.Errorf("下载内容 = %q", got)
	}
}

// 远端有、本地没有的目录不预建；删除要跟着走。
func TestWebDAVDeletePropagates(t *testing.T) {
	f, base := newDAV(t)
	local := t.TempDir()
	writeFile(t, local, "dir/a.md", "一")
	if _, err := (&Engine{Root: local, Remote: &WebDAVRemote{Base: base}}).Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(local, "dir", "a.md")); err != nil {
		t.Fatal(err)
	}
	res, err := (&Engine{Root: local, Remote: &WebDAVRemote{Base: base}}).Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 {
		t.Fatalf("删除 %d 个，想要 1（%s）", res.Deleted, res.Summary())
	}
	if _, ok := f.files["Papero/dir/a.md"]; ok {
		t.Error("远端文件还在")
	}
}

// href 有三种写法：绝对 URL、绝对路径、相对路径。三种都要认，跑出同步根目录
// 的要丢掉，路径里的中文要解回来。
func TestWebDAVResolveHref(t *testing.T) {
	d := &WebDAVRemote{Base: "https://dav.example.com/dav/Papero"}
	cases := []struct {
		href string
		rel  string
		ok   bool
	}{
		{"/dav/Papero/a.md", "a.md", true},
		{"https://dav.example.com/dav/Papero/b.md", "b.md", true},
		{"c.md", "c.md", true},
		{"/dav/Papero/%E7%AC%94%E8%AE%B0/%E4%B8%80.md", "笔记/一.md", true},
		{"/dav/Papero/", "", true},
		{"/dav/Other/x.md", "", false},
		{"/elsewhere/x.md", "", false},
	}
	for _, c := range cases {
		u, rel, ok := d.resolve(c.href)
		if ok != c.ok {
			t.Errorf("%s：ok = %v，想要 %v", c.href, ok, c.ok)
			continue
		}
		if !ok {
			continue
		}
		if rel != c.rel {
			t.Errorf("%s：rel = %q，想要 %q", c.href, rel, c.rel)
		}
		if !strings.HasPrefix(u.String(), "https://dav.example.com/dav/Papero") {
			t.Errorf("%s：地址拼错了：%s", c.href, u.String())
		}
	}
}

// 「测试连接」：目录还没建不算错，能数出文件数，地址写错要说清。
func TestWebDAVProbe(t *testing.T) {
	_, base := newDAV(t)
	ctx := context.Background()

	res, err := (&WebDAVRemote{Base: base}).Probe(ctx)
	if err != nil {
		t.Fatalf("目录还没建时不该报错：%v", err)
	}
	if !res.New {
		t.Errorf("应当是「还没建」：%+v", res)
	}

	r := &WebDAVRemote{Base: base}
	if _, err := r.Put(ctx, "a.md", []byte("一"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Put(ctx, "d/b.md", []byte("二"), ""); err != nil {
		t.Fatal(err)
	}
	res, err = r.Probe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if res.New || res.Files != 2 {
		t.Errorf("探到 %+v，想要 2 个文件", res)
	}

	// 上一级也不在：地址大概写错了
	bad := &WebDAVRemote{Base: strings.TrimSuffix(base, "/Papero") + "/nope/Papero"}
	if _, err := bad.Probe(ctx); err == nil {
		t.Error("地址不对时应当报错")
	}
}

// 503 要变成「限速」这个专门的错误：engine 见到它整轮停下、保留进度，
// 而不是当成一个文件坏了去逐个重试。
func TestWebDAVRateLimitIsRecognised(t *testing.T) {
	f, base := newDAV(t)
	r := &WebDAVRemote{Base: base}
	f.mu.Lock()
	f.block = true
	f.mu.Unlock()

	_, err := r.List(context.Background())
	var lim *rateLimited
	if !errors.As(err, &lim) {
		t.Fatalf("503 应当被认成限速，实际 %v", err)
	}
	if lim.Status != http.StatusServiceUnavailable {
		t.Errorf("状态码 = %d", lim.Status)
	}
	if !strings.Contains(err.Error(), "坚果云") {
		t.Errorf("没说是坚果云的限制：%q", err.Error())
	}
}

// 条件写：拿着过期的 ETag 写会被拒，而不是盖掉别人的改动。
func TestWebDAVConditionalPut(t *testing.T) {
	f, base := newDAV(t)
	ctx := context.Background()
	first := &WebDAVRemote{Base: base}
	second := &WebDAVRemote{Base: base}

	if _, err := first.Put(ctx, "a.md", []byte("一"), ""); err != nil {
		t.Fatal(err)
	}
	list, err := first.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	seen := list["a.md"].Version
	if seen == "" {
		t.Fatal("列目录没给出版本")
	}

	// 第一台按看到的版本写：成功
	if _, err := first.Put(ctx, "a.md", []byte("二"), seen); err != nil {
		t.Fatalf("按版本写失败：%v", err)
	}
	// 第二台还拿着旧版本：应当被拒
	_, err = second.Put(ctx, "a.md", []byte("三"), seen)
	if !errors.Is(err, ErrVersionChanged) {
		t.Fatalf("过期版本写入应当被拒，实际 %v", err)
	}
	if got := string(f.files["Papero/a.md"]); got != "二" {
		t.Errorf("文件被盖掉了：%q", got)
	}
	if f.stales != 1 {
		t.Errorf("服务器收到 %d 次过期请求，想要 1", f.stales)
	}
}

// 目录还不存在时列目录应当是空的，不是错误——第一次同步靠这个把目录建出来。
func TestWebDAVMissingRootIsEmpty(t *testing.T) {
	_, base := newDAV(t)
	entry, err := (&WebDAVRemote{Base: base}).List(context.Background())
	if err != nil {
		t.Fatalf("根目录不在时不该报错：%v", err)
	}
	if len(entry) != 0 {
		t.Errorf("列目录得到 %d 个文件", len(entry))
	}
}

// 地址的上一级不在时，说清楚要去建，而不是替用户在账号里造一串目录。
func TestWebDAVParentMissing(t *testing.T) {
	_, base := newDAV(t)
	// Papero 的上一级 dav 在假服务器里是存在的；用它下面再深一层来试
	missing := strings.TrimSuffix(base, "/Papero") + "/nope/Papero"
	err := (&WebDAVRemote{Base: missing}).mkdirs(context.Background(), "")
	if err == nil {
		t.Fatal("上一级不在时应当报错")
	}
	if !strings.Contains(err.Error(), "上一级不在") {
		t.Errorf("错误没说清原因：%v", err)
	}
}

func TestWebDAVUnauthorized(t *testing.T) {
	f, base := newDAV(t)
	f.user, f.pass = "me@example.com", "app-password"

	if _, err := (&WebDAVRemote{Base: base}).List(context.Background()); err == nil {
		t.Fatal("没给密码应当报错")
	} else if !strings.Contains(err.Error(), "401") {
		t.Errorf("错误没说 401：%v", err)
	}

	ok := &WebDAVRemote{Base: base, User: f.user, Pass: f.pass}
	if _, err := ok.Put(context.Background(), "a.md", []byte("x"), ""); err != nil {
		t.Errorf("给了密码还失败：%v", err)
	}
}

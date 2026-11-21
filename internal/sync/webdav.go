package sync

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	stdsync "sync"
	"time"
)

// WebDAVRemote 把一个 WebDAV 目录当成远端：坚果云、群晖、Nextcloud、ownCloud
// 都算。同步单位还是文件，所以只需要五个动作：PROPFIND 列目录、GET、PUT、
// DELETE、MKCOL 建目录。
//
// 版本用服务器给的 ETag。它随列目录一起回来，写的时候作为 If-Match 发回去：
// 两台设备同时改了同一个文件，后写的那次会被 412 拒掉，而不是悄悄盖上去。
// 有的服务器不给 ETag，那就退回「不知道版本」（Version 为空），仍能同步，
// 只是没有这层保护。
type WebDAVRemote struct {
	// Base 是同步根目录的地址，可以在账号里还不存在——第一次写入时逐级建。
	Base string
	User string
	Pass string

	mu   stdsync.Mutex
	made map[string]bool // 已经建过的目录，免得每个文件都去 MKCOL 一次
	root bool            // 同步根目录已经确认存在
}

// Kind 返回 webdav。
func (d *WebDAVRemote) Kind() string { return "webdav" }

// Descriptor 是地址加账号：换服务器、换目录、换账号都算换远端。
func (d *WebDAVRemote) Descriptor() string {
	b, err := d.baseURL()
	if err != nil {
		return "webdav:" + strings.TrimSpace(d.Base) + "#" + d.User
	}
	return "webdav:" + b.String() + "#" + d.User
}

// baseURL 是同步根目录的地址：补上 https://、去掉结尾的斜杠与查询串。
func (d *WebDAVRemote) baseURL() (*url.URL, error) {
	raw := strings.TrimSpace(d.Base)
	if raw == "" {
		return nil, errors.New("没有填 WebDAV 地址")
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, fmt.Errorf("%q 不是 WebDAV 地址（https://…）", d.Base)
	}
	u.Path = strings.TrimRight(u.Path, "/")
	u.RawQuery, u.Fragment, u.User = "", "", nil
	return u, nil
}

// urlFor 是根目录下某个相对路径的地址；rel 为空时是根目录本身（带结尾斜杠，
// 有的服务器按斜杠判断是不是目录）。
func (d *WebDAVRemote) urlFor(rel string) (*url.URL, error) {
	base, err := d.baseURL()
	if err != nil {
		return nil, err
	}
	u := *base
	if rel == "" {
		u.Path = base.Path + "/"
	} else {
		u.Path = base.Path + "/" + strings.Trim(rel, "/")
	}
	u.RawPath = "" // 路径换过了：留着旧的百分号编码会让 String() 拼出别的地址
	return &u, nil
}

var errNoCollection = errors.New("WebDAV 上没有这个目录")

// List 递归列出根目录下的全部文件。根目录还不存在时是空的——第一次同步会把它
// 建出来，而不是报错。
func (d *WebDAVRemote) List(ctx context.Context) (map[string]Entry, error) {
	out := map[string]Entry{}
	root, err := d.urlFor("")
	if err != nil {
		return nil, err
	}
	if err := d.walk(ctx, root, "", out, 0); err != nil {
		if errors.Is(err, errNoCollection) {
			return out, nil
		}
		return nil, err
	}
	return out, nil
}

// walk 列一层，遇到子目录再往下走。
//
// 不用 Depth: infinity：很多服务器（含坚果云）直接禁用，而且一次拿整棵树在
// 大库上既慢又容易超时。逐层 Depth: 1 也让出错时知道是哪一层。
//
// prefix 是当前这一层自己的相对路径：PROPFIND 的回答里第一条就是它自己，
// 不认出来就会「目录里有自己」，一路递归下去。
func (d *WebDAVRemote) walk(ctx context.Context, dir *url.URL, prefix string, out map[string]Entry, depth int) error {
	if depth > 32 {
		return errors.New("WebDAV 目录嵌套太深：先看看服务器上的目录结构对不对")
	}
	items, err := d.propfind(ctx, dir)
	if err != nil {
		return err
	}
	return d.collect(ctx, items, prefix, out, depth)
}

// collect 把一层的结果收进来，遇到子目录再往下走。
func (d *WebDAVRemote) collect(ctx context.Context, items []davItem, prefix string, out map[string]Entry, depth int) error {
	for _, it := range items {
		if it.rel == prefix {
			continue // 这一层自己
		}
		if it.collection {
			u := it.url
			if err := d.walk(ctx, &u, it.rel, out, depth+1); err != nil {
				return err
			}
			continue
		}
		out[it.rel] = Entry{Version: it.etag}
	}
	return nil
}

// davBody 只要这几样：是不是目录、版本。内容大小用不上——判断变化靠摘要。
var davBody = `<?xml version="1.0" encoding="utf-8"?><propfind xmlns="DAV:"><prop><resourcetype/><getetag/></prop></propfind>`

func (d *WebDAVRemote) propfind(ctx context.Context, dir *url.URL) ([]davItem, error) {
	req, err := http.NewRequestWithContext(ctx, "PROPFIND", dir.String(), strings.NewReader(davBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Depth", "1")
	req.Header.Set("Content-Type", "application/xml; charset=utf-8")
	res, err := d.do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusMultiStatus:
	case http.StatusNotFound, http.StatusConflict:
		return nil, errNoCollection
	default:
		return nil, fmt.Errorf("WebDAV 列目录 %s 失败：HTTP %d", dir.Path, res.StatusCode)
	}
	var ms davMultistatus
	if err := xml.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&ms); err != nil {
		return nil, fmt.Errorf("WebDAV 返回的目录列表读不了：%w", err)
	}
	items := make([]davItem, 0, len(ms.Responses))
	for _, r := range ms.Responses {
		if it, ok := d.itemOf(r); ok {
			items = append(items, it)
		}
	}
	return items, nil
}

// itemOf 把一条 response 读成一个文件或目录，丢掉不在同步根目录下的。
func (d *WebDAVRemote) itemOf(r davResponse) (davItem, bool) {
	var etag string
	var collection bool
	for _, ps := range r.Propstat {
		// 只认 200 的那一段：不支持的属性会被放在 404 段里
		if !strings.Contains(ps.Status, "200") {
			continue
		}
		if ps.Prop.ResourceType.Collection != nil {
			collection = true
		}
		if ps.Prop.ETag != "" {
			etag = ps.Prop.ETag
		}
	}
	// 有的服务器不给 resourcetype，靠 href 结尾的斜杠判断目录
	if strings.HasSuffix(strings.TrimSpace(r.Href), "/") {
		collection = true
	}
	u, rel, ok := d.resolve(r.Href)
	if !ok {
		return davItem{}, false
	}
	return davItem{url: u, rel: rel, collection: collection, etag: etag}, true
}

// resolve 把 href 变成本地路径。服务器给的是绝对 URL、绝对路径或罕见地相对
// 路径，三种都要认；路径里可能有中文，parse 会解开百分号编码。
func (d *WebDAVRemote) resolve(href string) (url.URL, string, bool) {
	base, err := d.baseURL()
	if err != nil {
		return url.URL{}, "", false
	}
	href = strings.TrimSpace(href)
	if href == "" {
		return url.URL{}, "", false
	}
	ref, err := url.Parse(href)
	if err != nil {
		return url.URL{}, "", false
	}
	var u url.URL
	switch {
	case ref.Host != "":
		u = *base.ResolveReference(ref)
	case strings.HasPrefix(ref.Path, "/"):
		u = *base.ResolveReference(ref)
	default:
		u = *base
		u.Path = base.Path + "/" + ref.Path
	}
	if !strings.HasPrefix(u.Path, base.Path) {
		return url.URL{}, "", false // 跑出同步根目录了
	}
	rel := strings.Trim(strings.TrimPrefix(u.Path, base.Path), "/")
	u.Path = base.Path + "/" + rel
	u.RawPath = ""
	u.RawQuery, u.Fragment = "", ""
	return u, rel, true
}

// Get 读一个文件。要求不压缩：服务器压缩后返回的字节不是原文，而 ETag 也会跟着变。
func (d *WebDAVRemote) Get(ctx context.Context, rel string) ([]byte, error) {
	u, err := d.urlFor(rel)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	res, err := d.do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, fmt.Errorf("WebDAV 上没有 %s", rel)
	default:
		return nil, fmt.Errorf("WebDAV 读取 %s 失败：HTTP %d", rel, res.StatusCode)
	}
	// 说了 identity 还是压了（有的服务器无视这个头）：自己解，别把压缩后的
	// 字节当成文件内容写进知识库。
	if strings.EqualFold(res.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(res.Body)
		if err != nil {
			return nil, fmt.Errorf("WebDAV 返回的 %s 解不开：%w", rel, err)
		}
		defer zr.Close()
		return io.ReadAll(zr)
	}
	return io.ReadAll(res.Body)
}

// Put 写一个文件，必要时逐级把目录建出来。base 非空时按条件写。
func (d *WebDAVRemote) Put(ctx context.Context, rel string, data []byte, base string) (Entry, error) {
	if err := d.mkdirs(ctx, path.Dir(rel)); err != nil {
		return Entry{}, err
	}
	u, err := d.urlFor(rel)
	if err != nil {
		return Entry{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), bytes.NewReader(data))
	if err != nil {
		return Entry{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if base != "" {
		// ETag 原样发回：引号、W/ 前缀都是它的一部分
		req.Header.Set("If-Match", base)
	}
	res, err := d.do(req)
	if err != nil {
		return Entry{}, err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return Entry{Version: res.Header.Get("ETag")}, nil
	case res.StatusCode == http.StatusPreconditionFailed:
		return Entry{}, ErrVersionChanged
	case res.StatusCode == http.StatusConflict:
		return Entry{}, fmt.Errorf("WebDAV 上没有 %s 的上级目录，也建不出来", rel)
	default:
		return Entry{}, fmt.Errorf("WebDAV 写入 %s 失败：HTTP %d", rel, res.StatusCode)
	}
}

// Delete 删一个文件。已经不在了算成功。
func (d *WebDAVRemote) Delete(ctx context.Context, rel string) error {
	u, err := d.urlFor(rel)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	res, err := d.do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300, res.StatusCode == http.StatusNotFound:
		return nil
	default:
		return fmt.Errorf("WebDAV 删除 %s 失败：HTTP %d", rel, res.StatusCode)
	}
}

// Probe 看同步目录在不在：在就数一遍文件；不在就看它的上一级在不在——「目录还
// 没建」和「地址填错了」是两件事，说清楚才对用户有用。
func (d *WebDAVRemote) Probe(ctx context.Context) (ProbeResult, error) {
	root, err := d.urlFor("")
	if err != nil {
		return ProbeResult{}, err
	}
	items, err := d.propfind(ctx, root)
	if errors.Is(err, errNoCollection) {
		return d.probeParent(ctx)
	}
	if err != nil {
		return ProbeResult{}, err
	}
	out := map[string]Entry{}
	if err := d.collect(ctx, items, "", out, 0); err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{Files: len(out)}, nil
}

// probeParent 分辨「同步目录还没建」和「地址写错了」。
func (d *WebDAVRemote) probeParent(ctx context.Context) (ProbeResult, error) {
	base, err := d.baseURL()
	if err != nil {
		return ProbeResult{}, err
	}
	if base.Path == "" || base.Path == "/" {
		return ProbeResult{}, fmt.Errorf("%s 上没有这个 WebDAV 目录", base.Host)
	}
	parent := *base
	parent.Path = path.Dir(base.Path)
	parent.RawPath = ""
	if _, err := d.propfind(ctx, &parent); err != nil {
		if errors.Is(err, errNoCollection) {
			return ProbeResult{}, fmt.Errorf("%s 不存在，它的上一级 %s 也不在：检查一下地址", base.Path, parent.Path)
		}
		return ProbeResult{}, err
	}
	return ProbeResult{New: true}, nil
}

// mkdirs 先保证同步根目录在，再逐级建 rel 的上级目录。rel 是相对根目录的路径，
// "." 表示根目录本身。
//
// 建过的目录记在 made 里：一个目录下几百个文件时，否则就是几百次 MKCOL。
func (d *WebDAVRemote) mkdirs(ctx context.Context, rel string) error {
	if err := d.ensureRoot(ctx); err != nil {
		return err
	}
	rel = strings.Trim(rel, "/")
	if rel == "" || rel == "." {
		return nil
	}
	d.mu.Lock()
	if d.made == nil {
		d.made = map[string]bool{}
	}
	done := d.made[rel]
	d.mu.Unlock()
	if done {
		return nil
	}
	cur := ""
	for _, seg := range strings.Split(rel, "/") {
		if cur == "" {
			cur = seg
		} else {
			cur += "/" + seg
		}
		d.mu.Lock()
		had := d.made[cur]
		d.mu.Unlock()
		if had {
			continue
		}
		u, err := d.urlFor(cur)
		if err != nil {
			return err
		}
		if err := d.mkcol(ctx, u); err != nil {
			return err
		}
		d.mu.Lock()
		d.made[cur] = true
		d.mu.Unlock()
	}
	return nil
}

// ensureRoot 建出地址里的同步根目录。
//
// 只建最后一段：它的上级（例如 /dav）是用户自己账号里的目录，跑出去替用户建
// 一串目录不合适。上一级不在时说清楚，让用户先在网盘里建好。
func (d *WebDAVRemote) ensureRoot(ctx context.Context) error {
	d.mu.Lock()
	done := d.root
	d.mu.Unlock()
	if done {
		return nil
	}
	base, err := d.baseURL()
	if err != nil {
		return err
	}
	if err := d.mkcol(ctx, base); err != nil {
		return err
	}
	d.mu.Lock()
	d.root = true
	d.mu.Unlock()
	return nil
}

func (d *WebDAVRemote) mkcol(ctx context.Context, u *url.URL) error {
	req, err := http.NewRequestWithContext(ctx, "MKCOL", u.String(), nil)
	if err != nil {
		return err
	}
	res, err := d.do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		return nil
	case res.StatusCode == http.StatusMethodNotAllowed:
		return nil // 已经有了
	case res.StatusCode == http.StatusConflict:
		return fmt.Errorf("WebDAV 建不出目录 %s：它的上一级不在，先在网盘里建好再同步", u.Path)
	default:
		return fmt.Errorf("WebDAV 建目录 %s 失败：HTTP %d", u.Path, res.StatusCode)
	}
}

// rateLimited 是服务器说「你发得太多了」：HTTP 429，或 503——坚果云用 503 +
// BlockedTemporarily 表示「每 30 分钟 600 次请求」用完了。
//
// engine 见到它就整轮停下：再发也是同样的回答，只会把剩下的耐心也耗掉。已经
// 做完的照常落状态，下次接着传。
type rateLimited struct {
	Status int
	Host   string
	// Blocked 是答复里说了 BlockedTemporarily / too many requests：坚果云的限速
	// 就是这么答的，比看域名准
	Blocked bool
	After   time.Duration // Retry-After 说了就等这么久
}

func (e *rateLimited) Error() string {
	if e.Blocked || strings.Contains(e.Host, "jianguoyun.com") {
		return "坚果云对请求频率有限制（每 30 分钟 600 次），刚才发得太多了：已经同步的都会留着，过二三十分钟再点一次同步会接着传"
	}
	if e.After > 0 {
		return fmt.Sprintf("服务器限速（HTTP %d）：%s 后再同步，已经同步的会留着", e.Status, e.After.Round(time.Second))
	}
	return fmt.Sprintf("服务器限速（HTTP %d：最近请求太多）：过一会儿再同步，已经同步的会留着", e.Status)
}

// retryAfter 读 Retry-After：秒数，或一个时间点。
func retryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

// do 发一条请求。认证只支持 Basic：Digest 要做一次挑战往返，WebDAV 用户多数
// 用的是「应用密码」，Basic 够用。
func (d *WebDAVRemote) do(req *http.Request) (*http.Response, error) {
	if d.User != "" || d.Pass != "" {
		req.SetBasicAuth(d.User, d.Pass)
	}
	res, err := davClient.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusUnauthorized {
		res.Body.Close()
		return nil, errors.New("WebDAV 服务器拒绝了用户名或密码（HTTP 401）：网盘通常要「应用密码」，不是登录密码")
	}
	if res.StatusCode == http.StatusTooManyRequests || res.StatusCode == http.StatusServiceUnavailable {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		res.Body.Close()
		text := strings.ToLower(string(body))
		blocked := strings.Contains(string(body), "BlockedTemporarily") || strings.Contains(text, "too many requests")
		return nil, &rateLimited{
			Status:  res.StatusCode,
			Host:    req.URL.Host,
			Blocked: blocked,
			After:   retryAfter(res.Header.Get("Retry-After")),
		}
	}
	return res, nil
}

// davClient 不设总超时：一次同步传几百 MB 的附件可能好几分钟，时间由上层
// 的 context 管。连接层的超时由 Transport 自己的默认值兜着。
var davClient = &http.Client{}

// davItem 是列目录得到的一条。
type davItem struct {
	url        url.URL
	rel        string
	collection bool
	etag       string
}

type davMultistatus struct {
	Responses []davResponse `xml:"response"`
}

type davResponse struct {
	Href     string        `xml:"href"`
	Propstat []davPropstat `xml:"propstat"`
}

type davPropstat struct {
	Prop   davProp `xml:"prop"`
	Status string  `xml:"status"`
}

type davCollection struct{}

type davProp struct {
	ResourceType struct {
		Collection *davCollection `xml:"collection"`
	} `xml:"resourcetype"`
	ETag string `xml:"getetag"`
}

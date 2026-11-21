package update

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// FetchSums 取 SHA256SUMS.txt，返回「文件名 → 摘要」。
//
// 文件不存在（老版本发布还没有这项）返回空表而不是错误：那种情况下只能校验大小，
// 界面上会说明。
func FetchSums(ctx context.Context, c *http.Client, url string) (map[string]string, error) {
	if url == "" {
		return nil, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "papero-updater")
	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("校验和：%s", res.Status)
	}
	return parseSums(io.LimitReader(res.Body, 1<<20))
}

// parseSums 读 sha256sum 的输出格式："<hex>  <name>"，二进制模式的 "*<name>" 也认。
func parseSums(r io.Reader) (map[string]string, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	out := make(map[string]string)
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(strings.TrimSpace(line))
		if len(f) != 2 || len(f[0]) != 64 {
			continue
		}
		out[strings.TrimPrefix(f[1], "*")] = strings.ToLower(f[0])
	}
	if len(out) == 0 {
		return nil, errors.New("校验和文件是空的")
	}
	return out, nil
}

// Fetch 把 rel 里这个平台的文件下到 dir，落成「文件名.new」，校验大小与摘要后返回路径。
//
// 落在 exe 旁边而不是系统临时目录：换文件是一次同卷 rename，跨盘就变成拷贝，
// 而 Windows 上拷贝不了正在运行的那个 exe。
func Fetch(ctx context.Context, c *http.Client, rel *Release, dir string, sums map[string]string, progress func(done, total int64)) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rel.File.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "papero-updater")
	res, err := c.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载：%s", res.Status)
	}

	path := filepath.Join(dir, rel.File.Name+".new")
	f, err := os.Create(path)
	if err != nil {
		return "", err
	}
	sum := sha256.New()
	var n int64
	var last time.Time
	n, err = io.Copy(io.MultiWriter(f, sum), &reader{
		r:     io.LimitReader(res.Body, rel.File.Size+1), // 多读一个字节，超了就知道不对
		total: rel.File.Size, progress: progress, last: &last,
	})
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(path)
		return "", err
	}
	if rel.File.Size > 0 && n != rel.File.Size {
		os.Remove(path)
		return "", fmt.Errorf("下载了 %d 字节，应该是 %d", n, rel.File.Size)
	}
	if want := sums[rel.File.Name]; want != "" {
		if got := hex.EncodeToString(sum.Sum(nil)); got != want {
			os.Remove(path)
			return "", errors.New("下载的文件和 SHA256SUMS.txt 对不上")
		}
	}
	return path, nil
}

// reader 顺手报进度：每 100 毫秒一次，别让每个 32KB 块都触发一次重绘。
type reader struct {
	r        io.Reader
	n, total int64
	progress func(done, total int64)
	last     *time.Time
}

func (p *reader) Read(b []byte) (int, error) {
	n, err := p.r.Read(b)
	p.n += int64(n)
	if p.progress != nil && (err != nil || p.n == p.total || time.Since(*p.last) > 100*time.Millisecond) {
		*p.last = time.Now()
		p.progress(p.n, p.total)
	}
	return n, err
}

// Install 把下好的新文件换到 exe 的位置。
//
// Windows 上正在运行的 exe 不能覆盖，但可以改名，所以顺序是：旧 exe 改名让位 →
// 新的就位。新文件进不去就把旧的改回来，绝不留一个「两边都不是」的状态。
// 让位出来的旧文件由 Cleanup 在下次启动时删。
func Install(fresh, exe string) error {
	old := filepath.Join(filepath.Dir(exe), "."+filepath.Base(exe)+".old-"+strconv.Itoa(os.Getpid()))
	os.Remove(old)
	if runtime.GOOS != "windows" {
		// 下载下来的文件带着 0600，换上去之前得先给它执行位
		if err := os.Chmod(fresh, 0o755); err != nil {
			return err
		}
	}
	if err := os.Rename(exe, old); err != nil {
		return err
	}
	if err := os.Rename(fresh, exe); err != nil {
		os.Rename(old, exe) // 放回去
		return err
	}
	return nil
}

// Cleanup 删掉上次更新让位出来的旧 exe。
//
// 只能在启动时调：更新那一刻删不掉（进程还占着它）。
func Cleanup(exe string) {
	dir, base := filepath.Dir(exe), filepath.Base(exe)
	olds, _ := filepath.Glob(filepath.Join(dir, "."+base+".old-*"))
	for _, p := range olds {
		os.Remove(p)
	}
}

// Executable 返回正在运行的 exe 的绝对路径，软链解析成真实文件。
func Executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return filepath.Abs(exe)
}

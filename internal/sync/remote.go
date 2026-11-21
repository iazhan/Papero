package sync

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// DirRemote 是一个文件夹：iCloud、OneDrive、Syncthing 之类把文件夹搬到别处，
// Papero 只读写这个文件夹，不需要账号。
type DirRemote struct{ Root string }

// Kind 返回 dir。
func (d *DirRemote) Kind() string { return "dir" }

// Descriptor 返回这个文件夹的绝对路径：换个文件夹就是换了个远端，旧状态作废。
// 用绝对路径而不是原字符串，避免 `D:\Notes` 与 `D:/Notes` 被当成两个远端。
func (d *DirRemote) Descriptor() string {
	abs, err := filepath.Abs(d.Root)
	if err != nil {
		abs = d.Root
	}
	return "dir:" + filepath.Clean(abs)
}

// List 返回文件夹里的全部文件。文件夹没有单独的版本概念，内容是即时可读的，
// 所以版本就是内容摘要——与改动前存下的状态天然对得上。
func (d *DirRemote) List(ctx context.Context) (map[string]Entry, error) {
	if _, err := os.Stat(d.Root); err != nil {
		if err := os.MkdirAll(d.Root, 0o755); err != nil {
			return nil, err
		}
	}
	hashes, err := scanLocal(d.Root, Filter{})
	if err != nil {
		return nil, err
	}
	out := make(map[string]Entry, len(hashes))
	for rel, h := range hashes {
		out[rel] = Entry{Version: h, Hash: h}
	}
	return out, nil
}

// Get 读取一个文件。
func (d *DirRemote) Get(ctx context.Context, rel string) ([]byte, error) {
	clean, err := safeRel(rel)
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(d.Root, filepath.FromSlash(clean)))
}

// Put 写入一个文件。
//
// base 不用：一个文件夹没有原子的条件写，两个进程同时写同一个文件本来就以最后
// 写完的为准。远端是本地或网盘文件夹时，真正在并发的是网盘客户端，同步看到的是
// 它同步完之后的结果。
func (d *DirRemote) Put(ctx context.Context, rel string, data []byte, base string) (Entry, error) {
	if err := writeLocal(d.Root, rel, data); err != nil {
		return Entry{}, err
	}
	h := hash(data)
	return Entry{Version: h, Hash: h}, nil
}

// Delete 删除一个文件。
func (d *DirRemote) Delete(ctx context.Context, rel string) error {
	clean, err := safeRel(rel)
	if err != nil {
		return err
	}
	err = os.Remove(filepath.Join(d.Root, filepath.FromSlash(clean)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

// Probe 看这个文件夹在不在、读不读得了，顺便数一下文件。不建它：测试连接
// 不该有副作用。
func (d *DirRemote) Probe(ctx context.Context) (ProbeResult, error) {
	if _, err := os.Stat(d.Root); err != nil {
		if os.IsNotExist(err) {
			return ProbeResult{New: true}, nil
		}
		return ProbeResult{}, err
	}
	hashes, err := scanLocal(d.Root, Filter{})
	if err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{Files: len(hashes)}, nil
}

// safeRel 把同步里的相对路径规范化，并拒绝越界的写法（`..`）。
// 远端的文件列表是外部数据，写回本地前必须过这一关。
func safeRel(rel string) (string, error) {
	for _, seg := range strings.Split(strings.ReplaceAll(rel, "\\", "/"), "/") {
		if seg == ".." {
			return "", errors.New("非法的文件路径")
		}
	}
	clean := path.Clean("/" + rel)
	clean = strings.TrimPrefix(clean, "/")
	if clean == "" || strings.HasPrefix(clean, "..") {
		return "", errors.New("非法的文件路径")
	}
	return clean, nil
}

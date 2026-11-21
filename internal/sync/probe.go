package sync

import "context"

// ProbeResult 是一次「测试连接」看到的东西。
type ProbeResult struct {
	// Files 是远端现有的文件数；-1 表示这次没数（git 只验到仓库可达）。
	Files int
	// New 是远端还没有同步位置（文件夹、目录或分支不在）：不算错，第一次同步
	// 会把它建出来。
	New bool
	// Detail 是给人看的补充，例如「git@github.com:me/notes.git · main」。
	Detail string
}

// Prober 是能自查连接的远端。设置页的「测试连接」用它，比直接跑一次同步快得多，
// 也不会改动远端。
type Prober interface {
	Probe(ctx context.Context) (ProbeResult, error)
}

// Probe 探一次远端。实现了 Prober 的问它；其余的就列一遍目录——对同步来说
// 「列得出来」本身就是连接正常的意思。
func Probe(ctx context.Context, r Remote) (ProbeResult, error) {
	if p, ok := r.(Prober); ok {
		return p.Probe(ctx)
	}
	files, err := r.List(ctx)
	if err != nil {
		return ProbeResult{}, err
	}
	return ProbeResult{Files: len(files)}, nil
}

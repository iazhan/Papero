// Package update 从 GitHub Releases 取新版本，下载、校验、换掉正在运行的 exe。
//
// 它只管「版本怎么比、文件怎么换」，不碰界面，也不 import ui——这样它能脱离窗口
// 做单元测试，将来也能给 CLI 用。
//
// 为什么不用 mygo 自带的 Updater：那套要求 `mygo build` 签名发布（ed25519 + 私钥进
// CI + delta + manifest），而本仓库的发布流水线出的是裸 exe 加一份 SHA256SUMS.txt。
// 这一版先按「GitHub 官方 release + 摘要校验」走通，将来要换签名更新只需要换掉
// Check/Fetch 两步，界面的状态机不用动。
package update

import (
	"strconv"
	"strings"
)

// version 是三段式版本号：主.次.修订，外加可选的前缀（"-beta.1"）。
type version struct {
	nums [3]int
	pre  string
}

// parse 解析 "0.2.1"、"v0.2.1"、"0.3.0-beta.1+sha"；不认识就返回 ok=false。
//
// 构建元数据（"+sha.5d0c1dc"）不参与比较，直接丢掉——同一份代码编两次不该算新版本。
func parse(v string) (version, bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	v, _, _ = strings.Cut(v, "+")
	v, pre, _ := strings.Cut(v, "-")
	parts := strings.Split(v, ".")
	if len(parts) != 3 {
		return version{}, false
	}
	var out version
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return version{}, false
		}
		out.nums[i] = n
	}
	out.pre = pre
	return out, true
}

// Compare 比较两个版本：a 小于 b 返回 -1，相等 0，大于 1。
//
// 不认识的版本（"dev"、git describe 的 "0.2.1-3-gabc123"）一律当作最小：解析不出来
// 的东西不该被当成「更新」推给用户。
func Compare(a, b string) int {
	x, okx := parse(a)
	y, oky := parse(b)
	switch {
	case !okx && !oky:
		return 0
	case !okx:
		return -1
	case !oky:
		return 1
	}
	for i := 0; i < 3; i++ {
		if x.nums[i] != y.nums[i] {
			if x.nums[i] < y.nums[i] {
				return -1
			}
			return 1
		}
	}
	return comparePre(x.pre, y.pre)
}

// comparePre 按 semver 第 11 条排前 release 后缀：没有后缀的大于有后缀的，
// 其余逐个标识符比，数字按数字比（beta.10 在 beta.9 之后），数字小于文字。
func comparePre(a, b string) int {
	switch {
	case a == b:
		return 0
	case a == "":
		return 1
	case b == "":
		return -1
	}
	x, y := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < len(x) && i < len(y); i++ {
		m, errM := strconv.Atoi(x[i])
		n, errN := strconv.Atoi(y[i])
		switch {
		case errM == nil && errN == nil:
			if m != n {
				if m < n {
					return -1
				}
				return 1
			}
		case errM == nil:
			return -1
		case errN == nil:
			return 1
		default:
			if c := strings.Compare(x[i], y[i]); c != 0 {
				return c
			}
		}
	}
	switch {
	case len(x) < len(y):
		return -1
	case len(x) > len(y):
		return 1
	}
	return 0
}

// Newer 报告 a 是不是比 b 新。
func Newer(a, b string) bool { return Compare(a, b) > 0 }

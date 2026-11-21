package sync

import (
	"encoding/base64"
	"strings"
	"testing"
)

// 令牌不能进仓库地址：git 会把地址原样写进副本的 .git/config，而副本就在
// 知识库里，整个文件夹同步出去就带走了。
func TestGitKeepsTokenOutOfURL(t *testing.T) {
	g := &GitRemote{Repo: "owner/name", Token: "ghp_secret_value"}

	if strings.Contains(g.remoteURL(), "ghp_secret_value") {
		t.Errorf("仓库地址里带了令牌：%s", g.remoteURL())
	}
	if !strings.Contains(g.remoteURL(), "owner/name") {
		t.Errorf("仓库地址不对：%s", g.remoteURL())
	}

	args := g.auth()
	if len(args) != 2 || args[0] != "-c" {
		t.Fatalf("鉴权参数形状不对：%q", args)
	}
	const prefix = "http.extraheader=Authorization: Basic "
	if !strings.HasPrefix(args[1], prefix) {
		t.Fatalf("不是 basic 头：%q", args[1])
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(args[1], prefix))
	if err != nil {
		t.Fatalf("头里的 base64 解不开：%v", err)
	}
	if got := string(raw); got != "x-access-token:ghp_secret_value" {
		t.Errorf("鉴权内容 = %q", got)
	}
}

// 没有令牌时不要多带参数。
func TestGitAuthEmptyWithoutToken(t *testing.T) {
	g := &GitRemote{Repo: "owner/name"}
	if args := g.auth(); len(args) != 0 {
		t.Errorf("没有令牌却带了参数：%q", args)
	}
}

// 分支决定状态键：换分支等于换远端。
func TestGitDescriptorIncludesBranch(t *testing.T) {
	a := (&GitRemote{Repo: "owner/name"}).Descriptor()
	b := (&GitRemote{Repo: "owner/name", Branch: "main"}).Descriptor()
	c := (&GitRemote{Repo: "owner/name", Branch: "notes"}).Descriptor()
	if a != b {
		t.Errorf("空分支应当等于 main：%q / %q", a, b)
	}
	if a == c {
		t.Errorf("换分支后描述没变：%q", a)
	}
}

package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"runtime"
	"strings"
)

// Repo 是发布所在的 GitHub 仓库。更新器只认这一个来源。
const Repo = "iazhan/Papero"

// apiBase 是 GitHub 的 API 根；测试改成自己的服务器。
var apiBase = "https://api.github.com"

// sumsName 是 CI 生成的校验和文件（release.yml 里的 SHA256SUMS.txt）。
const sumsName = "SHA256SUMS.txt"

// ErrNoReleases 表示仓库还没有发布过任何版本（第一个 tag 之前）。
var ErrNoReleases = errors.New("还没有发布过版本")

// Release 是一个新版本，连同这个平台要下载的那个文件。
type Release struct {
	// Version 是 "0.2.1"，不带 v。
	Version string
	// Tag 是 release 的标签，原样保留（"v0.2.1"）。
	Tag string
	// Notes 是 release 正文，Markdown。
	Notes string
	// Page 是 release 的网页地址。
	Page string
	// File 是这个平台要下载的文件。
	File Asset
	// Sums 是 SHA256SUMS.txt 的下载地址，没有这一项时为空（那就只校验大小）。
	Sums string
}

// Asset 是 release 里的一个文件。
type Asset struct {
	Name string
	URL  string
	Size int64
}

// AssetName 是这个平台该下载的文件名，和 release.yml 里的产物名一致：
// papero-windows-amd64.exe、papero-linux-amd64、papero-darwin-arm64。
func AssetName(goos, goarch string) string {
	name := "papero-" + goos + "-" + goarch
	if goos == "windows" {
		name += ".exe"
	}
	return name
}

// Check 问 GitHub 要最新一版：比 current 新就返回它，否则返回 nil（已是最新）。
//
// 只认 GitHub 官方 release：不打 tag 的草稿不会出现在 /releases/latest 里，
// 预发布版本也不会——那正是我们想要的（用户默认吃稳定版）。
func Check(ctx context.Context, c *http.Client, repo, current string) (*Release, error) {
	return check(ctx, c, repo, current, runtime.GOOS, runtime.GOARCH)
}

func check(ctx context.Context, c *http.Client, repo, current, goos, goarch string) (*Release, error) {
	url := apiBase + "/repos/" + repo + "/releases/latest"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	// 不带头 User-Agent 的请求会被 GitHub 拒掉；Accept 固定 API 版本，
	// 免得哪天默认字段变了把解析搞坏。
	req.Header.Set("User-Agent", "papero-updater")
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	res, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	switch res.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrNoReleases
	default:
		return nil, fmt.Errorf("GitHub：%s", res.Status)
	}

	var body struct {
		TagName string `json:"tag_name"`
		Body    string `json:"body"`
		HTMLURL string `json:"html_url"`
		Assets  []struct {
			Name string `json:"name"`
			Size int64  `json:"size"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("release：%w", err)
	}
	ver := strings.TrimPrefix(strings.TrimSpace(body.TagName), "v")
	if ver == "" {
		return nil, errors.New("release 没有标签")
	}
	if !Newer(ver, current) {
		return nil, nil
	}

	rel := &Release{Version: ver, Tag: body.TagName, Notes: body.Body, Page: body.HTMLURL}
	want := AssetName(goos, goarch)
	for _, a := range body.Assets {
		switch a.Name {
		case want:
			rel.File = Asset{Name: a.Name, URL: a.URL, Size: a.Size}
		case sumsName:
			rel.Sums = a.URL
		}
	}
	if rel.File.URL == "" {
		return nil, fmt.Errorf("%s 里没有 %s", ver, want)
	}
	return rel, nil
}

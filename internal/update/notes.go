package update

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
)

// Note 是一个版本的更新日志。
type Note struct {
	// Version 是 "0.2.1"，不带 v。
	Version string
	// Notes 是 release 正文，Markdown。
	Notes string
	// URL 是这个版本的 release 页面。
	URL string
}

// MaxNotes 是一次最多给多少个版本。
//
// 落后几十个版本的人不该一次拉几百篇日志下来；超出的部分只报总数，
// 让界面能说「还有 N 个更早的」。
const MaxNotes = 40

// maxPages 是翻页上限：一页 100 个，五页够覆盖 500 个版本。
const maxPages = 5

// Notes 取 after（不含）到 upto（含）之间每个版本的日志，从新到旧。
//
// after 为空时只要 upto 自己的。草稿与预发布不算——用户手上跑的是正式版，
// 给他看没发出去的日志只会更困惑。
func Notes(ctx context.Context, c *http.Client, repo, after, upto string) ([]Note, int, error) {
	var all []Note
	seen := map[string]bool{}
	for page := 1; page <= maxPages; page++ {
		list, err := releases(ctx, c, repo, page)
		if err != nil {
			return nil, 0, err
		}
		for _, r := range list {
			if r.Draft || r.Prerelease {
				continue
			}
			v := strings.TrimPrefix(strings.TrimSpace(r.TagName), "v")
			if v == "" || seen[v] || Newer(v, upto) {
				continue
			}
			if after == "" {
				if v != upto {
					continue
				}
			} else if !Newer(v, after) {
				continue
			}
			seen[v] = true
			all = append(all, Note{Version: v, Notes: strings.TrimSpace(r.Body), URL: r.HTMLURL})
		}
		// 这一页已经翻到 after 或更旧的版本了，再往后只会更旧
		if len(list) < 100 || (after != "" && reachedAfter(list, after)) {
			break
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return Newer(all[i].Version, all[j].Version) })
	total := len(all)
	if total > MaxNotes {
		all = all[:MaxNotes]
	}
	return all, total, nil
}

// reachedAfter 报告这一页里有没有已经到 after（或更旧）的版本。列表是从新到旧，
// 见到一个就不用再翻页了。
func reachedAfter(list []apiRelease, after string) bool {
	for _, r := range list {
		if r.Draft || r.Prerelease {
			continue
		}
		v := strings.TrimPrefix(strings.TrimSpace(r.TagName), "v")
		if v == "" {
			continue
		}
		if !Newer(v, after) {
			return true
		}
	}
	return false
}

// apiRelease 是 GitHub releases 列表里的一项（只挑要用的字段）。
type apiRelease struct {
	TagName    string `json:"tag_name"`
	Body       string `json:"body"`
	HTMLURL    string `json:"html_url"`
	Draft      bool   `json:"draft"`
	Prerelease bool   `json:"prerelease"`
}

func releases(ctx context.Context, c *http.Client, repo string, page int) ([]apiRelease, error) {
	url := fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", apiBase, repo, page)
	res, err := apiGet(ctx, c, url)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var list []apiRelease
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, fmt.Errorf("releases：%w", err)
	}
	return list, nil
}

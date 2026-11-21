package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ErrNoReleases 表示仓库还没有发布过任何版本（第一个 tag 之前）。
var ErrNoReleases = errors.New("还没有发布过版本")

// RateLimitError 是 GitHub 的查询次数用完了。
//
// 未登录的请求按 IP 每小时 60 次——同一个出口 IP（公司网关、代理）是共享的，
// 所以「自己没查几次」也可能撞上。Reset 是配额恢复的时刻（零值表示没说）。
type RateLimitError struct{ Reset time.Time }

func (e *RateLimitError) Error() string {
	if e.Reset.IsZero() {
		return "GitHub 限速：未登录每小时只能查 60 次，稍后再试"
	}
	d := time.Until(e.Reset)
	if d < 0 {
		d = 0
	}
	return fmt.Sprintf("GitHub 限速：未登录每小时只能查 60 次（这个出口 IP 已用完），约 %d 分钟后恢复",
		int(d.Minutes())+1)
}

// apiGet 发一次 GitHub API 请求，只把 200 交出去；其它状态码翻成带原因的错误。
func apiGet(ctx context.Context, c *http.Client, url string) (*http.Response, error) {
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
	if res.StatusCode == http.StatusOK {
		return res, nil
	}
	defer res.Body.Close()
	return nil, apiFailure(res)
}

// apiFailure 把一个非 200 的响应翻成能看懂的错误。
//
// 只说「403 Forbidden」等于没说：403 里最常见的是限速（带上剩余次数与恢复时间），
// 其余情况 GitHub 会在正文里写一句 message，抄出来比状态码有用得多。
func apiFailure(res *http.Response) error {
	if res.StatusCode == http.StatusNotFound {
		return ErrNoReleases
	}
	if res.StatusCode == http.StatusForbidden || res.StatusCode == http.StatusTooManyRequests {
		if reset, ok := rateLimited(res); ok {
			return &RateLimitError{Reset: reset}
		}
	}
	msg := apiMessage(res)
	if msg != "" {
		return fmt.Errorf("GitHub：%s（%s）", res.Status, msg)
	}
	return fmt.Errorf("GitHub：%s", res.Status)
}

// rateLimited 从响应头判断这是不是限速，顺带解出恢复时间。
//
// 429 一定是；403 要看 X-RateLimit-Remaining——GitHub 对「次数用完」也回 403。
func rateLimited(res *http.Response) (time.Time, bool) {
	if res.StatusCode != http.StatusTooManyRequests && res.Header.Get("X-RateLimit-Remaining") != "0" {
		return time.Time{}, false
	}
	if n, err := strconv.ParseInt(res.Header.Get("X-RateLimit-Reset"), 10, 64); err == nil && n > 0 {
		return time.Unix(n, 0), true
	}
	return time.Time{}, true
}

// apiMessage 抄出 GitHub 正文里的 message（最多读 8KB，别为一个错误吞掉整个响应）。
func apiMessage(res *http.Response) string {
	data, err := io.ReadAll(io.LimitReader(res.Body, 8<<10))
	if err != nil {
		return ""
	}
	var body struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.Message)
}

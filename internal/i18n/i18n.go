// Package i18n says the interface in the language the reader picked.
//
// 每条文案有一个稳定的 key 和两种说法（zh / en），表在 messages.go。key 不带原文，
// 所以改文案不会换 key，同一句中文在不同语境也能各说各的。缺 key 时 T 返回 key
// 本身——界面上冒出 pdf.tool.prev_page 很难看，但比空白好查，测试也会先拦下来。
//
// 本包不依赖 UI，任何包都能用。
package i18n

import (
	"strings"
	"sync/atomic"
	"time"
)

// Languages are the languages the interface is written in, by BCP 47 tag.
var Languages = []string{"zh-Hans", "en"}

// current is the language shown, "zh-Hans" or "en".
var current atomic.Value

func init() { current.Store("zh-Hans") }

// SetLanguage shows the interface in tag: English for any "en" tag,
// Chinese otherwise.
func SetLanguage(tag string) {
	if strings.HasPrefix(strings.ToLower(tag), "en") {
		current.Store("en")
		return
	}
	current.Store("zh-Hans")
}

// Language is the language shown.
func Language() string { return current.Load().(string) }

// English reports whether the interface shows English.
func English() bool { return Language() == "en" }

// Pick chooses the system's language among those the interface is written
// in: the first preferred one that is Chinese or English, else English.
func Pick(preferred []string) string {
	for _, p := range preferred {
		p = strings.ToLower(p)
		switch {
		case strings.HasPrefix(p, "zh"):
			return "zh-Hans"
		case strings.HasPrefix(p, "en"):
			return "en"
		}
	}
	return "en"
}

// Tag 是当前语言的 BCP 47 标签，给日期、数字这类跟着语言走的格式化用。
func Tag() string {
	if Language() == "en" {
		return "en"
	}
	return "zh-CN"
}

// Date 按界面语言写日期：中文「2026年11月21日」，英文「Nov 21, 2026」。
//
// 显示日期一律走这里。跟着系统语言跑会出事：中文系统的英文界面里冒出「11月21日」，
// 或者反过来——界面语言和日期格式对不上，读者一眼就觉得别扭。
func Date(t time.Time) string {
	if Language() == "en" {
		return t.Format("Jan 2, 2006")
	}
	return t.Format("2006年1月2日")
}

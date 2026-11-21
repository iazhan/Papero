package update

import "strings"

// zhMarker 是双语更新日志的分界线：这行上面是英文，下面中文。
//
// release 正文一份、两种语言，靠这一行切开——和 magpie 的做法一样。GitHub 页面上
// 读者会先看到英文、再看到一行注释、然后是中文，这是唯一的代价；换来的是客户端
// 不必为两种语言各发一次请求，也不会出现「release 里有中文、英文界面却看不到」。
const zhMarker = "<!-- lang:zh -->"

// InLang 从一份双语更新日志里取 lang 那一半。
//
//	lang = "zh"  → 分界线下面的中文（没有中文就用英文，别给用户看空）
//	其它         → 分界线上面的英文
//
// 没有分界线的（0.2.1 之前的老 release）原样返回：那时候日志只有中文。
func InLang(md, lang string) string {
	i := strings.Index(md, zhMarker)
	if i < 0 {
		return strings.TrimSpace(md)
	}
	if lang == "zh" {
		if zh := strings.TrimSpace(md[i+len(zhMarker):]); zh != "" {
			return zh
		}
	}
	return strings.TrimSpace(md[:i])
}

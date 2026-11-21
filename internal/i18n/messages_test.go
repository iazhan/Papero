package i18n

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode"
)

// key 的形状：小写点分段，段内下划线。形状固定了才好按域找、按域删。
var keyPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(\.[a-z][a-z0-9_]*)+$`)

func TestMessageShape(t *testing.T) {
	for key, m := range messages {
		if !keyPattern.MatchString(key) {
			t.Errorf("key %q 不合规范（应是小写点分段，如 pdf.tool.prev_page）", key)
		}
		if strings.TrimSpace(m.zh) == "" {
			t.Errorf("%s 没有中文", key)
		}
		if strings.TrimSpace(m.en) == "" {
			t.Errorf("%s 没有英文", key)
		}
	}
}

// 代码里 T("…") 的每个 key 都得在表里。key 是字符串，拼错了编译器不管，
// 界面上就会冒出一串 "pdf.tool.prev_pag"——这条测试把拼错变成失败。
func TestMessageKeysExist(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var bad []string

	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case "ref", "tmp", "build", "node_modules", ".git", ".workbuddy":
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, perr := parser.ParseFile(fset, path, nil, 0)
		if perr != nil {
			return perr
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "i18n" || sel.Sel.Name != "T" {
				return true
			}
			lit, ok := call.Args[0].(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			key, uerr := strconv.Unquote(lit.Value)
			if uerr != nil {
				return true
			}
			if _, ok := messages[key]; !ok {
				bad = append(bad, fmt.Sprintf("%s  %s", fset.Position(lit.Pos()), key))
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatalf("扫描源码失败：%v", err)
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("这些 key 不在 messages 表里（%d 处）：\n%s", len(bad), strings.Join(bad, "\n"))
	}
}

func TestT(t *testing.T) {
	t.Cleanup(func() { SetLanguage("zh-Hans") })

	SetLanguage("zh-Hans")
	if got := T("graph.title"); got != "知识图谱" {
		t.Errorf("中文下 T(graph.title) = %q", got)
	}
	SetLanguage("en")
	if got := T("graph.title"); got != "Knowledge Graph" {
		t.Errorf("英文下 T(graph.title) = %q", got)
	}
	// 拼错时不 panic、不空白，直接把 key 抛到台面上
	if got := T("graph.nope"); got != "graph.nope" {
		t.Errorf("缺 key 应返回 key 本身，得到 %q", got)
	}
}

// 英文里不许出现中文。源文案是中文，抄一条中文进 en 字段很难自己发现，
// 而英文用户会直接看到——magpie 那边是真踩过这个坑（英文窗口里冒出「接入」）。
//
// 允许清单留给「故意要显示中文」的条目，比如字体预览的样字；加的时候写清为什么。
var englishMayHoldCJK = map[string]string{}

func TestEnglishHasNoChinese(t *testing.T) {
	for key, m := range messages {
		cjk := hasCJK(m.en)
		_, allowed := englishMayHoldCJK[key]
		if cjk && !allowed {
			t.Errorf("%s 的英文里有中文：%q", key, m.en)
		}
		if allowed && !cjk {
			t.Errorf("%s 在允许清单里，但英文里已经没有中文了，把它从清单删掉", key)
		}
	}
}

// hasCJK 认汉字、假名、谚文和中文标点。全角加号之类不算——那是符号，不是词。
func hasCJK(s string) bool {
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Han, r), unicode.Is(unicode.Hiragana, r),
			unicode.Is(unicode.Katakana, r), unicode.Is(unicode.Hangul, r):
			return true
		}
		switch r {
		// 只列中文专用的标点。…、·、— 英文里也用，不算。
		case '　', '，', '。', '、', '：', '；', '！', '？', '「', '」', '『', '』',
			'（', '）', '《', '》', '〈', '〉':
			return true
		}
	}
	return false
}

// Tn 按数量选一条：中文两条一样，英文不同。
func TestTn(t *testing.T) {
	t.Cleanup(func() { SetLanguage("zh-Hans") })

	SetLanguage("en")
	if got := Tn(1, "graph.stats.docs.one", "graph.stats.docs.other", 1); got != "1 document" {
		t.Errorf("一个 = %q", got)
	}
	if got := Tn(3, "graph.stats.docs.one", "graph.stats.docs.other", 3); got != "3 documents" {
		t.Errorf("三个 = %q", got)
	}
	SetLanguage("zh-Hans")
	if got := Tn(3, "graph.stats.docs.one", "graph.stats.docs.other", 3); got != "3 篇" {
		t.Errorf("中文 = %q", got)
	}
}

// 日期跟界面语言走，不跟系统。
func TestDateFollowsLanguage(t *testing.T) {
	t.Cleanup(func() { SetLanguage("zh-Hans") })
	day := time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)

	SetLanguage("zh-Hans")
	if got := Date(day); got != "2026年11月21日" {
		t.Errorf("中文日期 = %q", got)
	}
	SetLanguage("en")
	if got := Date(day); got != "Nov 21, 2026" {
		t.Errorf("英文日期 = %q", got)
	}
	if got := Tag(); got != "en" {
		t.Errorf("英文 Tag = %q", got)
	}
}

// one 和 other 必须成对：只写一条，Tn 的另一半就会在界面上露出 key。
func TestPluralPairs(t *testing.T) {
	for key := range messages {
		if other, ok := strings.CutSuffix(key, ".one"); ok {
			if _, has := messages[other+".other"]; !has {
				t.Errorf("%s 没有配对的 %s.other", key, other)
			}
		}
		if base, ok := strings.CutSuffix(key, ".other"); ok {
			if _, has := messages[base+".one"]; !has {
				t.Errorf("%s 没有配对的 %s.one", key, base)
			}
		}
	}
}

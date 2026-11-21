package app

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode"
)

// dataOnly 里的中文不是界面文案：shot.go 是 -shot 调试工具，写的是样例库内容和
// 终端输出，界面上看不到。别往里加真正的界面文案。
var dataOnly = map[string]string{
	"shot.go": "-shot 用的样例库与终端输出",
}

// app 包的函数体里不该出现任何中文字面量——界面文案一律走 i18n.T(key)。
//
// 只扫函数体：包级 const/var 放的是数据（整篇欢迎文档、字体名之类），
// 由用到的地方决定翻不翻，不是这里该管的事。
func TestNoUnwrappedChinese(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var bad []string
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") {
			continue
		}
		if _, ok := dataOnly[name]; ok {
			continue
		}
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		file, err := parser.ParseFile(fset, name, src, 0)
		if err != nil {
			t.Fatal(err)
		}
		// 只进函数体：包级 const/var 是数据，翻不翻由用到它的地方决定。
		var bodies []ast.Node
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Body != nil {
				bodies = append(bodies, fn.Body)
			}
		}
		for _, body := range bodies {
			ast.Inspect(body, func(n ast.Node) bool {
				lit, ok := n.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					return true
				}
				s, err := strconv.Unquote(lit.Value)
				if err != nil || !hasHan(s) {
					return true
				}
				pos := fset.Position(lit.Pos())
				bad = append(bad, fmt.Sprintf("%s  %q", pos, s))
				return true
			})
		}
	}
	if len(bad) > 0 {
		sort.Strings(bad)
		t.Fatalf("函数体里还有中文字面量（%d 处），界面文案要写成 i18n.T(<key>)：\n%s", len(bad), strings.Join(bad, "\n"))
	}
}

func hasHan(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}

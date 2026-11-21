package richdoc

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"papero/internal/tex"
)

type inlineMathEdit struct {
	blockID    string
	start, end int
	source     string
}

// Inline syntax stays lossless in the model. Only layout replaces it with an
// object, with a reversible UTF-16 mapping shared by caret, selection and PDF.
type inlineToken struct {
	start, end   int
	source, note string
}

type inlineObject struct {
	start, end, index int
	box               *tex.Box
	label             *ui.TextLayout
	sourceLayout      *ui.TextLayout
	source            string
	note              string
	rect              ui.Rect
	scale             float32
	script            bool
	editing           bool
	placeholder       bool
	hasErr            bool
}

type textLayout struct {
	*ui.TextLayout
	objects []inlineObject
}

func inlineTokens(s string) []inlineToken {
	var out []inlineToken
	for i := 0; i < len(s); {
		if s[i] == '\\' {
			_, n := utf8.DecodeRuneInString(s[min(i+1, len(s)):])
			i += 1 + n
			continue
		}
		if strings.HasPrefix(s[i:], "[^") {
			if n := strings.IndexByte(s[i+2:], ']'); n > 0 {
				z := i + 2 + n + 1
				label := s[i+2 : z-1]
				if !strings.ContainsAny(label, " \t\r\n[") {
					out = append(out, inlineToken{i, z, "", label})
					i = z
					continue
				}
			}
		}
		if s[i] != '$' || i+1 >= len(s) || s[i+1] == '$' || i > 0 && s[i-1] == '$' {
			i++
			continue
		}
		first, _ := utf8.DecodeRuneInString(s[i+1:])
		if unicode.IsSpace(first) {
			i++
			continue
		}
		z := i + 1
		for z < len(s) && s[z] != '\n' {
			if s[z] == '\\' {
				z += min(2, len(s)-z)
				continue
			}
			if s[z] == '$' {
				break
			}
			z++
		}
		if z >= len(s) || s[z] != '$' {
			i++
			continue
		}
		last, _ := utf8.DecodeLastRuneInString(s[i+1 : z])
		if z == i+1 || unicode.IsSpace(last) || z+1 < len(s) && (s[z+1] >= '0' && s[z+1] <= '9' || s[z+1] == '$') {
			i++
			continue
		}
		out = append(out, inlineToken{i, z + 1, s[i+1 : z], ""})
		i = z + 1
	}
	return out
}

func literalBlock(kind string) bool {
	return kind == "code" || kind == "raw" || kind == "math" || kind == "image"
}

func tokenAllowed(b *Block, a, z int) bool {
	if literalBlock(b.Kind) {
		return false
	}
	for _, r := range SliceRuns(b.Runs, a, z) {
		if r.Style.Code || r.Style.Link != "" || r.Style.NoMath {
			return false
		}
	}
	return true
}

func scanDollarSpan(s string, openByte int) (closeByte int, ok bool) {
	if openByte < 0 || openByte >= len(s) || s[openByte] != '$' {
		return 0, false
	}
	if openByte > 0 && (s[openByte-1] == '$' || s[openByte-1] == '\\') {
		return 0, false
	}
	z := openByte + 1
	for z < len(s) && s[z] != '\n' {
		if s[z] == '\\' {
			z += min(2, len(s)-z)
			continue
		}
		if s[z] == '$' {
			if z+1 < len(s) && s[z+1] == '$' {
				return 0, false
			}
			return z, true
		}
		z++
	}
	return 0, false
}

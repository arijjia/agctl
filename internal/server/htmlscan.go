package server

// htmlscan.go — HTML-aware сканирование для инъекций head-содержимого.
// Комментарии, quoted attributes и raw-text элементы пропускаются, поэтому
// literal "</head>" внутри <script> не может разрезать документ.

import (
	"html"
	"strings"

	"github.com/arijjia/agctl/internal/profile"
)

// openingTagEnd — HTML-aware scan до открывающего тега name.
func openingTagEnd(s, name string) int {
	lower := asciiLower(s)
	for pos := 0; pos < len(s); {
		rel := strings.IndexByte(s[pos:], '<')
		if rel < 0 {
			return -1
		}
		i := pos + rel
		if strings.HasPrefix(s[i:], "<!--") {
			end := strings.Index(s[i+4:], "-->")
			if end < 0 {
				return -1
			}
			pos = i + 4 + end + 3
			continue
		}
		if i+1 >= len(s) || s[i+1] == '!' || s[i+1] == '?' {
			end := htmlTagEnd(s, i+1)
			if end < 0 {
				return -1
			}
			pos = end + 1
			continue
		}
		j := i + 1
		closing := false
		if s[j] == '/' {
			closing = true
			j++
		}
		start := j
		for j < len(s) && isHTMLNameByte(s[j]) {
			j++
		}
		if j == start {
			pos = i + 1
			continue
		}
		tag := lower[start:j]
		end := htmlTagEnd(s, j)
		if end < 0 {
			return -1
		}
		if !closing && tag == name {
			return end + 1
		}
		pos = end + 1
		if !closing && isHTMLRawText(tag) {
			close := rawTextClose(lower, pos, tag)
			if close < 0 {
				return -1
			}
			closeEnd := htmlTagEnd(s, close+2+len(tag))
			if closeEnd < 0 {
				return -1
			}
			pos = closeEnd + 1
		}
	}
	return -1
}

func htmlTagEnd(s string, start int) int {
	var quote byte
	for i := start; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
		} else if c == '>' {
			return i
		}
	}
	return -1
}

func rawTextClose(lower string, from int, name string) int {
	needle := "</" + name
	for from < len(lower) {
		rel := strings.Index(lower[from:], needle)
		if rel < 0 {
			return -1
		}
		i := from + rel
		end := i + len(needle)
		if end == len(lower) || lower[end] == '>' || lower[end] == '/' || lower[end] == ' ' || lower[end] == '\n' || lower[end] == '\t' || lower[end] == '\r' {
			return i
		}
		from = end
	}
	return -1
}

func isHTMLNameByte(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ':' || c == '-'
}

// asciiLower меняет только ASCII-теги; byte offsets остаются точными даже
// если до тега встречается Unicode-текст с многобайтовым case mapping.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

func isHTMLRawText(tag string) bool {
	switch tag {
	case "script", "style", "textarea", "title", "xmp", "iframe", "noembed", "noframes":
		return true
	default:
		return false
	}
}

func doctypeEnd(s string) int {
	lower := asciiLower(s)
	i := strings.Index(lower, "<!doctype")
	if i < 0 {
		return -1
	}
	if end := htmlTagEnd(s, i+2); end >= 0 {
		return end + 1
	}
	return -1
}

// injectBrandBoot ставит window.__AGCTL_BRAND__ до загрузки SPA-модулей
// (страница входа) и подменяет <title> на бренд профиля. Идемпотентно.
func injectBrandBoot(index []byte, v profile.View) []byte {
	s := string(index)
	if strings.Contains(s, `data-agctl="brand-boot"`) {
		return index
	}
	if v.Title != "" {
		if start := strings.Index(s, "<title>"); start >= 0 {
			if end := strings.Index(s[start:], "</title>"); end >= 0 {
				s = s[:start] + "<title>" + html.EscapeString(v.Title) + "</title>" + s[start+end+len("</title>"):]
			}
		}
	}
	script := v.BrandBootScript()
	if i := openingTagEnd(s, "head"); i >= 0 {
		return []byte(s[:i] + script + s[i:])
	}
	return []byte(script + s)
}

package gojinja2

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/yzfly/gojinja2/runtime"
)

// splitKeep 复刻 re.split 含捕获组的行为 (分隔符也保留在结果中).
func splitKeep(re *regexp.Regexp, s string) []string {
	var out []string
	last := 0
	for _, loc := range re.FindAllStringIndex(s, -1) {
		out = append(out, s[last:loc[0]], s[loc[0]:loc[1]])
		last = loc[1]
	}
	return append(out, s[last:])
}

func pyIsSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return r == 0x85 || r == 0xa0 || (r >= 0x1c && r <= 0x1f) ||
		(r >= 0x2000 && r <= 0x200a) || r == 0x1680 || r == 0x2028 ||
		r == 0x2029 || r == 0x202f || r == 0x205f || r == 0x3000
}

// pySplitlinesStr 同 str.splitlines (不保留行尾).
func pySplitlinesStr(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' || c == '\r' {
			out = append(out, s[start:i])
			if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	if out == nil {
		out = []string{}
	}
	return out
}

var tagRe = regexp.MustCompile(`(?s)(<!--.*?-->|<[^>]*>)`)
var wsRunRe = regexp.MustCompile(`\s+`)

// markupStriptags 复刻 markupsafe Markup.striptags:
// 去注释和标签, 邻接空白合一, 再 unescape HTML 实体.
func markupStriptags(s string) string {
	stripped := tagRe.ReplaceAllString(s, "")
	stripped = strings.TrimSpace(wsRunRe.ReplaceAllString(stripped, " "))
	return htmlUnescape(stripped)
}

var entityMap = map[string]string{
	"lt": "<", "gt": ">", "amp": "&", "quot": "\"", "#39": "'",
	"#34": "\"", "apos": "'", "nbsp": " ",
}

func htmlUnescape(s string) string {
	if !strings.Contains(s, "&") {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != '&' {
			b.WriteByte(s[i])
			i++
			continue
		}
		end := strings.IndexByte(s[i:], ';')
		if end < 0 || end > 10 {
			b.WriteByte(s[i])
			i++
			continue
		}
		name := s[i+1 : i+end]
		if r, ok := entityMap[name]; ok {
			b.WriteString(r)
		} else if strings.HasPrefix(name, "#x") || strings.HasPrefix(name, "#X") {
			if n, ok := parsePyInt(name[2:], 16); ok {
				b.WriteRune(rune(n))
			} else {
				b.WriteString(s[i : i+end+1])
			}
		} else if strings.HasPrefix(name, "#") {
			if n, ok := parsePyInt(name[1:], 10); ok {
				b.WriteRune(rune(n))
			} else {
				b.WriteString(s[i : i+end+1])
			}
		} else {
			b.WriteString(s[i : i+end+1])
		}
		i += end + 1
	}
	return b.String()
}

// ---- textwrap.wrap 移植 (expand_tabs=False, replace_whitespace=False) ----

// textwrapChunks 复刻 textwrap 的分块: 空白段与非空白段交替;
// break_on_hyphens 时, 字母后跟连字符且后续是字母的位置也是块边界
// (原版用 lookahead 正则, RE2 不支持, 手写实现).
func textwrapChunks(text string, breakOnHyphens bool) []string {
	isWS := func(b byte) bool {
		switch b {
		case '\t', '\n', '\v', '\f', '\r', ' ':
			return true
		}
		return false
	}
	var chunks []string
	i := 0
	for i < len(text) {
		j := i
		if isWS(text[i]) {
			for j < len(text) && isWS(text[j]) {
				j++
			}
			chunks = append(chunks, text[i:j])
			i = j
			continue
		}
		for j < len(text) && !isWS(text[j]) {
			j++
		}
		seg := text[i:j]
		if breakOnHyphens {
			chunks = append(chunks, splitHyphens(seg)...)
		} else {
			chunks = append(chunks, seg)
		}
		i = j
	}
	return chunks
}

func isLetterByte(runes []rune, idx int) bool {
	if idx < 0 || idx >= len(runes) {
		return false
	}
	r := runes[idx]
	return r == '_' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127
}

// splitHyphens 在 字母- 后跟字母 处切块 (近似原版 wordsep 规则).
func splitHyphens(seg string) []string {
	runes := []rune(seg)
	var out []string
	start := 0
	for i, r := range runes {
		if r == '-' && isLetterByte(runes, i-1) && isLetterByte(runes, i+1) {
			out = append(out, string(runes[start:i+1]))
			start = i + 1
		}
	}
	if start < len(runes) {
		out = append(out, string(runes[start:]))
	}
	return out
}

// textwrapWrap 复刻 textwrap.wrap 的核心逻辑 (drop_whitespace=True).
func textwrapWrap(text string, width int, breakLongWords, breakOnHyphens bool) []string {
	chunks := textwrapChunks(text, breakOnHyphens)
	var lines []string

	i := 0
	for i < len(chunks) {
		var cur []string
		curLen := 0

		// 行首丢弃空白 (除第一行外; textwrap: drop if stripped == '' and lines)
		if len(lines) > 0 && i < len(chunks) && strings.TrimSpace(chunks[i]) == "" {
			i++
			continue
		}

		for i < len(chunks) {
			l := runesLen(chunks[i])
			if curLen+l <= width {
				cur = append(cur, chunks[i])
				curLen += l
				i++
			} else {
				break
			}
		}

		// 处理超长块
		if i < len(chunks) && runesLen(chunks[i]) > width {
			if breakLongWords {
				spaceLeft := width - curLen
				if width < 1 {
					spaceLeft = 1
				}
				if spaceLeft > 0 {
					r := []rune(chunks[i])
					cur = append(cur, string(r[:spaceLeft]))
					chunks[i] = string(r[spaceLeft:])
					curLen = width
				}
			} else if len(cur) == 0 {
				cur = append(cur, chunks[i])
				i++
			}
		}

		// 行尾丢弃空白
		for len(cur) > 0 && strings.TrimSpace(cur[len(cur)-1]) == "" {
			cur = cur[:len(cur)-1]
		}
		if len(cur) > 0 {
			lines = append(lines, strings.Join(cur, ""))
		}
	}
	return lines
}

// ---- urlencode (urllib.parse.quote) ----

const urlAlwaysSafe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-~"

// urlQuote 复刻 urllib.parse.quote; forQS 时 '/' 也转义且空格转 +?
// Python urlencode 用 quote_via=quote_plus? jinja 的 url_quote:
// quote(str(obj), safe="" if for_qs else "/")
func urlQuote(s string, forQS bool) string {
	safe := "/"
	if forQS {
		safe = ""
	}
	var b strings.Builder
	for _, c := range []byte(s) {
		if strings.IndexByte(urlAlwaysSafe, c) >= 0 ||
			(safe != "" && strings.IndexByte(safe, c) >= 0) {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

// ---- urlize ----

var httpRe = regexp.MustCompile(`(?i)^((https?://|www\.)(([\w%-]+\.)+)?([a-z]{2,63}|xn--[\w%]{2,59})|([\w%-]{2,63}\.)+(com|net|int|edu|gov|org|info|mil)|(https?://)((([0-9]{1,3})(\.[0-9]{1,3}){3})|(\[([0-9a-f]{0,4}:){2}([0-9a-f]{0,4}:?){1,6}\])))(:[0-9]{1,5})?([/?#]\S*)?$`)
var emailRe = regexp.MustCompile(`^\S+@[\w][\w.-]*\.[\w]+$`)
var urlizeHeadRe = regexp.MustCompile(`^([(<]|&lt;)+`)
var urlizeTailRe = regexp.MustCompile(`([)>.,\n]|&gt;)+$`)
var urlizeSplitRe = regexp.MustCompile(`(\s+)`)

func urlizeImpl(text string, trimLimit int, rel, target string, extraSchemes []string) string {
	trimURL := func(x string) string {
		if trimLimit >= 0 && runesLen(x) > trimLimit {
			r := []rune(x)
			return string(r[:trimLimit]) + "..."
		}
		return x
	}
	words := splitKeep(urlizeSplitRe, string(runtime.Escape(text)))
	relAttr := ""
	if rel != "" {
		relAttr = ` rel="` + string(runtime.Escape(rel)) + `"`
	}
	targetAttr := ""
	if target != "" {
		targetAttr = ` target="` + string(runtime.Escape(target)) + `"`
	}

	for i, word := range words {
		head, middle, tail := "", word, ""
		if mloc := urlizeHeadRe.FindString(middle); mloc != "" {
			head = mloc
			middle = middle[len(mloc):]
		}
		if strings.HasSuffix(middle, ")") || strings.HasSuffix(middle, ">") ||
			strings.HasSuffix(middle, ".") || strings.HasSuffix(middle, ",") ||
			strings.HasSuffix(middle, "\n") || strings.HasSuffix(middle, "&gt;") {
			if loc := urlizeTailRe.FindStringIndex(middle); loc != nil {
				tail = middle[loc[0]:]
				middle = middle[:loc[0]]
			}
		}
		// 括号配平
		for _, pair := range [][2]string{{"(", ")"}, {"<", ">"}, {"&lt;", "&gt;"}} {
			startCount := strings.Count(middle, pair[0])
			if startCount <= strings.Count(middle, pair[1]) {
				continue
			}
			n := strings.Count(tail, pair[1])
			if startCount < n {
				n = startCount
			}
			for j := 0; j < n; j++ {
				endIndex := strings.Index(tail, pair[1]) + len(pair[1])
				middle += tail[:endIndex]
				tail = tail[endIndex:]
			}
		}

		switch {
		case httpRe.MatchString(middle):
			if strings.HasPrefix(middle, "https://") || strings.HasPrefix(middle, "http://") {
				middle = `<a href="` + middle + `"` + relAttr + targetAttr + `>` +
					trimURL(middle) + `</a>`
			} else {
				middle = `<a href="https://` + middle + `"` + relAttr + targetAttr + `>` +
					trimURL(middle) + `</a>`
			}
		case strings.HasPrefix(middle, "mailto:") && emailRe.MatchString(middle[7:]):
			middle = `<a href="` + middle + `">` + middle[7:] + `</a>`
		case strings.Contains(middle, "@") && !strings.HasPrefix(middle, "www.") &&
			!strings.HasPrefix(middle, "@") && !strings.Contains(middle, ":") &&
			emailRe.MatchString(middle):
			middle = `<a href="mailto:` + middle + `">` + middle + `</a>`
		default:
			for _, scheme := range extraSchemes {
				if middle != scheme && strings.HasPrefix(middle, scheme) {
					middle = `<a href="` + middle + `"` + relAttr + targetAttr + `>` +
						middle + `</a>`
					break
				}
			}
		}
		words[i] = head + middle + tail
	}
	return strings.Join(words, "")
}

// ---- json.dumps (sort_keys=True, ensure_ascii=True) ----

func pyJSONDumps(v any, indent int, sortKeys bool) string {
	var b strings.Builder
	jsonDumpValue(&b, v, indent, sortKeys, 0)
	return b.String()
}

func jsonDumpValue(b *strings.Builder, v any, indent int, sortKeys bool, depth int) {
	switch tv := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		if tv {
			b.WriteString("true")
		} else {
			b.WriteString("false")
		}
	case int64:
		fmt.Fprintf(b, "%d", tv)
	case float64:
		b.WriteString(runtime.PyFloatStr(tv))
	case string:
		jsonDumpString(b, tv)
	case runtime.Markup:
		jsonDumpString(b, string(tv))
	case []any:
		jsonDumpArray(b, tv, indent, sortKeys, depth)
	case runtime.Tuple:
		jsonDumpArray(b, tv, indent, sortKeys, depth)
	case *runtime.Dict:
		items := tv.Items()
		if sortKeys {
			items = append([]runtime.DictItem{}, items...)
			sort.SliceStable(items, func(i, j int) bool {
				return pyCmpLess(items[i].Key, items[j].Key)
			})
		}
		if len(items) == 0 {
			b.WriteString("{}")
			return
		}
		open, sep, kvSep, closeS := jsonSeps(indent, depth, "{", "}")
		b.WriteString(open)
		for i, it := range items {
			if i > 0 {
				b.WriteString(sep)
			}
			ks, ok := it.Key.(string)
			if !ok {
				if mk, isM := it.Key.(runtime.Markup); isM {
					ks = string(mk)
				} else {
					ks = runtime.Str(it.Key) // 数字键转字符串 (json.dumps 行为)
				}
			}
			jsonDumpString(b, ks)
			b.WriteString(kvSep)
			jsonDumpValue(b, it.Value, indent, sortKeys, depth+1)
		}
		b.WriteString(closeS)
	case *runtime.Undefined:
		tv.Fail()
	default:
		runtime.RaiseType("Object of type " + runtime.PyTypeName(v) +
			" is not JSON serializable")
	}
}

func jsonDumpArray(b *strings.Builder, items []any, indent int, sortKeys bool, depth int) {
	if len(items) == 0 {
		b.WriteString("[]")
		return
	}
	open, sep, _, closeS := jsonSeps(indent, depth, "[", "]")
	b.WriteString(open)
	for i, item := range items {
		if i > 0 {
			b.WriteString(sep)
		}
		jsonDumpValue(b, item, indent, sortKeys, depth+1)
	}
	b.WriteString(closeS)
}

func jsonSeps(indent, depth int, open, close string) (string, string, string, string) {
	if indent < 0 {
		return open, ", ", ": ", close
	}
	pad := strings.Repeat(" ", indent*(depth+1))
	padClose := strings.Repeat(" ", indent*depth)
	return open + "\n" + pad, ",\n" + pad, ": ", "\n" + padClose + close
}

func jsonDumpString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			switch {
			case r < 0x20:
				fmt.Fprintf(b, `\u%04x`, r)
			case r < 0x7f:
				b.WriteRune(r)
			case r > 0xffff:
				// ensure_ascii: 代理对
				r -= 0x10000
				fmt.Fprintf(b, `\u%04x\u%04x`, 0xd800+(r>>10), 0xdc00+(r&0x3ff))
			default:
				fmt.Fprintf(b, `\u%04x`, r)
			}
		}
	}
	b.WriteByte('"')
}

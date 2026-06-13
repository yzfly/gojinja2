package lexer

import (
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ---- 空白 ----

// isSpaceRune 复刻 Python re 的 \s (含 \x1c-\x1f 文件分隔符).
func isSpaceRune(r rune) bool {
	return unicode.IsSpace(r) || (r >= 0x1c && r <= 0x1f)
}

func isSpaceByte(b byte) bool {
	if b < utf8.RuneSelf {
		return isSpaceRune(rune(b))
	}
	// 多字节字符的中间字节不可能是 ASCII 空白; 这里只用于 lookbehind 判断,
	// 取该字节所在字符是否空白由调用方的字节级回看近似 (非 ASCII 视为非空白)
	return false
}

// skipSpace 返回从 p 起连续空白后的位置 (\s*).
func skipSpace(src string, p int) int {
	for p < len(src) {
		r, sz := utf8.DecodeRuneInString(src[p:])
		if !isSpaceRune(r) {
			break
		}
		p += sz
	}
	return p
}

// skipInlineSpace 复刻 [^\S\r\n]* : 除换行外的空白.
func skipInlineSpace(src string, p int) int {
	for p < len(src) {
		r, sz := utf8.DecodeRuneInString(src[p:])
		if r == '\n' || r == '\r' || !isSpaceRune(r) {
			break
		}
		p += sz
	}
	return p
}

func isAllSpace(s string) bool {
	for _, r := range s {
		if !isSpaceRune(r) {
			return false
		}
	}
	return true
}

// ---- 数字 ----

func isDigit(b byte) bool { return b >= '0' && b <= '9' }

// scanDigitRun 匹配 (\d+_)*\d+ : 数字段, 段间单个下划线分隔.
// 返回结束位置, 不匹配返回 -1.
func scanDigitRun(src string, p int) int {
	if p >= len(src) || !isDigit(src[p]) {
		return -1
	}
	for {
		for p < len(src) && isDigit(src[p]) {
			p++
		}
		if p+1 < len(src) && src[p] == '_' && isDigit(src[p+1]) {
			p += 2
		} else {
			return p
		}
	}
}

// scanExponent 匹配 e[+\-]?(\d+_)*\d+ (大小写不敏感).
func scanExponent(src string, p int) int {
	if p >= len(src) || (src[p] != 'e' && src[p] != 'E') {
		return -1
	}
	q := p + 1
	if q < len(src) && (src[q] == '+' || src[q] == '-') {
		q++
	}
	return scanDigitRun(src, q)
}

// matchFloat 复刻 float_re:
//
//	(?<!\.)(\d+_)*\d+((\.(\d+_)*\d+)?e[+\-]?(\d+_)*\d+|\.(\d+_)*\d+)
func matchFloat(src string, pos int) (string, bool) {
	if pos > 0 && src[pos-1] == '.' {
		return "", false // lookbehind: 不允许跟在 . 之后
	}
	p := scanDigitRun(src, pos)
	if p < 0 {
		return "", false
	}
	// 分支 A: 可选小数部分 + 必需指数 (贪婪: 先试带小数)
	frac := -1
	if p < len(src) && src[p] == '.' {
		frac = scanDigitRun(src, p+1)
	}
	if frac > 0 {
		if e := scanExponent(src, frac); e > 0 {
			return src[pos:e], true
		}
	}
	if e := scanExponent(src, p); e > 0 {
		return src[pos:e], true
	}
	// 分支 B: 必需小数部分
	if frac > 0 {
		return src[pos:frac], true
	}
	return "", false
}

// matchInteger 复刻 integer_re (IGNORECASE), 分支按序:
//
//	0b(_?[01])+ | 0o(_?[0-7])+ | 0x(_?[\da-f])+ | [1-9](_?\d)* | 0(_?0)*
func matchInteger(src string, pos int) (string, bool) {
	if pos >= len(src) {
		return "", false
	}
	if src[pos] == '0' && pos+1 < len(src) {
		var ok func(byte) bool
		switch src[pos+1] {
		case 'b', 'B':
			ok = func(b byte) bool { return b == '0' || b == '1' }
		case 'o', 'O':
			ok = func(b byte) bool { return b >= '0' && b <= '7' }
		case 'x', 'X':
			ok = func(b byte) bool {
				return isDigit(b) || (b >= 'a' && b <= 'f') || (b >= 'A' && b <= 'F')
			}
		}
		if ok != nil {
			// (_?digit)+
			p, n := pos+2, 0
			for p < len(src) {
				q := p
				if src[q] == '_' && q+1 < len(src) {
					q++
				}
				if q < len(src) && ok(src[q]) {
					p = q + 1
					n++
				} else {
					break
				}
			}
			if n > 0 {
				return src[pos:p], true
			}
			// 前缀后无合法数字: 回落到十进制分支
		}
	}
	b := src[pos]
	if b >= '1' && b <= '9' {
		// [1-9](_?\d)*
		p := pos + 1
		for p < len(src) {
			q := p
			if src[q] == '_' && q+1 < len(src) {
				q++
			}
			if q < len(src) && isDigit(src[q]) {
				p = q + 1
			} else {
				break
			}
		}
		return src[pos:p], true
	}
	if b == '0' {
		// 0(_?0)*
		p := pos + 1
		for p < len(src) {
			q := p
			if src[q] == '_' && q+1 < len(src) {
				q++
			}
			if q < len(src) && src[q] == '0' {
				p = q + 1
			} else {
				break
			}
		}
		return src[pos:p], true
	}
	return "", false
}

// ---- 标识符 ----

// isIdentStart 近似 Python 的 XID_Start (字母 + Nl + 下划线).
func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
}

// isIdentContinue 近似 XID_Continue.
func isIdentContinue(r rune) bool {
	return isIdentStart(r) || unicode.Is(unicode.Nd, r) ||
		unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Mc, r) ||
		unicode.Is(unicode.Pc, r)
}

func matchName(src string, pos int) (string, bool) {
	r, sz := utf8.DecodeRuneInString(src[pos:])
	if !isIdentStart(r) {
		return "", false
	}
	p := pos + sz
	for p < len(src) {
		r, sz = utf8.DecodeRuneInString(src[p:])
		if !isIdentContinue(r) {
			break
		}
		p += sz
	}
	return src[pos:p], true
}

// isIdentifier 对应 str.isidentifier 的校验 (近似 XID).
func isIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isIdentStart(r) {
				return false
			}
		} else if !isIdentContinue(r) {
			return false
		}
	}
	return true
}

// ---- 字符串 ----

// matchString 复刻 string_re: 单/双引号字符串, 反斜杠可转义任意字符 (含换行).
// 返回含引号的完整文本.
func matchString(src string, pos int) (string, bool) {
	quote := src[pos]
	if quote != '\'' && quote != '"' {
		return "", false
	}
	p := pos + 1
	for p < len(src) {
		switch src[p] {
		case '\\':
			p += 2 // 转义任意字符; 若 \ 在串尾则越界, 下面统一判未闭合
		case quote:
			return src[pos : p+1], true
		default:
			p++
		}
	}
	return "", false // 未闭合: 落入 unexpected char 错误
}

// pyUnescape 复刻 Python bytes.decode("unicode-escape") 的语义
// (经 backslashreplace 往返后, 非 ASCII 字符原样保留).
// 未知转义序列按 Python 行为原样保留反斜杠.
func pyUnescape(s string) (string, error) {
	if !strings.ContainsRune(s, '\\') {
		return s, nil
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			i++
			continue
		}
		if i+1 >= len(s) {
			return "", errors.New(`\ at end of string`)
		}
		e := s[i+1]
		i += 2
		switch e {
		case '\n': // 行延续
		case '\\':
			b.WriteByte('\\')
		case '\'':
			b.WriteByte('\'')
		case '"':
			b.WriteByte('"')
		case 'a':
			b.WriteByte('\a')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'v':
			b.WriteByte('\v')
		case '0', '1', '2', '3', '4', '5', '6', '7':
			// 八进制, 最多 3 位
			n := int(e - '0')
			for k := 0; k < 2 && i < len(s) && s[i] >= '0' && s[i] <= '7'; k++ {
				n = n*8 + int(s[i]-'0')
				i++
			}
			b.WriteRune(rune(n))
		case 'x':
			r, ni, err := hexEscape(s, i, 2, `truncated \xXX escape`)
			if err != nil {
				return "", err
			}
			b.WriteByte(byte(r))
			i = ni
		case 'u':
			r, ni, err := hexEscape(s, i, 4, `truncated \uXXXX escape`)
			if err != nil {
				return "", err
			}
			b.WriteRune(r)
			i = ni
		case 'U':
			r, ni, err := hexEscape(s, i, 8, `truncated \UXXXXXXXX escape`)
			if err != nil {
				return "", err
			}
			if r > unicode.MaxRune {
				return "", errors.New(`illegal Unicode character`)
			}
			b.WriteRune(r)
			i = ni
		case 'N':
			// \N{UNICODE NAME}
			if i >= len(s) || s[i] != '{' {
				return "", errors.New(`malformed \N character escape`)
			}
			end := strings.IndexByte(s[i:], '}')
			if end < 0 {
				return "", errors.New(`malformed \N character escape`)
			}
			r, ok := lookupRuneByName(s[i+1 : i+end])
			if !ok {
				return "", errors.New(`unknown Unicode character name`)
			}
			b.WriteRune(r)
			i += end + 1
		default:
			// 未知转义: 反斜杠与字符原样保留 (Python 行为, 带 DeprecationWarning)
			b.WriteByte('\\')
			b.WriteByte(e)
		}
	}
	return b.String(), nil
}

func hexEscape(s string, i, n int, errMsg string) (rune, int, error) {
	if i+n > len(s) {
		return 0, 0, errors.New(errMsg)
	}
	v, err := strconv.ParseUint(s[i:i+n], 16, 32)
	if err != nil {
		return 0, 0, errors.New(errMsg)
	}
	return rune(v), i + n, nil
}

// pyCharRepr 返回 src 首字符的 Python 风格 repr, 用于错误信息.
// Python 单字符 repr: 默认单引号; 字符本身是单引号时用双引号.
func pyCharRepr(src string) string {
	r, _ := utf8.DecodeRuneInString(src)
	switch r {
	case '\'':
		return `"'"`
	case '\\':
		return `'\\'`
	case '\n':
		return `'\n'`
	case '\r':
		return `'\r'`
	case '\t':
		return `'\t'`
	}
	if unicode.IsPrint(r) {
		return "'" + string(r) + "'"
	}
	// 非可打印字符: Python 用 \xXX / \uXXXX / \UXXXXXXXX
	q := strconv.QuoteRuneToASCII(r) // 形如 '\x00' / ' '
	return q
}

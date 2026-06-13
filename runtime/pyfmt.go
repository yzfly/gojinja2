package runtime

import (
	"fmt"
	"strconv"
	"strings"
)

// PyFormatPercent 实现 Python 的 % 字符串格式化 (format filter 与 % 运算符).
// 支持: %s %r %d %i %u %f %F %e %E %g %G %x %X %o %c %% 与
// 标志 - + 0 空格 #, 宽度, 精度, * 形式与 %(name)s 映射键.
func PyFormatPercent(format string, args any) string {
	// 参数规约: 单值视为单元素元组 (除非格式串使用映射键)
	var positional []any
	var mapping *Dict
	switch av := args.(type) {
	case Tuple:
		positional = av
	case *Dict:
		mapping = av
		positional = []any{av}
	default:
		positional = []any{args}
	}

	var b strings.Builder
	argIdx := 0
	nextArg := func() any {
		if argIdx >= len(positional) {
			RaiseType("not enough arguments for format string")
		}
		v := positional[argIdx]
		argIdx++
		return v
	}

	i := 0
	for i < len(format) {
		c := format[i]
		if c != '%' {
			b.WriteByte(c)
			i++
			continue
		}
		i++
		if i >= len(format) {
			RaiseType("incomplete format")
		}
		if format[i] == '%' {
			b.WriteByte('%')
			i++
			continue
		}
		// 映射键 %(name)s
		var value any
		hasValue := false
		if format[i] == '(' {
			end := strings.IndexByte(format[i:], ')')
			if end < 0 {
				RaiseType("incomplete format key")
			}
			key := format[i+1 : i+end]
			i += end + 1
			if mapping == nil {
				RaiseType("format requires a mapping")
			}
			v, ok := mapping.Get(key)
			if !ok {
				RaiseType("KeyError: " + PyStrRepr(key))
			}
			value = v
			hasValue = true
		}
		// 标志
		var flagMinus, flagPlus, flagZero, flagSpace, flagAlt bool
		for i < len(format) {
			switch format[i] {
			case '-':
				flagMinus = true
			case '+':
				flagPlus = true
			case '0':
				flagZero = true
			case ' ':
				flagSpace = true
			case '#':
				flagAlt = true
			default:
				goto flagsDone
			}
			i++
		}
	flagsDone:
		// 宽度
		width := -1
		if i < len(format) && format[i] == '*' {
			w, _ := asInt(nextArg())
			width = int(w)
			if width < 0 {
				flagMinus = true
				width = -width
			}
			i++
		} else {
			for i < len(format) && format[i] >= '0' && format[i] <= '9' {
				if width < 0 {
					width = 0
				}
				width = width*10 + int(format[i]-'0')
				i++
			}
		}
		// 精度
		prec := -1
		if i < len(format) && format[i] == '.' {
			i++
			prec = 0
			if i < len(format) && format[i] == '*' {
				p, _ := asInt(nextArg())
				prec = int(p)
				i++
			} else {
				for i < len(format) && format[i] >= '0' && format[i] <= '9' {
					prec = prec*10 + int(format[i]-'0')
					i++
				}
			}
		}
		// 长度修饰符 (忽略)
		for i < len(format) && (format[i] == 'h' || format[i] == 'l' || format[i] == 'L') {
			i++
		}
		if i >= len(format) {
			RaiseType("incomplete format")
		}
		verb := format[i]
		i++
		if !hasValue {
			value = nextArg()
		}
		b.WriteString(formatOne(verb, value, width, prec,
			flagMinus, flagPlus, flagZero, flagSpace, flagAlt))
	}
	if mapping == nil && argIdx < len(positional) {
		RaiseType("not all arguments converted during string formatting")
	}
	return b.String()
}

func formatOne(verb byte, value any, width, prec int,
	minus, plus, zero, space, alt bool) string {
	var s string
	numeric := false
	negative := false

	switch verb {
	case 's':
		s = Str(value)
		if prec >= 0 {
			r := []rune(s)
			if len(r) > prec {
				s = string(r[:prec])
			}
		}
	case 'r':
		s = Repr(value)
		if prec >= 0 {
			r := []rune(s)
			if len(r) > prec {
				s = string(r[:prec])
			}
		}
	case 'c':
		switch tv := value.(type) {
		case string:
			s = tv
		case Markup:
			s = string(tv)
		default:
			n, ok := asInt(value)
			if !ok {
				RaiseType("%c requires int or char")
			}
			s = string(rune(n))
		}
	case 'd', 'i', 'u':
		n := toIntForFmt(value, verb)
		numeric = true
		negative = n < 0
		if negative {
			s = strconv.FormatInt(-n, 10)
		} else {
			s = strconv.FormatInt(n, 10)
		}
	case 'x', 'X', 'o':
		n := toIntForFmt(value, verb)
		numeric = true
		negative = n < 0
		abs := n
		if negative {
			abs = -n
		}
		base := 16
		if verb == 'o' {
			base = 8
		}
		s = strconv.FormatInt(abs, base)
		if verb == 'X' {
			s = strings.ToUpper(s)
		}
		if alt {
			switch verb {
			case 'x':
				s = "0x" + s
			case 'X':
				s = "0X" + s
			case 'o':
				s = "0o" + s
			}
		}
	case 'f', 'F', 'e', 'E', 'g', 'G':
		f := toFloatForFmt(value, verb)
		numeric = true
		negative = f < 0
		if negative {
			f = -f
		}
		p := prec
		if p < 0 {
			p = 6
		}
		switch verb {
		case 'f', 'F':
			s = strconv.FormatFloat(f, 'f', p, 64)
		case 'e', 'E':
			s = strconv.FormatFloat(f, byte(verb), p, 64)
		case 'g', 'G':
			if p == 0 {
				p = 1
			}
			s = strconv.FormatFloat(f, byte(verb), -1, 64)
			s = pyGFormat(f, p, verb == 'G')
		}
	default:
		RaiseType(fmt.Sprintf("unsupported format character '%c'", verb))
	}

	sign := ""
	if numeric {
		switch {
		case negative:
			sign = "-"
		case plus:
			sign = "+"
		case space:
			sign = " "
		}
	}

	body := sign + s
	if width > len(body) {
		pad := width - len(body)
		switch {
		case minus:
			body += strings.Repeat(" ", pad)
		case zero && numeric:
			body = sign + strings.Repeat("0", pad) + s
		default:
			body = strings.Repeat(" ", pad) + body
		}
	}
	return body
}

func toIntForFmt(v any, verb byte) int64 {
	if n, ok := asInt(v); ok {
		return n
	}
	if f, ok := v.(float64); ok {
		return int64(f)
	}
	RaiseType(fmt.Sprintf("%%%c format: a real number is required, not %s", verb, PyTypeName(v)))
	return 0
}

func toFloatForFmt(v any, verb byte) float64 {
	if _, f, _, ok := asNumber(v); ok {
		return f
	}
	RaiseType(fmt.Sprintf("must be real number, not %s", PyTypeName(v)))
	return 0
}

// pyGFormat 复刻 %g: 精度内有效数字, 指数在 [-4, prec) 之外用科学计数.
func pyGFormat(f float64, prec int, upper bool) string {
	s := strconv.FormatFloat(f, 'g', prec, 64)
	// Go 的 'g' 与 Python 在大多数情况一致; 指数格式 Go=e+06 Python=e+06 一致
	if upper {
		s = strings.ToUpper(s)
	}
	return s
}

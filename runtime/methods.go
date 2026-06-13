package runtime

import (
	"strings"
)

// 本文件实现 str / dict / list 的 Python 内建方法子集.
// 这些方法通过 getattr 暴露为 BoundMethod, 是 "Python 对象模型" 的一部分.

func arg(args []any, i int, def any) any {
	if i < len(args) {
		return args[i]
	}
	return def
}

func strArg(v any, what string) string {
	switch tv := v.(type) {
	case string:
		return tv
	case Markup:
		return string(tv)
	}
	RaiseType(what + " must be str, not " + PyTypeName(v))
	return ""
}

func intArg(v any, what string) int64 {
	if i, ok := asInt(v); ok {
		return i
	}
	RaiseType(what + " must be int, not " + PyTypeName(v))
	return 0
}

func method(name string, fn func(args []any, kwargs *Dict) any) (any, bool) {
	return &BoundMethod{Name: name, Fn: fn}, true
}

// strMethod 返回字符串的内建方法.
func strMethod(s string, name string) (any, bool) {
	switch name {
	case "upper":
		return method(name, func(a []any, k *Dict) any { return strings.ToUpper(s) })
	case "lower":
		return method(name, func(a []any, k *Dict) any { return strings.ToLower(s) })
	case "title":
		return method(name, func(a []any, k *Dict) any { return pyTitle(s) })
	case "capitalize":
		return method(name, func(a []any, k *Dict) any { return pyCapitalize(s) })
	case "strip":
		return method(name, func(a []any, k *Dict) any { return pyStrip(s, arg(a, 0, nil), true, true) })
	case "lstrip":
		return method(name, func(a []any, k *Dict) any { return pyStrip(s, arg(a, 0, nil), true, false) })
	case "rstrip":
		return method(name, func(a []any, k *Dict) any { return pyStrip(s, arg(a, 0, nil), false, true) })
	case "split":
		return method(name, func(a []any, k *Dict) any {
			return pySplit(s, arg(a, 0, nil), int(intArg(arg(a, 1, int64(-1)), "maxsplit")))
		})
	case "rsplit":
		return method(name, func(a []any, k *Dict) any {
			return pyRSplit(s, arg(a, 0, nil), int(intArg(arg(a, 1, int64(-1)), "maxsplit")))
		})
	case "splitlines":
		return method(name, func(a []any, k *Dict) any { return pySplitlines(s, Truth(arg(a, 0, false))) })
	case "replace":
		return method(name, func(a []any, k *Dict) any {
			old := strArg(arg(a, 0, ""), "replace old")
			new_ := strArg(arg(a, 1, ""), "replace new")
			n := -1
			if len(a) > 2 {
				n = int(intArg(a[2], "count"))
			}
			return strings.Replace(s, old, new_, n)
		})
	case "startswith":
		return method(name, func(a []any, k *Dict) any {
			return strings.HasPrefix(s, strArg(arg(a, 0, ""), "prefix"))
		})
	case "endswith":
		return method(name, func(a []any, k *Dict) any {
			return strings.HasSuffix(s, strArg(arg(a, 0, ""), "suffix"))
		})
	case "join":
		return method(name, func(a []any, k *Dict) any {
			items := Iterate(arg(a, 0, []any{}))
			parts := make([]string, len(items))
			for i, item := range items {
				parts[i] = strArg(item, "sequence item")
			}
			return strings.Join(parts, s)
		})
	case "find":
		return method(name, func(a []any, k *Dict) any {
			idx := strings.Index(s, strArg(arg(a, 0, ""), "sub"))
			if idx < 0 {
				return int64(-1)
			}
			return int64(len([]rune(s[:idx])))
		})
	case "count":
		return method(name, func(a []any, k *Dict) any {
			return int64(strings.Count(s, strArg(arg(a, 0, ""), "sub")))
		})
	case "format":
		return method(name, func(a []any, k *Dict) any { return pyStrFormat(s, a, k) })
	case "encode":
		return method(name, func(a []any, k *Dict) any { return s })
	case "islower":
		return method(name, func(a []any, k *Dict) any { return s != "" && s == strings.ToLower(s) && s != strings.ToUpper(s) })
	case "isupper":
		return method(name, func(a []any, k *Dict) any { return s != "" && s == strings.ToUpper(s) && s != strings.ToLower(s) })
	case "zfill":
		return method(name, func(a []any, k *Dict) any {
			w := int(intArg(arg(a, 0, int64(0)), "width"))
			if len(s) >= w {
				return s
			}
			pad := strings.Repeat("0", w-len(s))
			if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
				return s[:1] + pad + s[1:]
			}
			return pad + s
		})
	}
	return nil, false
}

// pyTitle 复刻 str.title.
func pyTitle(s string) string {
	var b strings.Builder
	prevCased := false
	for _, r := range s {
		isCased := isAlphaRune(r)
		if isCased && !prevCased {
			b.WriteString(strings.ToUpper(string(r)))
		} else if isCased {
			b.WriteString(strings.ToLower(string(r)))
		} else {
			b.WriteRune(r)
		}
		prevCased = isCased
	}
	return b.String()
}

func isAlphaRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r > 127 && isLetterish(r)
}

func isLetterish(r rune) bool {
	s := string(r)
	return strings.ToUpper(s) != strings.ToLower(s)
}

func pyCapitalize(s string) string {
	runes := []rune(s)
	if len(runes) == 0 {
		return s
	}
	return strings.ToUpper(string(runes[0])) + strings.ToLower(string(runes[1:]))
}

func pyStrip(s string, chars any, left, right bool) string {
	cut := func(r rune) bool { return isSpaceRuneRT(r) }
	if chars != nil {
		set := strArg(chars, "chars")
		cut = func(r rune) bool { return strings.ContainsRune(set, r) }
	}
	if left {
		s = strings.TrimLeftFunc(s, cut)
	}
	if right {
		s = strings.TrimRightFunc(s, cut)
	}
	return s
}

func isSpaceRuneRT(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f':
		return true
	}
	return r == 0x85 || r == 0xa0 || (r >= 0x2000 && r <= 0x200a) ||
		r == 0x1680 || r == 0x2028 || r == 0x2029 || r == 0x202f ||
		r == 0x205f || r == 0x3000 || (r >= 0x1c && r <= 0x1f)
}

// pySplit 复刻 str.split: sep=None 时按连续空白切并忽略首尾空白.
func pySplit(s string, sep any, maxsplit int) []any {
	if sep == nil {
		return whitespaceSplit(s, maxsplit)
	}
	sepStr := strArg(sep, "sep")
	if sepStr == "" {
		RaiseType("empty separator")
	}
	var parts []string
	if maxsplit < 0 {
		parts = strings.Split(s, sepStr)
	} else {
		parts = strings.SplitN(s, sepStr, maxsplit+1)
	}
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}

func pyRSplit(s string, sep any, maxsplit int) []any {
	if sep == nil || maxsplit < 0 {
		return pySplit(s, sep, maxsplit)
	}
	sepStr := strArg(sep, "sep")
	var parts []string
	rest := s
	for maxsplit > 0 {
		idx := strings.LastIndex(rest, sepStr)
		if idx < 0 {
			break
		}
		parts = append([]string{rest[idx+len(sepStr):]}, parts...)
		rest = rest[:idx]
		maxsplit--
	}
	parts = append([]string{rest}, parts...)
	out := make([]any, len(parts))
	for i, p := range parts {
		out[i] = p
	}
	return out
}

func whitespaceSplit(s string, maxsplit int) []any {
	var out []any
	field := strings.Builder{}
	flush := func() {
		if field.Len() > 0 {
			out = append(out, field.String())
			field.Reset()
		}
	}
	rest := []rune(s)
	for i := 0; i < len(rest); i++ {
		r := rest[i]
		if isSpaceRuneRT(r) {
			flush()
			continue
		}
		if maxsplit >= 0 && len(out) == maxsplit && field.Len() == 0 {
			out = append(out, strings.TrimLeftFunc(string(rest[i:]), isSpaceRuneRT))
			return out
		}
		field.WriteRune(r)
	}
	flush()
	if out == nil {
		out = []any{}
	}
	return out
}

func pySplitlines(s string, keepends bool) []any {
	var out []any
	start := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\n' || c == '\r' {
			end := i
			nl := 1
			if c == '\r' && i+1 < len(s) && s[i+1] == '\n' {
				nl = 2
			}
			if keepends {
				out = append(out, s[start:end+nl])
			} else {
				out = append(out, s[start:end])
			}
			i += nl - 1
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	if out == nil {
		out = []any{}
	}
	return out
}

// pyStrFormat 实现 str.format 的常用子集: {} {0} {name} {a.b} {a[b]} 与 !r/!s.
func pyStrFormat(format string, args []any, kwargs *Dict) string {
	var b strings.Builder
	auto := 0
	for i := 0; i < len(format); i++ {
		c := format[i]
		if c == '{' {
			if i+1 < len(format) && format[i+1] == '{' {
				b.WriteByte('{')
				i++
				continue
			}
			end := strings.IndexByte(format[i:], '}')
			if end < 0 {
				RaiseType("Single '{' encountered in format string")
			}
			spec := format[i+1 : i+end]
			i += end
			b.WriteString(formatField(spec, args, kwargs, &auto))
			continue
		}
		if c == '}' {
			if i+1 < len(format) && format[i+1] == '}' {
				b.WriteByte('}')
				i++
				continue
			}
			RaiseType("Single '}' encountered in format string")
		}
		b.WriteByte(c)
	}
	return b.String()
}

func formatField(spec string, args []any, kwargs *Dict, auto *int) string {
	conv := ""
	if idx := strings.LastIndexByte(spec, '!'); idx >= 0 && !strings.Contains(spec[idx:], "[") {
		conv = spec[idx+1:]
		spec = spec[:idx]
	}
	var v any
	name := spec
	if name == "" {
		if *auto >= len(args) {
			RaiseType("Replacement index out of range for positional args tuple")
		}
		v = args[*auto]
		*auto++
	} else {
		// 解析 a.b / a[b] 链
		head := name
		var restOps string
		if dot := strings.IndexAny(name, ".["); dot >= 0 {
			head, restOps = name[:dot], name[dot:]
		}
		if n, ok := asIntStr(head); ok {
			if int(n) >= len(args) {
				RaiseType("Replacement index out of range for positional args tuple")
			}
			v = args[n]
		} else {
			var has bool
			if kwargs != nil {
				v, has = kwargs.Get(head)
			}
			if !has {
				RaiseType("KeyError: " + PyStrRepr(head))
			}
		}
		for restOps != "" {
			if restOps[0] == '.' {
				rest := restOps[1:]
				end := strings.IndexAny(rest, ".[")
				attr := rest
				if end >= 0 {
					attr, restOps = rest[:end], rest[end:]
				} else {
					restOps = ""
				}
				v = GetAttr(NewUndefinedFactory(UndefinedDefault), v, attr)
			} else if restOps[0] == '[' {
				end := strings.IndexByte(restOps, ']')
				key := restOps[1:end]
				restOps = restOps[end+1:]
				var kv any = key
				if n, ok := asIntStr(key); ok {
					kv = n
				}
				v = GetItem(NewUndefinedFactory(UndefinedDefault), v, kv)
			}
		}
	}
	switch conv {
	case "r":
		return Repr(v)
	}
	return Str(v)
}

func asIntStr(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}

// dictMethod 返回 dict 的内建方法.
func dictMethod(d *Dict, name string) (any, bool) {
	switch name {
	case "get":
		return method(name, func(a []any, k *Dict) any {
			if v, ok := d.Get(arg(a, 0, nil)); ok {
				return v
			}
			return arg(a, 1, nil)
		})
	case "keys":
		return method(name, func(a []any, k *Dict) any { return d.Keys() })
	case "values":
		return method(name, func(a []any, k *Dict) any { return d.Values() })
	case "items":
		return method(name, func(a []any, k *Dict) any {
			out := make([]any, d.Len())
			for i, it := range d.Items() {
				out[i] = Tuple{it.Key, it.Value}
			}
			return out
		})
	case "pop":
		return method(name, func(a []any, k *Dict) any {
			key := arg(a, 0, nil)
			if v, ok := d.Get(key); ok {
				d.Delete(key)
				return v
			}
			if len(a) > 1 {
				return a[1]
			}
			RaiseType("KeyError: " + Repr(key))
			return nil
		})
	case "copy":
		return method(name, func(a []any, k *Dict) any { return d.Copy() })
	case "update":
		return method(name, func(a []any, k *Dict) any {
			if len(a) > 0 {
				if od, ok := a[0].(*Dict); ok {
					for _, it := range od.Items() {
						d.Set(it.Key, it.Value)
					}
				}
			}
			if k != nil {
				for _, it := range k.Items() {
					d.Set(it.Key, it.Value)
				}
			}
			return nil
		})
	case "setdefault":
		return method(name, func(a []any, k *Dict) any {
			key := arg(a, 0, nil)
			if v, ok := d.Get(key); ok {
				return v
			}
			def := arg(a, 1, nil)
			d.Set(key, def)
			return def
		})
	}
	return nil, false
}

// listMethod 返回 list 的内建方法.
// 注意: Go 切片按值传递, append 等变更方法需要 *[]any 才能生效;
// 模板中的 list 都以 []any 存放在容器/变量里, 这里实现为收集器模式:
// 通过 listRef 指针包装由 interp 在 do 语句等场景下使用.
func listMethod(l []any, name string) (any, bool) {
	switch name {
	case "count":
		return method(name, func(a []any, k *Dict) any {
			n := int64(0)
			for _, v := range l {
				if Equal(v, arg(a, 0, nil)) {
					n++
				}
			}
			return n
		})
	case "index":
		return method(name, func(a []any, k *Dict) any {
			for i, v := range l {
				if Equal(v, arg(a, 0, nil)) {
					return int64(i)
				}
			}
			RaiseType(Repr(arg(a, 0, nil)) + " is not in list")
			return nil
		})
	}
	return nil, false
}

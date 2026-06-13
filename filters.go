package gojinja2

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
	"github.com/yzfly/gojinja2/runtime"
)

// 本文件完整移植 jinja2/filters.py 的 54 个内置 filter.

func failFilterArg(msg string) {
	panic(&exceptions.FilterArgumentError{Message: msg})
}

// attrGetter 对应 make_attrgetter: 点路径逐段用 getitem 语义查找,
// 整数段按下标. default 在结果为 Undefined 时逐段替换.
func attrGetter(env *Environment, attribute any, lowercase bool, def any) func(any) any {
	parts := prepareAttributeParts(attribute)
	undef := env.undefinedFactory()
	return func(item any) any {
		for _, part := range parts {
			item = runtime.GetItem(undef, item, part)
			if def != nil {
				if _, isU := item.(*runtime.Undefined); isU {
					item = def
				}
			}
		}
		if lowercase {
			item = ignoreCase(item)
		}
		return item
	}
}

// multiAttrGetter 对应 make_multi_attrgetter ("a.b,c" 逗号分隔多键).
func multiAttrGetter(env *Environment, attribute any, lowercase bool) func(any) []any {
	var specs [][]any
	if s, ok := attribute.(string); ok {
		for _, part := range strings.Split(s, ",") {
			specs = append(specs, prepareAttributeParts(part))
		}
	} else {
		specs = append(specs, prepareAttributeParts(attribute))
	}
	undef := env.undefinedFactory()
	return func(item any) []any {
		out := make([]any, len(specs))
		for i, parts := range specs {
			cur := item
			for _, part := range parts {
				cur = runtime.GetItem(undef, cur, part)
			}
			if lowercase {
				cur = ignoreCase(cur)
			}
			out[i] = cur
		}
		return out
	}
}

func prepareAttributeParts(attribute any) []any {
	switch av := attribute.(type) {
	case nil:
		return nil
	case int64:
		return []any{av}
	case string:
		parts := strings.Split(av, ".")
		out := make([]any, len(parts))
		for i, p := range parts {
			if n, ok := intFromStr(p); ok {
				out[i] = n
			} else {
				out[i] = p
			}
		}
		return out
	}
	return []any{attribute}
}

func ignoreCase(v any) any {
	switch sv := v.(type) {
	case string:
		return strings.ToLower(sv)
	case runtime.Markup:
		return strings.ToLower(string(sv))
	}
	return v
}

// pyCmpLess 是排序比较 (runtime 富比较语义).
func pyCmpLess(a, b any) bool { return runtime.CompareOp("lt", a, b) }

func runesLen(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// markupAware 包装字符串处理: 输入是 Markup 则输出保持 Markup.
func markupAware(v any, fn func(string) string) any {
	if m, ok := v.(runtime.Markup); ok {
		return runtime.Markup(fn(string(m)))
	}
	return fn(runtime.Str(softStr(v)))
}

var wordBeginningSplitRe = regexp.MustCompile(`([-\s({\[<]+)`)
var wordRe = regexp.MustCompile(`[\p{L}\p{N}_]+`)

func registerAllFilters(m map[string]*runtime.Func) {
	reg := func(name string, pass runtime.PassArg,
		fn func(args []any, kwargs *runtime.Dict) any) {
		m[name] = &runtime.Func{Name: name, Pass: pass, Fn: fn}
	}

	// ---- 数值 ----
	reg("abs", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		switch v := a[0].(type) {
		case int64:
			if v < 0 {
				return -v
			}
			return v
		case float64:
			return math.Abs(v)
		case bool:
			if v {
				return int64(1)
			}
			return int64(0)
		}
		runtime.RaiseType("bad operand type for abs(): " + runtime.PyStrRepr(runtime.PyTypeName(a[0])))
		return nil
	})
	reg("int", runtime.PassNone, func(a []any, k *runtime.Dict) any { return filterInt(a, k) })
	reg("float", runtime.PassNone, func(a []any, k *runtime.Dict) any { return filterFloat(a, k) })
	reg("round", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		var value float64
		switch v := a[0].(type) {
		case int64:
			value = float64(v)
		case float64:
			value = v
		case bool:
			if v {
				value = 1
			}
		default:
			runtime.RaiseType("must be real number, not " + runtime.PyTypeName(a[0]))
		}
		precision := int(toInt(argOr(a, k, 1, "precision", int64(0))))
		method := runtime.Str(argOr(a, k, 2, "method", "common"))
		factor := math.Pow(10, float64(precision))
		switch method {
		case "common":
			return pyRound(value, precision)
		case "ceil":
			return math.Ceil(value*factor) / factor
		case "floor":
			return math.Floor(value*factor) / factor
		}
		failFilterArg("method must be common, ceil or floor")
		return nil
	})
	reg("sum", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		items := runtime.Iterate(a[1])
		attribute := argOr(a, k, 2, "attribute", nil)
		var acc any = argOr(a, k, 3, "start", int64(0))
		if attribute != nil {
			get := attrGetter(env, attribute, false, nil)
			for _, it := range items {
				acc = runtime.Add(acc, get(it))
			}
		} else {
			for _, it := range items {
				acc = runtime.Add(acc, it)
			}
		}
		return acc
	})
	reg("filesizeformat", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		var bytes float64
		switch v := a[0].(type) {
		case int64:
			bytes = float64(v)
		case float64:
			bytes = v
		case string:
			f, _ := parsePyFloat(v)
			bytes = f
		}
		binary := runtime.Truth(argOr(a, k, 1, "binary", false))
		base := 1000.0
		prefixes := []string{"kB", "MB", "GB", "TB", "PB", "EB", "ZB", "YB"}
		if binary {
			base = 1024.0
			prefixes = []string{"KiB", "MiB", "GiB", "TiB", "PiB", "EiB", "ZiB", "YiB"}
		}
		if bytes == 1 {
			return "1 Byte"
		}
		if bytes < base {
			return fmt.Sprintf("%d Bytes", int64(bytes))
		}
		unit := base
		for i, prefix := range prefixes {
			unit = math.Pow(base, float64(i+2))
			if bytes < unit {
				return fmt.Sprintf("%.1f %s", base*bytes/unit, prefix)
			}
		}
		return fmt.Sprintf("%.1f %s", base*bytes/unit, prefixes[len(prefixes)-1])
	})

	// ---- 字符串 ----
	reg("upper", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		return strings.ToUpper(runtime.Str(softStr(a[0])))
	})
	reg("lower", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		return strings.ToLower(runtime.Str(softStr(a[0])))
	})
	reg("capitalize", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		s := runtime.Str(softStr(a[0]))
		r := []rune(s)
		if len(r) == 0 {
			return s
		}
		return strings.ToUpper(string(r[0])) + strings.ToLower(string(r[1:]))
	})
	reg("title", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		s := runtime.Str(softStr(a[0]))
		var b strings.Builder
		for _, item := range splitKeep(wordBeginningSplitRe, s) {
			if item == "" {
				continue
			}
			r := []rune(item)
			b.WriteString(strings.ToUpper(string(r[0])))
			b.WriteString(strings.ToLower(string(r[1:])))
		}
		return b.String()
	})
	reg("trim", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		chars := argOr(a, k, 1, "chars", nil)
		return markupAware(a[0], func(s string) string {
			if chars == nil {
				return strings.TrimFunc(s, pyIsSpace)
			}
			return strings.Trim(s, runtime.Str(chars))
		})
	})
	reg("striptags", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		s := runtime.Str(softStr(a[0]))
		if h, ok := a[0].(runtime.HTMLer); ok {
			s = h.HTML()
		}
		return markupStriptags(s)
	})
	reg("center", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		width := int(toInt(argOr(a, k, 1, "width", int64(80))))
		s := runtime.Str(softStr(a[0]))
		n := runesLen(s)
		if n >= width {
			return s
		}
		// Python str.center: 左边 pad = (width-n)//2 + (width-n)%2*0... 实际:
		// marg = width - n; left = marg // 2 + (marg & width & 1)
		marg := width - n
		left := marg/2 + (marg & width & 1)
		return strings.Repeat(" ", left) + s + strings.Repeat(" ", marg-left)
	})
	reg("indent", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		widthArg := argOr(a, k, 1, "width", int64(4))
		first := runtime.Truth(argOr(a, k, 2, "first", false))
		blank := runtime.Truth(argOr(a, k, 3, "blank", false))
		var indention string
		if ws, ok := widthArg.(string); ok {
			indention = ws
		} else if wm, ok := widthArg.(runtime.Markup); ok {
			indention = string(wm)
		} else {
			indention = strings.Repeat(" ", int(toInt(widthArg)))
		}
		return markupAware(a[0], func(s string) string {
			s += "\n"
			lines := pySplitlinesStr(s)
			var rv string
			if blank {
				rv = strings.Join(lines, "\n"+indention)
			} else {
				rv = lines[0]
				if len(lines) > 1 {
					var parts []string
					for _, line := range lines[1:] {
						if line != "" {
							parts = append(parts, indention+line)
						} else {
							parts = append(parts, line)
						}
					}
					rv += "\n" + strings.Join(parts, "\n")
				}
			}
			if first {
				rv = indention + rv
			}
			return rv
		})
	})
	reg("truncate", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		s := runtime.Str(softStr(a[1]))
		length := int(toInt(argOr(a, k, 2, "length", int64(255))))
		killwords := runtime.Truth(argOr(a, k, 3, "killwords", false))
		end := runtime.Str(argOr(a, k, 4, "end", "..."))
		leewayArg := argOr(a, k, 5, "leeway", nil)
		leeway := int(toInt(env.Policies["truncate.leeway"]))
		if leewayArg != nil {
			leeway = int(toInt(leewayArg))
		}
		runes := []rune(s)
		endLen := runesLen(end)
		if length < endLen {
			panic(&exceptions.TemplateRuntimeError{
				Message: fmt.Sprintf("expected length >= %d, got %d", endLen, length)})
		}
		if leeway < 0 {
			panic(&exceptions.TemplateRuntimeError{
				Message: fmt.Sprintf("expected leeway >= 0, got %d", leeway)})
		}
		if len(runes) <= length+leeway {
			return s
		}
		cut := string(runes[:length-endLen])
		if killwords {
			return cut + end
		}
		if idx := strings.LastIndex(cut, " "); idx >= 0 {
			cut = cut[:idx]
		}
		return cut + end
	})
	reg("wordwrap", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		s := runtime.Str(softStr(a[1]))
		width := int(toInt(argOr(a, k, 2, "width", int64(79))))
		breakLongWords := runtime.Truth(argOr(a, k, 3, "break_long_words", true))
		wrapstringArg := argOr(a, k, 4, "wrapstring", nil)
		breakOnHyphens := runtime.Truth(argOr(a, k, 5, "break_on_hyphens", true))
		wrapstring := env.NewlineSequence
		if wrapstringArg != nil {
			wrapstring = runtime.Str(wrapstringArg)
		}
		var out []string
		for _, line := range pySplitlinesStr(s) {
			out = append(out, strings.Join(
				textwrapWrap(line, width, breakLongWords, breakOnHyphens), wrapstring))
		}
		return strings.Join(out, wrapstring)
	})
	reg("wordcount", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		return int64(len(wordRe.FindAllString(runtime.Str(softStr(a[0])), -1)))
	})
	reg("replace", runtime.PassEvalContext, func(a []any, k *runtime.Dict) any {
		ec := a[0].(*evalCtxProxy)
		old := argOr(a, k, 2, "old", "")
		new_ := argOr(a, k, 3, "new", "")
		count := -1
		if c := argOr(a, k, 4, "count", nil); c != nil {
			count = int(toInt(c))
		}
		if !ec.autoescape {
			return strings.Replace(runtime.Str(softStr(a[1])),
				runtime.Str(softStr(old)), runtime.Str(softStr(new_)), count)
		}
		// autoescape: 全部转义后替换, 结果是 Markup
		s := string(runtime.Escape(a[1]))
		return runtime.Markup(strings.Replace(s,
			string(runtime.Escape(old)), string(runtime.Escape(new_)), count))
	})
	reg("format", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		if len(a) > 1 && k != nil && k.Len() > 0 {
			failFilterArg("can't handle positional and keyword arguments at the same time")
		}
		s := softStr(a[0])
		if k != nil && k.Len() > 0 {
			if m, ok := s.(runtime.Markup); ok {
				return runtime.Mod(m, k)
			}
			return runtime.Mod(runtime.Str(s), k)
		}
		args := runtime.Tuple(a[1:])
		if m, ok := s.(runtime.Markup); ok {
			return runtime.Mod(m, args)
		}
		return runtime.Mod(runtime.Str(s), args)
	})
	reg("string", runtime.PassNone, func(a []any, k *runtime.Dict) any { return softStr(a[0]) })
	reg("safe", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		return runtime.Markup(runtime.Str(a[0]))
	})
	escapeFn := func(a []any, k *runtime.Dict) any { return runtime.Escape(a[0]) }
	reg("e", runtime.PassNone, escapeFn)
	reg("escape", runtime.PassNone, escapeFn)
	reg("forceescape", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		return runtime.Escape(runtime.Str(softStr(a[0])))
	})
	reg("urlencode", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		switch v := a[0].(type) {
		case string:
			return urlQuote(v, false)
		case runtime.Markup:
			return urlQuote(string(v), false)
		case *runtime.Dict:
			var parts []string
			for _, it := range v.Items() {
				parts = append(parts, urlQuote(runtime.Str(it.Key), true)+"="+
					urlQuote(runtime.Str(it.Value), true))
			}
			return strings.Join(parts, "&")
		}
		// 键值对序列
		items, ok := tryIter(a[0])
		if !ok {
			return urlQuote(runtime.Str(a[0]), false)
		}
		var parts []string
		for _, pair := range items {
			kv := runtime.Iterate(pair)
			if len(kv) != 2 {
				runtime.RaiseType("expected (key, value) pairs")
			}
			parts = append(parts, urlQuote(runtime.Str(kv[0]), true)+"="+
				urlQuote(runtime.Str(kv[1]), true))
		}
		return strings.Join(parts, "&")
	})
	reg("urlize", runtime.PassEvalContext, func(a []any, k *runtime.Dict) any {
		ec := a[0].(*evalCtxProxy)
		value := runtime.Str(softStr(a[1]))
		var trimLimit = -1
		if v := argOr(a, k, 2, "trim_url_limit", nil); v != nil {
			trimLimit = int(toInt(v))
		}
		nofollow := runtime.Truth(argOr(a, k, 3, "nofollow", false))
		target := argOr(a, k, 4, "target", nil)
		relArg := argOr(a, k, 5, "rel", nil)
		extraSchemes := argOr(a, k, 6, "extra_schemes", nil)

		policies := ec.env.Policies
		relParts := map[string]bool{}
		if relArg != nil {
			for _, p := range strings.Fields(runtime.Str(relArg)) {
				relParts[p] = true
			}
		}
		if nofollow {
			relParts["nofollow"] = true
		}
		if pr, ok := policies["urlize.rel"].(string); ok && pr != "" {
			for _, p := range strings.Fields(pr) {
				relParts[p] = true
			}
		}
		var relList []string
		for p := range relParts {
			relList = append(relList, p)
		}
		sort.Strings(relList)
		rel := strings.Join(relList, " ")

		if target == nil {
			target = policies["urlize.target"]
		}
		var schemes []string
		if extraSchemes != nil {
			for _, sch := range runtime.Iterate(extraSchemes) {
				schemes = append(schemes, runtime.Str(sch))
			}
		} else if ps, ok := policies["urlize.extra_schemes"].([]string); ok {
			schemes = ps
		}
		var targetStr string
		if target != nil {
			targetStr = runtime.Str(target)
		}
		rv := urlizeImpl(value, trimLimit, rel, targetStr, schemes)
		if ec.autoescape {
			return runtime.Markup(rv)
		}
		return rv
	})

	// ---- 序列 ----
	reg("first", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		items := runtime.Iterate(a[1])
		if len(items) == 0 {
			return env.undefinedFactory()("No first item, sequence was empty.", runtime.Missing, nil)
		}
		return items[0]
	})
	reg("last", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		items := runtime.Iterate(a[1])
		if len(items) == 0 {
			return env.undefinedFactory()("No last item, sequence was empty.", runtime.Missing, nil)
		}
		return items[len(items)-1]
	})
	reg("length", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		return int64(runtime.Length(a[0]))
	})
	m["count"] = m["length"]
	reg("list", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		return runtime.ToList(a[0])
	})
	reg("reverse", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		switch v := a[0].(type) {
		case string, runtime.Markup:
			return markupAware(v, func(s string) string {
				runes := []rune(s)
				for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
					runes[i], runes[j] = runes[j], runes[i]
				}
				return string(runes)
			})
		}
		items := runtime.ToList(a[0])
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
		return items
	})
	reg("sort", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		items := runtime.ToList(a[1])
		reverse := runtime.Truth(argOr(a, k, 2, "reverse", false))
		caseSensitive := runtime.Truth(argOr(a, k, 3, "case_sensitive", false))
		attribute := argOr(a, k, 4, "attribute", nil)
		get := multiAttrGetter(env, attribute, !caseSensitive)
		sort.SliceStable(items, func(i, j int) bool {
			ki, kj := get(items[i]), get(items[j])
			if reverse {
				return seqLess(kj, ki)
			}
			return seqLess(ki, kj)
		})
		return items
	})
	reg("min", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		return minMax(a, k, true)
	})
	reg("max", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		return minMax(a, k, false)
	})
	reg("unique", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		caseSensitive := runtime.Truth(argOr(a, k, 2, "case_sensitive", false))
		attribute := argOr(a, k, 3, "attribute", nil)
		get := attrGetter(env, attribute, !caseSensitive, nil)
		seen := runtime.NewDict()
		var out []any
		for _, item := range runtime.Iterate(a[1]) {
			key := get(item)
			if !seen.Has(key) {
				seen.Set(key, true)
				out = append(out, item)
			}
		}
		if out == nil {
			out = []any{}
		}
		return out
	})
	reg("batch", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		items := runtime.Iterate(a[0])
		linecount := int(toInt(argOr(a, k, 1, "linecount", int64(1))))
		fill := argOr(a, k, 2, "fill_with", nil)
		var out []any
		var tmp []any
		for _, item := range items {
			if len(tmp) == linecount {
				out = append(out, tmp)
				tmp = nil
			}
			tmp = append(tmp, item)
		}
		if len(tmp) > 0 {
			if fill != nil && len(tmp) < linecount {
				for len(tmp) < linecount {
					tmp = append(tmp, fill)
				}
			}
			out = append(out, tmp)
		}
		if out == nil {
			out = []any{}
		}
		return out
	})
	reg("slice", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		seq := runtime.ToList(a[0])
		slices := int(toInt(argOr(a, k, 1, "slices", int64(1))))
		fill := argOr(a, k, 2, "fill_with", nil)
		length := len(seq)
		perSlice := length / slices
		withExtra := length % slices
		offset := 0
		var out []any
		for n := 0; n < slices; n++ {
			start := offset + n*perSlice
			if n < withExtra {
				offset++
			}
			end := offset + (n+1)*perSlice
			tmp := append([]any{}, seq[start:end]...)
			if fill != nil && n >= withExtra {
				tmp = append(tmp, fill)
			}
			out = append(out, tmp)
		}
		if out == nil {
			out = []any{}
		}
		return out
	})
	reg("join", runtime.PassEvalContext, func(a []any, k *runtime.Dict) any {
		ec := a[0].(*evalCtxProxy)
		sep := runtime.Str(argOr(a, k, 2, "d", ""))
		attribute := argOr(a, k, 3, "attribute", nil)
		items := runtime.Iterate(a[1])
		if attribute != nil {
			get := attrGetter(ec.env, attribute, false, nil)
			mapped := make([]any, len(items))
			for i, it := range items {
				mapped[i] = get(it)
			}
			items = mapped
		}
		if ec.autoescape {
			return joinMarkup(items, sep)
		}
		strs := make([]string, len(items))
		for i, it := range items {
			strs[i] = runtime.Str(it)
		}
		return strings.Join(strs, sep)
	})
	reg("random", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		items := runtime.Iterate(a[1])
		if len(items) == 0 {
			return env.undefinedFactory()("No random item, sequence was empty.", runtime.Missing, nil)
		}
		return items[pseudoRandomIndex(len(items))]
	})

	// ---- dict ----
	reg("dictsort", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		d, ok := a[0].(*runtime.Dict)
		if !ok {
			failFilterArg("cannot convert " + runtime.PyTypeName(a[0]) + " to dict")
		}
		caseSensitive := runtime.Truth(argOr(a, k, 1, "case_sensitive", false))
		by := runtime.Str(argOr(a, k, 2, "by", "key"))
		reverse := runtime.Truth(argOr(a, k, 3, "reverse", false))
		var pos int
		switch by {
		case "key":
			pos = 0
		case "value":
			pos = 1
		default:
			failFilterArg(`You can only sort by either "key" or "value"`)
		}
		items := d.Items()
		pairs := make([]any, len(items))
		for i, it := range items {
			pairs[i] = runtime.Tuple{it.Key, it.Value}
		}
		key := func(p any) any {
			v := p.(runtime.Tuple)[pos]
			if !caseSensitive {
				v = ignoreCase(v)
			}
			return v
		}
		sort.SliceStable(pairs, func(i, j int) bool {
			if reverse {
				return pyCmpLess(key(pairs[j]), key(pairs[i]))
			}
			return pyCmpLess(key(pairs[i]), key(pairs[j]))
		})
		return pairs
	})
	reg("items", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		switch v := a[0].(type) {
		case *runtime.Dict:
			out := make([]any, v.Len())
			for i, it := range v.Items() {
				out[i] = runtime.Tuple{it.Key, it.Value}
			}
			return out
		case *runtime.Undefined:
			return []any{}
		}
		runtime.RaiseType("can only get items from dict, got " +
			runtime.PyStrRepr(runtime.PyTypeName(a[0])))
		return nil
	})
	reg("xmlattr", runtime.PassEvalContext, func(a []any, k *runtime.Dict) any {
		ec := a[0].(*evalCtxProxy)
		d, ok := a[1].(*runtime.Dict)
		if !ok {
			failFilterArg("xmlattr filter requires a dict")
		}
		autospace := runtime.Truth(argOr(a, k, 2, "autospace", true))
		var parts []string
		for _, it := range d.Items() {
			if it.Value == nil {
				continue
			}
			if u, isU := it.Value.(*runtime.Undefined); isU {
				_ = u
				continue
			}
			key := runtime.Str(it.Key)
			if strings.ContainsAny(key, " /><=\t\n\f") {
				panic(&exceptions.TemplateRuntimeError{
					Message: fmt.Sprintf("Invalid character(s) in attribute name: %s",
						runtime.Repr(key))})
			}
			parts = append(parts, fmt.Sprintf(`%s="%s"`,
				runtime.Escape(key), runtime.Escape(it.Value)))
		}
		rv := strings.Join(parts, " ")
		if autospace && rv != "" {
			rv = " " + rv
		}
		if ec.autoescape {
			return runtime.Markup(rv)
		}
		return rv
	})

	// ---- 高阶 ----
	reg("attr", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		name := runtime.Str(argOr(a, k, 2, "name", ""))
		v := runtime.GetAttr(env.undefinedFactory(), a[1], name)
		// attr 过滤器不回退到 getitem: 如果属性来自 getitem 回退,
		// runtime.GetAttr 已合并; 对 dict 的键访问需排除
		if d, isDict := a[1].(*runtime.Dict); isDict {
			if _, fromKey := d.Get(name); fromKey {
				return env.undefinedFactory()("", a[1], name)
			}
		}
		return v
	})
	reg("map", runtime.PassContext, func(a []any, k *runtime.Dict) any {
		cp := a[0].(*contextProxy)
		items, ok := tryIter(a[1])
		if !ok || !runtime.Truth(a[1]) {
			return []any{}
		}
		fn := prepareMap(cp, a[2:], k)
		out := make([]any, len(items))
		for i, item := range items {
			out[i] = fn(item)
		}
		return out
	})
	selectReject := func(selectMode, lookupAttr bool) func(a []any, k *runtime.Dict) any {
		return func(a []any, k *runtime.Dict) any {
			cp := a[0].(*contextProxy)
			if !runtime.Truth(a[1]) {
				return []any{}
			}
			items := runtime.Iterate(a[1])
			fn := prepareSelectOrReject(cp, a[2:], k, selectMode, lookupAttr)
			var out []any
			for _, item := range items {
				if fn(item) {
					out = append(out, item)
				}
			}
			if out == nil {
				out = []any{}
			}
			return out
		}
	}
	reg("select", runtime.PassContext, selectReject(true, false))
	reg("reject", runtime.PassContext, selectReject(false, false))
	reg("selectattr", runtime.PassContext, selectReject(true, true))
	reg("rejectattr", runtime.PassContext, selectReject(false, true))
	reg("groupby", runtime.PassEnvironment, func(a []any, k *runtime.Dict) any {
		env := a[0].(*Environment)
		attribute := argOr(a, k, 2, "attribute", nil)
		def := argOr(a, k, 3, "default", nil)
		caseSensitive := runtime.Truth(argOr(a, k, 4, "case_sensitive", false))
		get := attrGetter(env, attribute, !caseSensitive, def)
		items := runtime.ToList(a[1])
		sort.SliceStable(items, func(i, j int) bool {
			return pyCmpLess(get(items[i]), get(items[j]))
		})
		var out []any
		var curKey any = runtime.Missing
		var group []any
		flush := func() {
			if group != nil {
				out = append(out, runtime.Tuple{curKey, group})
			}
		}
		for _, item := range items {
			key := get(item)
			if curKey == runtime.Missing || !runtime.Equal(key, curKey) {
				flush()
				curKey = key
				group = nil
			}
			group = append(group, item)
		}
		flush()
		if !caseSensitive {
			// 用组内第一个元素的原始大小写作为 grouper
			outGet := attrGetter(env, attribute, false, def)
			for i, g := range out {
				t := g.(runtime.Tuple)
				values := t[1].([]any)
				out[i] = runtime.Tuple{outGet(values[0]), values}
			}
		}
		if out == nil {
			out = []any{}
		}
		return out
	})
	reg("default", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		def := argOr(a, k, 1, "default_value", "")
		boolean := runtime.Truth(argOr(a, k, 2, "boolean", false))
		if _, isU := a[0].(*runtime.Undefined); isU {
			return def
		}
		if boolean && !runtime.Truth(a[0]) {
			return def
		}
		return a[0]
	})
	m["d"] = m["default"]
	reg("pprint", runtime.PassNone, func(a []any, k *runtime.Dict) any {
		return runtime.Repr(a[0])
	})
	reg("tojson", runtime.PassEvalContext, func(a []any, k *runtime.Dict) any {
		indent := -1
		if v := argOr(a, k, 2, "indent", nil); v != nil {
			indent = int(toInt(v))
		}
		s := pyJSONDumps(a[1], indent, true)
		s = strings.ReplaceAll(s, "<", "\\u003c")
		s = strings.ReplaceAll(s, ">", "\\u003e")
		s = strings.ReplaceAll(s, "&", "\\u0026")
		s = strings.ReplaceAll(s, "'", "\\u0027")
		return runtime.Markup(s)
	})
}

func toInt(v any) int64 {
	switch tv := v.(type) {
	case int64:
		return tv
	case float64:
		return int64(tv)
	case bool:
		if tv {
			return 1
		}
	case *runtime.Undefined:
		tv.Fail()
	}
	if n, ok := v.(int); ok {
		return int64(n)
	}
	return 0
}

// pyRound 实现 Python 银行家舍入 (round-half-even).
func pyRound(value float64, precision int) float64 {
	factor := math.Pow(10, float64(precision))
	scaled := value * factor
	floor := math.Floor(scaled)
	diff := scaled - floor
	switch {
	case diff > 0.5:
		floor++
	case diff == 0.5:
		if math.Mod(floor, 2) != 0 {
			floor++
		}
	}
	return floor / factor
}

func minMax(a []any, k *runtime.Dict, isMin bool) any {
	env := a[0].(*Environment)
	items := runtime.Iterate(a[1])
	caseSensitive := runtime.Truth(argOr(a, k, 2, "case_sensitive", false))
	attribute := argOr(a, k, 3, "attribute", nil)
	if len(items) == 0 {
		which := "max"
		if isMin {
			which = "min"
		}
		return env.undefinedFactory()("No aggregated item, sequence was empty.", runtime.Missing, which)
	}
	get := attrGetter(env, attribute, !caseSensitive, nil)
	best := items[0]
	bestKey := get(best)
	for _, item := range items[1:] {
		key := get(item)
		if (isMin && pyCmpLess(key, bestKey)) || (!isMin && pyCmpLess(bestKey, key)) {
			best, bestKey = item, key
		}
	}
	return best
}

func seqLess(a, b []any) bool {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if runtime.Equal(a[i], b[i]) {
			continue
		}
		return pyCmpLess(a[i], b[i])
	}
	return len(a) < len(b)
}

func prepareMap(cp *contextProxy, args []any, kwargs *runtime.Dict) func(any) any {
	env := cp.Environment()
	if len(args) == 0 && kwargs != nil && kwargs.Has("attribute") {
		attribute, _ := kwargs.Get("attribute")
		def, _ := kwargs.Get("default")
		for _, key := range kwargs.Keys() {
			ks := runtime.Str(key)
			if ks != "attribute" && ks != "default" {
				failFilterArg("Unexpected keyword argument " + runtime.PyStrRepr(ks))
			}
		}
		return attrGetter(env, attribute, false, def)
	}
	if len(args) == 0 {
		failFilterArg("map requires a filter argument")
	}
	name := runtime.Str(args[0])
	rest := args[1:]
	fn, ok := env.Filters[name]
	if !ok {
		panic(&exceptions.TemplateRuntimeError{
			Message: "No filter named " + runtime.PyStrRepr(name) + "."})
	}
	return func(item any) any {
		all := append([]any{item}, rest...)
		all = cp.state.injectPassArg(fn.Pass, all, cp.frame)
		return fn.Fn(all, kwargs)
	}
}

func prepareSelectOrReject(cp *contextProxy, args []any, kwargs *runtime.Dict,
	selectMode, lookupAttr bool) func(any) bool {
	env := cp.Environment()
	trans := func(x any) any { return x }
	off := 0
	if lookupAttr {
		if len(args) == 0 {
			failFilterArg("Missing parameter for attribute name")
		}
		trans = attrGetter(env, args[0], false, nil)
		off = 1
	}
	var test func(any) bool
	if len(args) > off {
		name := runtime.Str(args[off])
		rest := args[off+1:]
		fn, ok := env.Tests[name]
		if !ok {
			panic(&exceptions.TemplateRuntimeError{
				Message: "No test named " + runtime.PyStrRepr(name) + "."})
		}
		test = func(item any) bool {
			all := append([]any{item}, rest...)
			all = cp.state.injectPassArg(fn.Pass, all, cp.frame)
			return runtime.Truth(fn.Fn(all, kwargs))
		}
	} else {
		test = runtime.Truth
	}
	return func(item any) bool {
		r := test(trans(item))
		if selectMode {
			return r
		}
		return !r
	}
}

// pseudoRandomIndex 可注入以便测试 (默认基于时间的弱随机即可).
var pseudoRandomIndex = func(n int) int {
	// 不引入 math/rand 的全局状态; 用地址熵 + 计数即可满足 "随机" 语义
	randState = randState*6364136223846793005 + 1442695040888963407
	v := int((randState >> 33) % uint64(n))
	if v < 0 {
		v += n
	}
	return v
}

var randState uint64 = 0x9E3779B97F4A7C15

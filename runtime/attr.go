package runtime

import (
	"reflect"
	"strings"
	"unicode"
	"unicode/utf8"
)

// UndefinedCtor 由调用方 (Environment) 注入, attr.go 在访问失败时
// 用它构造 Undefined.
type UndefinedCtor = UndefinedFactory

// GetAttr 对应 environment.getattr: 先属性后下标.
// attribute 必须是字符串.
func GetAttr(undef UndefinedCtor, obj any, attribute string) any {
	if v, ok := tryAttr(obj, attribute); ok {
		return v
	}
	if v, ok := tryItem(obj, attribute); ok {
		return v
	}
	if u, ok := obj.(*Undefined); ok {
		return u.GetAttr(attribute)
	}
	return undef("", obj, attribute)
}

// GetItem 对应 environment.getitem: 先下标后属性.
func GetItem(undef UndefinedCtor, obj any, argument any) any {
	if v, ok := tryItem(obj, argument); ok {
		return v
	}
	if s, isStr := argument.(string); isStr {
		if v, ok := tryAttr(obj, s); ok {
			return v
		}
	}
	if u, ok := obj.(*Undefined); ok {
		return u.GetItem(argument)
	}
	return undef("", obj, argument)
}

// AttrGetter 允许用户类型自定义属性访问 (对应 __getattr__).
type AttrGetter interface {
	JinjaGetAttr(name string) (any, bool)
}

// ItemGetter 允许用户类型自定义下标访问 (对应 __getitem__).
type ItemGetter interface {
	JinjaGetItem(key any) (any, bool)
}

// tryAttr 实现 Python getattr 语义.
func tryAttr(obj any, name string) (any, bool) {
	switch ov := obj.(type) {
	case *Namespace:
		return ov.GetAttr(name)
	case *Dict:
		// dict 的属性是方法 (items/keys/...), 不是键
		if m, ok := dictMethod(ov, name); ok {
			return m, true
		}
		return nil, false
	case string:
		return strMethod(ov, name)
	case Markup:
		if m, ok := strMethod(string(ov), name); ok {
			return m, true
		}
		return nil, false
	case []any:
		return listMethod(ov, name)
	case *Undefined:
		return nil, false // 由 GetAttr/GetItem 的 undefined 分支处理
	case *LoopContext:
		return ov.Attr(name)
	case AttrGetter:
		return ov.JinjaGetAttr(name)
	case nil:
		return nil, false
	}
	return reflectAttr(obj, name)
}

// reflectAttr 通过反射访问 Go 结构体字段与方法.
// 方法名映射: 精确匹配优先, snake_case -> CamelCase 兜底.
func reflectAttr(obj any, name string) (any, bool) {
	rv := reflect.ValueOf(obj)
	if !rv.IsValid() {
		return nil, false
	}
	// 方法 (含指针接收者)
	for _, candidate := range nameCandidates(name) {
		if m := rv.MethodByName(candidate); m.IsValid() {
			return goFunc{fn: m, name: name}, true
		}
	}
	elem := rv
	for elem.Kind() == reflect.Pointer {
		if elem.IsNil() {
			return nil, false
		}
		elem = elem.Elem()
	}
	if elem.Kind() == reflect.Struct {
		for _, candidate := range nameCandidates(name) {
			if f := elem.FieldByName(candidate); f.IsValid() && f.CanInterface() {
				return f.Interface(), true
			}
		}
	}
	return nil, false
}

// nameCandidates 返回属性名的候选 Go 名称.
func nameCandidates(name string) []string {
	out := []string{name}
	if camel := snakeToCamel(name); camel != name {
		out = append(out, camel)
	}
	// 首字母大写的精确名 (foo -> Foo)
	if exported := exportName(name); exported != name && exported != snakeToCamel(name) {
		out = append(out, exported)
	}
	return out
}

func exportName(name string) string {
	r, sz := utf8.DecodeRuneInString(name)
	if r == utf8.RuneError {
		return name
	}
	return string(unicode.ToUpper(r)) + name[sz:]
}

func snakeToCamel(name string) string {
	parts := strings.Split(name, "_")
	var b strings.Builder
	for _, p := range parts {
		if p == "" {
			continue
		}
		r, sz := utf8.DecodeRuneInString(p)
		b.WriteRune(unicode.ToUpper(r))
		b.WriteString(p[sz:])
	}
	return b.String()
}

// tryItem 实现 Python obj[key] 语义.
func tryItem(obj any, key any) (any, bool) {
	switch ov := obj.(type) {
	case *Dict:
		return ov.Get(key)
	case []any:
		return seqIndex(ov, key)
	case Tuple:
		return seqIndex(ov, key)
	case string:
		return strIndex(ov, key, false)
	case Markup:
		return strIndex(string(ov), key, true)
	case ItemGetter:
		return ov.JinjaGetItem(key)
	case nil:
		return nil, false
	}
	rv := reflect.ValueOf(obj)
	switch rv.Kind() {
	case reflect.Map:
		kv := reflect.ValueOf(key)
		if !kv.IsValid() || !kv.Type().AssignableTo(rv.Type().Key()) {
			// int64 键访问 map[int]X 等: 尝试转换
			if kv.IsValid() && kv.Type().ConvertibleTo(rv.Type().Key()) {
				kv = kv.Convert(rv.Type().Key())
			} else {
				return nil, false
			}
		}
		mv := rv.MapIndex(kv)
		if !mv.IsValid() {
			return nil, false
		}
		return mv.Interface(), true
	case reflect.Slice, reflect.Array:
		if sl, ok := key.(*PySlice); ok {
			items := make([]any, rv.Len())
			for i := range items {
				items[i] = rv.Index(i).Interface()
			}
			v, _ := seqIndex(items, sl)
			return v, true
		}
		idx, ok := asInt(key)
		if !ok {
			return nil, false
		}
		n := int64(rv.Len())
		if idx < 0 {
			idx += n
		}
		if idx < 0 || idx >= n {
			return nil, false
		}
		return rv.Index(int(idx)).Interface(), true
	}
	return nil, false
}

func asInt(v any) (int64, bool) {
	switch tv := v.(type) {
	case int64:
		return tv, true
	case int:
		return int64(tv), true
	case bool:
		if tv {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// PySlice 是切片下标值 (Slice 节点求值结果).
type PySlice struct {
	Start, Stop, Step any // nil 表示省略
}

func seqIndex(seq []any, key any) (any, bool) {
	if sl, ok := key.(*PySlice); ok {
		start, stop, step := resolveSlice(sl, len(seq))
		var out []any
		for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
			out = append(out, seq[i])
		}
		if out == nil {
			out = []any{}
		}
		return out, true
	}
	idx, ok := asInt(key)
	if !ok {
		return nil, false
	}
	n := int64(len(seq))
	if idx < 0 {
		idx += n
	}
	if idx < 0 || idx >= n {
		return nil, false
	}
	return seq[idx], true
}

func strIndex(s string, key any, markup bool) (any, bool) {
	runes := []rune(s)
	wrap := func(str string) any {
		if markup {
			return Markup(str)
		}
		return str
	}
	if sl, ok := key.(*PySlice); ok {
		start, stop, step := resolveSlice(sl, len(runes))
		var b strings.Builder
		for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
			b.WriteRune(runes[i])
		}
		return wrap(b.String()), true
	}
	idx, ok := asInt(key)
	if !ok {
		return nil, false
	}
	n := int64(len(runes))
	if idx < 0 {
		idx += n
	}
	if idx < 0 || idx >= n {
		return nil, false
	}
	return wrap(string(runes[idx])), true
}

// resolveSlice 复刻 Python slice.indices 的钳制语义.
func resolveSlice(sl *PySlice, length int) (start, stop, step int) {
	step = 1
	if sl.Step != nil {
		s, ok := asInt(sl.Step)
		if !ok || s == 0 {
			if ok && s == 0 {
				RaiseType("slice step cannot be zero")
			}
			RaiseType("slice indices must be integers or None")
		}
		step = int(s)
	}
	defaultStart, defaultStop := 0, length
	if step < 0 {
		defaultStart, defaultStop = length-1, -1
	}
	clamp := func(v, lo, hi int) int {
		if v < lo {
			return lo
		}
		if v > hi {
			return hi
		}
		return v
	}
	resolve := func(v any, def int) int {
		if v == nil {
			return def
		}
		iv, ok := asInt(v)
		if !ok {
			RaiseType("slice indices must be integers or None")
		}
		i := int(iv)
		if i < 0 {
			i += length
		}
		if step > 0 {
			return clamp(i, 0, length)
		}
		return clamp(i, -1, length-1)
	}
	start = resolve(sl.Start, defaultStart)
	stop = resolve(sl.Stop, defaultStop)
	return start, stop, step
}

package runtime

import (
	"reflect"
)

// Iterate 实现 Python 迭代协议, 返回元素切片.
// 字符串按 rune 迭代为单字符字符串; dict 迭代键.
// 不可迭代时抛 TypeError.
func Iterate(v any) []any {
	items, ok := tryIterate(v)
	if !ok {
		RaiseType(PyStrRepr(PyTypeName(v)) + " object is not iterable")
	}
	return items
}

func tryIterate(v any) ([]any, bool) {
	switch tv := v.(type) {
	case string:
		return stringItems(tv, false), true
	case Markup:
		return stringItems(string(tv), true), true
	case []any:
		return tv, true
	case Tuple:
		return tv, true
	case *Dict:
		return tv.Keys(), true
	case *Undefined:
		return tv.IterItems(), true
	case Iterable:
		return tv.Iter(), true
	case nil:
		return nil, false
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		items := make([]any, rv.Len())
		for i := range items {
			items[i] = rv.Index(i).Interface()
		}
		return items, true
	case reflect.Map:
		keys := sortedMapKeys(rv)
		items := make([]any, len(keys))
		for i, k := range keys {
			items[i] = k.Interface()
		}
		return items, true
	case reflect.Chan:
		var items []any
		for {
			x, ok := rv.Recv()
			if !ok {
				break
			}
			items = append(items, x.Interface())
		}
		return items, true
	}
	return nil, false
}

// Iterable 允许用户类型自定义迭代行为 (对应 __iter__).
type Iterable interface {
	Iter() []any
}

func stringItems(s string, markup bool) []any {
	out := make([]any, 0, len(s))
	for _, r := range s {
		if markup {
			out = append(out, Markup(r))
		} else {
			out = append(out, string(r))
		}
	}
	return out
}

// Length 实现 Python len(). 不支持时抛 TypeError.
func Length(v any) int {
	n, ok := tryLength(v)
	if !ok {
		RaiseType("object of type " + PyStrRepr(PyTypeName(v)) + " has no len()")
	}
	return n
}

func tryLength(v any) (int, bool) {
	switch tv := v.(type) {
	case string:
		n := 0
		for range tv {
			n++
		}
		return n, true
	case Markup:
		n := 0
		for range string(tv) {
			n++
		}
		return n, true
	case []any:
		return len(tv), true
	case Tuple:
		return len(tv), true
	case *Dict:
		return tv.Len(), true
	case *Undefined:
		return tv.Length(), true
	case Lener:
		return tv.Len(), true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map, reflect.Chan:
		return rv.Len(), true
	}
	return 0, false
}

// Lener 允许用户类型自定义 len (对应 __len__).
type Lener interface {
	Len() int
}

// ToList 复制为 list ([]any).
func ToList(v any) []any {
	items := Iterate(v)
	out := make([]any, len(items))
	copy(out, items)
	return out
}

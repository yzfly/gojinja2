package runtime

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"unicode"
)

// Dict 是保插入序的字典, 复刻 Python 3.7+ dict 语义.
// 键按 Python 相等语义归一 (1 == 1.0 == True 视为同一键).
type DictItem struct {
	Key   any
	Value any
}

type Dict struct {
	items []DictItem
	index map[string]int // 归一化键 -> items 下标
}

func NewDict() *Dict {
	return &Dict{index: map[string]int{}}
}

// DictFromPairs 按顺序构造.
func DictFromPairs(pairs []DictItem) *Dict {
	d := NewDict()
	for _, p := range pairs {
		d.Set(p.Key, p.Value)
	}
	return d
}

// hashKey 把 Python 可哈希值归一为字符串键 (类型标记保证单射).
// 对应 Python hash 语义: 1 == 1.0 == True 同键.
func hashKey(v any) string {
	switch tv := v.(type) {
	case nil:
		return "n"
	case bool:
		if tv {
			return "i1"
		}
		return "i0"
	case int64:
		return "i" + fmt.Sprint(tv)
	case int:
		return "i" + fmt.Sprint(tv)
	case float64:
		if tv == float64(int64(tv)) {
			return "i" + fmt.Sprint(int64(tv))
		}
		return "f" + fmt.Sprint(tv)
	case string:
		return "s" + tv
	case Markup:
		return "s" + string(tv)
	case Tuple:
		parts := make([]string, len(tv))
		for i, item := range tv {
			parts[i] = hashKey(item)
		}
		return "t(" + strings.Join(parts, ",") + ")"
	case *Undefined:
		return "u" + fmt.Sprint(tv.Kind)
	}
	// 不可哈希类型 (list / dict)
	RaiseType("unhashable type: " + PyStrRepr(PyTypeName(v)))
	return ""
}

func (d *Dict) Len() int { return len(d.items) }

func (d *Dict) Set(key, value any) {
	k := hashKey(key)
	if i, ok := d.index[k]; ok {
		// 保留原键 (Python 语义), 只更新值
		d.items[i].Value = value
		return
	}
	d.index[k] = len(d.items)
	d.items = append(d.items, DictItem{Key: key, Value: value})
}

func (d *Dict) Get(key any) (any, bool) {
	if i, ok := d.index[hashKey(key)]; ok {
		return d.items[i].Value, true
	}
	return nil, false
}

func (d *Dict) Has(key any) bool {
	_, ok := d.index[hashKey(key)]
	return ok
}

func (d *Dict) Delete(key any) bool {
	k := hashKey(key)
	i, ok := d.index[k]
	if !ok {
		return false
	}
	delete(d.index, k)
	d.items = append(d.items[:i:i], d.items[i+1:]...)
	for kk, idx := range d.index {
		if idx > i {
			d.index[kk] = idx - 1
		}
	}
	return true
}

// Items 返回保序的键值对 (副本切片头).
func (d *Dict) Items() []DictItem { return d.items }

func (d *Dict) Keys() []any {
	out := make([]any, len(d.items))
	for i, it := range d.items {
		out[i] = it.Key
	}
	return out
}

func (d *Dict) Values() []any {
	out := make([]any, len(d.items))
	for i, it := range d.items {
		out[i] = it.Value
	}
	return out
}

func (d *Dict) Copy() *Dict {
	nd := NewDict()
	for _, it := range d.items {
		nd.Set(it.Key, it.Value)
	}
	return nd
}

func (d *Dict) repr() string {
	var b strings.Builder
	b.WriteByte('{')
	for i, it := range d.items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(Repr(it.Key))
		b.WriteString(": ")
		b.WriteString(Repr(it.Value))
	}
	b.WriteByte('}')
	return b.String()
}

// DictFromStringMap 从 Go map 构造 (键排序保证确定性).
func DictFromStringMap[V any](m map[string]V) *Dict {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	d := NewDict()
	for _, k := range keys {
		d.Set(k, m[k])
	}
	return d
}

// sortedMapKeys 为反射 map 返回确定性排序的键.
func sortedMapKeys(rv reflect.Value) []reflect.Value {
	keys := rv.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
	})
	return keys
}

// isPrintable 近似 Python str.isprintable 的单字符判断.
func isPrintable(r rune) bool {
	if r == ' ' {
		return true
	}
	return unicode.IsPrint(r) && !unicode.IsSpace(r)
}

// Namespace 对应 jinja2.utils.Namespace.
type Namespace struct {
	attrs *Dict
}

func NewNamespace(args []any, kwargs *Dict) *Namespace {
	ns := &Namespace{attrs: NewDict()}
	for _, a := range args {
		// dict(mapping) 语义: 接受 dict 或键值对序列
		if d, ok := a.(*Dict); ok {
			for _, it := range d.Items() {
				ns.attrs.Set(it.Key, it.Value)
			}
		}
	}
	if kwargs != nil {
		for _, it := range kwargs.Items() {
			ns.attrs.Set(it.Key, it.Value)
		}
	}
	return ns
}

func (ns *Namespace) GetAttr(name string) (any, bool) { return ns.attrs.Get(name) }
func (ns *Namespace) SetAttr(name string, value any)  { ns.attrs.Set(name, value) }

func (ns *Namespace) PyRepr() string { return "<Namespace " + ns.attrs.repr() + ">" }

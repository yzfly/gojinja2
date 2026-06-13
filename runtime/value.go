// Package runtime 实现 Jinja2 的值系统 (Python 语义层) 与运行时对象,
// 对应 jinja2/runtime.py 及散布在 utils.py / markupsafe 中的语义.
//
// 规范值类型 (模板内部产生的值):
//
//	nil            -> None
//	bool           -> bool
//	int64          -> int   (int64 起步, big.Int 预埋见 ops.go)
//	float64        -> float
//	string         -> str
//	Markup         -> markupsafe.Markup (安全字符串)
//	[]any          -> list
//	Tuple          -> tuple
//	*Dict          -> dict (保序)
//	*Undefined     -> Undefined 族
//
// 用户传入的任意 Go 值通过反射适配 (attr.go / iter.go).
//
// 错误约定: 模板执行栈使用 panic(error) 传播异常 (对应 Python 异常),
// 在 Template.Render 边界统一 recover.
package runtime

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
)

// Missing 是内部哨兵值, 对应 jinja2.utils.missing.
type missingType struct{}

func (missingType) String() string { return "missing" }

var Missing any = missingType{}

// Markup 是 HTML 安全字符串, 对应 markupsafe.Markup.
type Markup string

// HTML 实现 HTMLer 协议 (对应 __html__).
func (m Markup) HTML() string { return string(m) }

// HTMLer 对应 markupsafe 的 __html__ 协议.
type HTMLer interface{ HTML() string }

// Tuple 是 Python 元组.
type Tuple []any

// Callable 是模板可调用对象的协议.
// kwargs 传 nil 表示无关键字参数.
type Callable interface {
	Call(args []any, kwargs *Dict) any
}

// Repr 输出对象的 Python repr (用于调试与某些 str 转换).
type Reprer interface{ PyRepr() string }

// Strer 自定义 Python str() 行为.
type Strer interface{ PyStr() string }

// RaiseUndefined 抛 UndefinedError.
func RaiseUndefined(msg string) {
	panic(&exceptions.UndefinedError{Message: msg})
}

// RaiseType 抛 Python TypeError 的等价错误.
func RaiseType(msg string) {
	panic(&exceptions.TemplateRuntimeError{Message: msg})
}

// PyTypeName 返回值的 Python 类型名.
func PyTypeName(v any) string {
	switch tv := v.(type) {
	case nil:
		return "NoneType"
	case bool:
		return "bool"
	case int64, int, int32, int16, int8, uint, uint64, uint32, uint16, uint8:
		return "int"
	case float64, float32:
		return "float"
	case Markup:
		return "markupsafe.Markup"
	case string:
		return "str"
	case []any:
		return "list"
	case Tuple:
		return "tuple"
	case *Dict:
		return "dict"
	case *Undefined:
		return tv.pyClassName()
	case *Namespace:
		return "jinja2.utils.Namespace"
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		return "list"
	case reflect.Map:
		return "dict"
	case reflect.Func:
		return "function"
	}
	return rv.Type().String()
}

// ObjectTypeRepr 对应 jinja2.utils.object_type_repr.
func ObjectTypeRepr(v any) string {
	if v == nil {
		return "None"
	}
	name := PyTypeName(v)
	return name + " object"
}

// Truth 实现 Python 真值判断.
func Truth(v any) bool {
	switch tv := v.(type) {
	case nil:
		return false
	case missingType:
		return false
	case bool:
		return tv
	case int64:
		return tv != 0
	case float64:
		return tv != 0 // NaN 为 true, != 0 成立
	case string:
		return tv != ""
	case Markup:
		return tv != ""
	case []any:
		return len(tv) > 0
	case Tuple:
		return len(tv) > 0
	case *Dict:
		return tv.Len() > 0
	case *Undefined:
		return tv.truth()
	case int:
		return tv != 0
	case *LoopContext:
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Map, reflect.Array, reflect.Chan:
		return rv.Len() > 0
	case reflect.Ptr:
		if rv.IsNil() {
			return false
		}
	}
	// 其余对象 (函数, 结构体...) 恒为 True
	return true
}

// Str 实现 Python str().
func Str(v any) string {
	switch tv := v.(type) {
	case nil:
		return "None"
	case bool:
		if tv {
			return "True"
		}
		return "False"
	case int64:
		return strconv.FormatInt(tv, 10)
	case int:
		return strconv.Itoa(tv)
	case float64:
		return PyFloatStr(tv)
	case string:
		return tv
	case Markup:
		return string(tv)
	case *Undefined:
		return tv.str()
	case Strer:
		return tv.PyStr()
	case []any, Tuple, *Dict:
		return Repr(v)
	case Reprer:
		return tv.PyRepr()
	case error:
		return tv.Error()
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return Repr(v)
	}
	return fmt.Sprintf("%v", v)
}

// Repr 实现 Python repr().
func Repr(v any) string {
	switch tv := v.(type) {
	case nil:
		return "None"
	case bool, int64, int, float64:
		return Str(v)
	case string:
		return PyStrRepr(tv)
	case Markup:
		return "Markup(" + PyStrRepr(string(tv)) + ")"
	case *Undefined:
		return tv.repr()
	case []any:
		return seqRepr(tv, "[", "]", false)
	case Tuple:
		return seqRepr(tv, "(", ")", true)
	case *Dict:
		return tv.repr()
	case Reprer:
		return tv.PyRepr()
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		items := make([]any, rv.Len())
		for i := range items {
			items[i] = rv.Index(i).Interface()
		}
		return seqRepr(items, "[", "]", false)
	case reflect.Map:
		return mapRepr(rv)
	}
	return fmt.Sprintf("%v", v)
}

func seqRepr(items []any, open, close string, isTuple bool) string {
	var b strings.Builder
	b.WriteString(open)
	for i, item := range items {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(Repr(item))
	}
	if isTuple && len(items) == 1 {
		b.WriteByte(',')
	}
	b.WriteString(close)
	return b.String()
}

func mapRepr(rv reflect.Value) string {
	keys := sortedMapKeys(rv)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(Repr(k.Interface()))
		b.WriteString(": ")
		b.WriteString(Repr(rv.MapIndex(k).Interface()))
	}
	b.WriteByte('}')
	return b.String()
}

// PyStrRepr 复刻 Python str 的 repr.
func PyStrRepr(s string) string {
	quote := byte('\'')
	if strings.ContainsRune(s, '\'') && !strings.ContainsRune(s, '"') {
		quote = '"'
	}
	var b strings.Builder
	b.WriteByte(quote)
	for _, r := range s {
		switch {
		case r == rune(quote) || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case isPrintable(r):
			b.WriteRune(r)
		case r < 0x100:
			fmt.Fprintf(&b, `\x%02x`, r)
		case r < 0x10000:
			fmt.Fprintf(&b, `\u%04x`, r)
		default:
			fmt.Fprintf(&b, `\U%08x`, r)
		}
	}
	b.WriteByte(quote)
	return b.String()
}

// PyFloatStr 复刻 Python float 的 str/repr.
func PyFloatStr(f float64) string {
	switch {
	case math.IsInf(f, 1):
		return "inf"
	case math.IsInf(f, -1):
		return "-inf"
	case math.IsNaN(f):
		return "nan"
	case f == math.Trunc(f) && math.Abs(f) < 1e16:
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	// Python 在 1e16 以上才用科学计数法, Go 'g' 在 1e21; 而 Python repr
	// 对 1e16..1e21 区间输出 1e+16 形式; Go 输出 1e+16 相同. 差异主要在
	// 指数两位补零, FormatFloat 已与 CPython 一致 (e+16 / e-09).
	return s
}

// Escape 对应 markupsafe.escape.
// Undefined 一律按 str() 后转义 (仅 ChainableUndefined 有 __html__,
// 但其 str 恒为空串, 结果一致).
func Escape(v any) Markup {
	if u, ok := v.(*Undefined); ok {
		return Markup(escapeString(u.str()))
	}
	if h, ok := v.(HTMLer); ok {
		return Markup(h.HTML())
	}
	return Markup(escapeString(Str(v)))
}

// EscapeSoft 对应 soft_str + 条件转义: 已是 Markup 则原样.
func escapeString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&#34;")
		case '\'':
			b.WriteString("&#39;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

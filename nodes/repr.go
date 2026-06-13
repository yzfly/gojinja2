package nodes

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode"
)

// IsNil 判断接口值是否为 nil 或带类型的 nil 指针.
func IsNil(n Node) bool {
	if n == nil {
		return true
	}
	rv := reflect.ValueOf(n)
	return rv.Kind() == reflect.Pointer && rv.IsNil()
}

// PyRepr 输出与 Python repr(node) 完全一致的字符串,
// 用于和参考实现做 AST 级 conformance 对齐.
func PyRepr(n Node) string {
	var b strings.Builder
	writeNode(&b, n)
	return b.String()
}

func writeNode(b *strings.Builder, n Node) {
	if IsNil(n) {
		b.WriteString("None")
		return
	}
	b.WriteString(TypeName(n))
	b.WriteByte('(')
	switch v := n.(type) {
	case *Template:
		field(b, 0, "body", nodeList(v.Body))
	case *Output:
		field(b, 0, "nodes", exprList(v.Nodes))
	case *Extends:
		field(b, 0, "template", v.Template)
	case *For:
		field(b, 0, "target", v.Target)
		field(b, 1, "iter", v.Iter)
		field(b, 2, "body", nodeList(v.Body))
		field(b, 3, "else_", nodeList(v.Else))
		field(b, 4, "test", v.Test)
		field(b, 5, "recursive", v.Recursive)
	case *If:
		field(b, 0, "test", v.Test)
		field(b, 1, "body", nodeList(v.Body))
		elifs := make([]Node, len(v.Elif))
		for i, e := range v.Elif {
			elifs[i] = e
		}
		field(b, 2, "elif_", elifs)
		field(b, 3, "else_", nodeList(v.Else))
	case *Macro:
		field(b, 0, "name", v.Name)
		field(b, 1, "args", nameList(v.Args))
		field(b, 2, "defaults", exprList(v.Defaults))
		field(b, 3, "body", nodeList(v.Body))
	case *CallBlock:
		field(b, 0, "call", v.Call)
		field(b, 1, "args", nameList(v.Args))
		field(b, 2, "defaults", exprList(v.Defaults))
		field(b, 3, "body", nodeList(v.Body))
	case *FilterBlock:
		field(b, 0, "body", nodeList(v.Body))
		field(b, 1, "filter", v.Filter)
	case *With:
		field(b, 0, "targets", exprList(v.Targets))
		field(b, 1, "values", exprList(v.Values))
		field(b, 2, "body", nodeList(v.Body))
	case *Block:
		field(b, 0, "name", v.Name)
		field(b, 1, "body", nodeList(v.Body))
		field(b, 2, "scoped", v.Scoped)
		field(b, 3, "required", v.Required)
	case *Include:
		field(b, 0, "template", v.Template)
		field(b, 1, "with_context", v.WithContext)
		field(b, 2, "ignore_missing", v.IgnoreMissing)
	case *Import:
		field(b, 0, "template", v.Template)
		field(b, 1, "target", v.Target)
		field(b, 2, "with_context", v.WithContext)
	case *FromImport:
		field(b, 0, "template", v.Template)
		field(b, 1, "names", importNames(v.Names))
		field(b, 2, "with_context", v.WithContext)
	case *ExprStmt:
		field(b, 0, "node", v.Node)
	case *Assign:
		field(b, 0, "target", v.Target)
		field(b, 1, "node", v.Node)
	case *AssignBlock:
		field(b, 0, "target", v.Target)
		field(b, 1, "filter", v.Filter)
		field(b, 2, "body", nodeList(v.Body))
	case *Scope:
		field(b, 0, "body", nodeList(v.Body))
	case *ScopedEvalContextModifier:
		kws := make([]Node, len(v.Options))
		for i, k := range v.Options {
			kws[i] = k
		}
		field(b, 0, "options", kws)
		field(b, 1, "body", nodeList(v.Body))
	case *Break, *Continue:
		// 无字段
	case *Name:
		field(b, 0, "name", v.Name)
		field(b, 1, "ctx", v.Ctx)
	case *NSRef:
		field(b, 0, "name", v.Name)
		field(b, 1, "attr", v.Attr)
	case *Const:
		field(b, 0, "value", v.Value)
	case *TemplateData:
		field(b, 0, "data", v.Data)
	case *Tuple:
		field(b, 0, "items", exprList(v.Items))
		field(b, 1, "ctx", v.Ctx)
	case *List:
		field(b, 0, "items", exprList(v.Items))
	case *Dict:
		pairs := make([]Node, len(v.Items))
		for i, p := range v.Items {
			pairs[i] = p
		}
		field(b, 0, "items", pairs)
	case *Pair:
		field(b, 0, "key", v.Key)
		field(b, 1, "value", v.Value)
	case *Keyword:
		field(b, 0, "key", v.Key)
		field(b, 1, "value", v.Value)
	case *CondExpr:
		field(b, 0, "test", v.Test)
		field(b, 1, "expr1", v.Expr1)
		field(b, 2, "expr2", v.Expr2)
	case *Filter:
		writeFilterTestCall(b, v.Node, v.Name, true, v.Args, v.Kwargs, v.DynArgs, v.DynKwargs)
	case *Test:
		writeFilterTestCall(b, v.Node, v.Name, true, v.Args, v.Kwargs, v.DynArgs, v.DynKwargs)
	case *Call:
		writeFilterTestCall(b, v.Node, "", false, v.Args, v.Kwargs, v.DynArgs, v.DynKwargs)
	case *Getitem:
		field(b, 0, "node", v.Node)
		field(b, 1, "arg", v.Arg)
		field(b, 2, "ctx", v.Ctx)
	case *Getattr:
		field(b, 0, "node", v.Node)
		field(b, 1, "attr", v.Attr)
		field(b, 2, "ctx", v.Ctx)
	case *Slice:
		field(b, 0, "start", v.Start)
		field(b, 1, "stop", v.Stop)
		field(b, 2, "step", v.Step)
	case *Concat:
		field(b, 0, "nodes", exprList(v.Nodes))
	case *Compare:
		field(b, 0, "expr", v.Expr)
		ops := make([]Node, len(v.Ops))
		for i, o := range v.Ops {
			ops[i] = o
		}
		field(b, 1, "ops", ops)
	case *Operand:
		field(b, 0, "op", v.Op)
		field(b, 1, "expr", v.Expr)
	case *BinExpr:
		field(b, 0, "left", v.Left)
		field(b, 1, "right", v.Right)
	case *UnaryExpr:
		field(b, 0, "node", v.Node)
	case *MarkSafe:
		field(b, 0, "expr", v.Expr)
	case *MarkSafeIfAutoescape:
		field(b, 0, "expr", v.Expr)
	case *InternalName:
		field(b, 0, "name", v.Name)
	case *ContextReference, *DerivedContextReference:
		// 无字段
	}
	b.WriteByte(')')
}

// importNames 把 FromImport.Names 转为 Python repr 形式的中间值.
type pyTuple []any

func importNames(names []ImportName) any {
	out := make([]any, len(names))
	for i, n := range names {
		if n.Alias == "" {
			out[i] = n.Name
		} else {
			out[i] = pyTuple{n.Name, n.Alias}
		}
	}
	return out
}

func nodeList(ns []Node) []Node { return ns }

func exprList(es []Expr) []Node {
	out := make([]Node, len(es))
	for i, e := range es {
		out[i] = e
	}
	return out
}

func nameList(ns []*Name) []Node {
	out := make([]Node, len(ns))
	for i, n := range ns {
		out[i] = n
	}
	return out
}

func writeFilterTestCall(b *strings.Builder, node Expr, name string, hasName bool,
	args []Expr, kwargs []*Keyword, dynArgs, dynKwargs Expr) {
	field(b, 0, "node", node)
	i := 1
	if hasName {
		field(b, i, "name", name)
		i++
	}
	field(b, i, "args", exprList(args))
	kws := make([]Node, len(kwargs))
	for j, k := range kwargs {
		kws[j] = k
	}
	field(b, i+1, "kwargs", kws)
	field(b, i+2, "dyn_args", dynArgs)
	field(b, i+3, "dyn_kwargs", dynKwargs)
}

func field(b *strings.Builder, idx int, name string, value any) {
	if idx > 0 {
		b.WriteString(", ")
	}
	b.WriteString(name)
	b.WriteByte('=')
	writeValue(b, value)
}

func writeValue(b *strings.Builder, value any) {
	switch v := value.(type) {
	case nil:
		b.WriteString("None")
	case Node:
		writeNode(b, v)
	case Expr:
		writeNode(b, v)
	case []Node:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			writeNode(b, item)
		}
		b.WriteByte(']')
	case []any:
		b.WriteByte('[')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			writeValue(b, item)
		}
		b.WriteByte(']')
	case pyTuple:
		b.WriteByte('(')
		for i, item := range v {
			if i > 0 {
				b.WriteString(", ")
			}
			writeValue(b, item)
		}
		if len(v) == 1 {
			b.WriteByte(',')
		}
		b.WriteByte(')')
	case string:
		b.WriteString(PyStrRepr(v))
	case bool:
		if v {
			b.WriteString("True")
		} else {
			b.WriteString("False")
		}
	case int:
		b.WriteString(strconv.Itoa(v))
	case int64:
		b.WriteString(strconv.FormatInt(v, 10))
	case float64:
		b.WriteString(PyFloatRepr(v))
	default:
		fmt.Fprintf(b, "%v", v)
	}
}

// PyStrRepr 复刻 Python str 的 repr: 默认单引号;
// 串内含单引号且不含双引号时用双引号.
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
		case unicode.IsPrint(r):
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

// PyFloatRepr 复刻 Python float 的 repr.
func PyFloatRepr(f float64) string {
	if math.IsInf(f, 1) {
		return "inf"
	}
	if math.IsInf(f, -1) {
		return "-inf"
	}
	if f == math.Trunc(f) && math.Abs(f) < 1e16 {
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

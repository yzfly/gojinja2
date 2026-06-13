package runtime

import (
	"math"
	"strings"
)

// asNumber 把值规约为数值. bool 是 int 子类 (True==1).
// 返回 (intVal, floatVal, isInt, isNumber).
func asNumber(v any) (int64, float64, bool, bool) {
	switch tv := v.(type) {
	case bool:
		if tv {
			return 1, 1, true, true
		}
		return 0, 0, true, true
	case int64:
		return tv, float64(tv), true, true
	case int:
		return int64(tv), float64(tv), true, true
	case float64:
		return 0, tv, false, true
	}
	return 0, 0, false, false
}

func failUndefinedOp(v any) bool {
	if u, ok := v.(*Undefined); ok {
		u.Fail()
	}
	return false
}

func binOpTypeError(op string, a, b any) {
	RaiseType("unsupported operand type(s) for " + op + ": " +
		PyStrRepr(PyTypeName(a)) + " and " + PyStrRepr(PyTypeName(b)))
}

// Add 实现 Python 的 + 运算.
func Add(a, b any) any {
	failUndefinedOp(a)
	failUndefinedOp(b)
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if bi, bf, bInt, bNum := asNumber(b); bNum {
			if aInt && bInt {
				return ai + bi
			}
			return af + bf
		}
		binOpTypeError("+", a, b)
	}
	switch av := a.(type) {
	case Markup:
		switch bv := b.(type) {
		case string, Markup:
			return av + Escape(bv)
		}
		binOpTypeError("+", a, b)
	case string:
		switch bv := b.(type) {
		case string:
			return av + bv
		case Markup:
			return Escape(av) + bv
		}
		// Python: can only concatenate str (not "X") to str
		RaiseType(`can only concatenate str (not ` + PyStrRepr(PyTypeName(b)) + `) to str`)
	case []any:
		if bv, ok := b.([]any); ok {
			out := make([]any, 0, len(av)+len(bv))
			return append(append(out, av...), bv...)
		}
		RaiseType(`can only concatenate list (not ` + PyStrRepr(PyTypeName(b)) + `) to list`)
	case Tuple:
		if bv, ok := b.(Tuple); ok {
			out := make(Tuple, 0, len(av)+len(bv))
			return append(append(out, av...), bv...)
		}
		RaiseType(`can only concatenate tuple (not ` + PyStrRepr(PyTypeName(b)) + `) to tuple`)
	}
	binOpTypeError("+", a, b)
	return nil
}

// Sub 实现 -.
func Sub(a, b any) any {
	failUndefinedOp(a)
	failUndefinedOp(b)
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if bi, bf, bInt, bNum := asNumber(b); bNum {
			if aInt && bInt {
				return ai - bi
			}
			return af - bf
		}
	}
	binOpTypeError("-", a, b)
	return nil
}

// Mul 实现 *: 数值乘法, 序列/字符串重复.
func Mul(a, b any) any {
	failUndefinedOp(a)
	failUndefinedOp(b)
	ai, af, aInt, aNum := asNumber(a)
	bi, bf, bInt, bNum := asNumber(b)
	if aNum && bNum {
		if aInt && bInt {
			return ai * bi
		}
		return af * bf
	}
	// 序列重复: str * int, list * int (两个方向)
	if aNum && aInt {
		return repeatSeq(b, ai, a)
	}
	if bNum && bInt {
		return repeatSeq(a, bi, b)
	}
	binOpTypeError("*", a, b)
	return nil
}

func repeatSeq(seq any, n int64, other any) any {
	if n < 0 {
		n = 0
	}
	switch sv := seq.(type) {
	case string:
		return strings.Repeat(sv, int(n))
	case Markup:
		return Markup(strings.Repeat(string(sv), int(n)))
	case []any:
		out := make([]any, 0, int(n)*len(sv))
		for i := int64(0); i < n; i++ {
			out = append(out, sv...)
		}
		return out
	case Tuple:
		out := make(Tuple, 0, int(n)*len(sv))
		for i := int64(0); i < n; i++ {
			out = append(out, sv...)
		}
		return out
	}
	binOpTypeError("*", seq, other)
	return nil
}

// TrueDiv 实现 / (恒为 float).
func TrueDiv(a, b any) any {
	failUndefinedOp(a)
	failUndefinedOp(b)
	if _, af, _, aNum := asNumber(a); aNum {
		if _, bf, _, bNum := asNumber(b); bNum {
			if bf == 0 {
				RaiseType("division by zero")
			}
			return af / bf
		}
	}
	binOpTypeError("/", a, b)
	return nil
}

// FloorDiv 实现 // (向负无穷取整; int//int -> int).
func FloorDiv(a, b any) any {
	failUndefinedOp(a)
	failUndefinedOp(b)
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if bi, bf, bInt, bNum := asNumber(b); bNum {
			if aInt && bInt {
				if bi == 0 {
					RaiseType("integer division or modulo by zero")
				}
				q := ai / bi
				if (ai%bi != 0) && ((ai < 0) != (bi < 0)) {
					q--
				}
				return q
			}
			if bf == 0 {
				RaiseType("float floor division by zero")
			}
			return math.Floor(af / bf)
		}
	}
	binOpTypeError("//", a, b)
	return nil
}

// Mod 实现 % (结果符号随除数; 字符串为 printf 格式化).
func Mod(a, b any) any {
	failUndefinedOp(a)
	failUndefinedOp(b)
	if s, ok := a.(string); ok {
		return PyFormatPercent(s, b)
	}
	if m, ok := a.(Markup); ok {
		// Markup % args: 参数转义
		return Markup(PyFormatPercent(string(m), escapeFormatArgs(b)))
	}
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if bi, bf, bInt, bNum := asNumber(b); bNum {
			if aInt && bInt {
				if bi == 0 {
					RaiseType("integer division or modulo by zero")
				}
				r := ai % bi
				if r != 0 && (r < 0) != (bi < 0) {
					r += bi
				}
				return r
			}
			if bf == 0 {
				RaiseType("float modulo")
			}
			r := math.Mod(af, bf)
			if r != 0 && (r < 0) != (bf < 0) {
				r += bf
			}
			return r
		}
	}
	binOpTypeError("%", a, b)
	return nil
}

func escapeFormatArgs(b any) any {
	switch bv := b.(type) {
	case Tuple:
		out := make(Tuple, len(bv))
		for i, item := range bv {
			out[i] = Escape(item)
		}
		return out
	case *Dict:
		out := NewDict()
		for _, it := range bv.Items() {
			out.Set(it.Key, Escape(it.Value))
		}
		return out
	default:
		return Escape(b)
	}
}

// Pow 实现 **.
func Pow(a, b any) any {
	failUndefinedOp(a)
	failUndefinedOp(b)
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if bi, bf, bInt, bNum := asNumber(b); bNum {
			if aInt && bInt && bi >= 0 {
				// 整数幂 (int64 起步; 溢出风险是文档化差异)
				result := int64(1)
				base := ai
				exp := bi
				for exp > 0 {
					if exp&1 == 1 {
						result *= base
					}
					base *= base
					exp >>= 1
				}
				return result
			}
			return math.Pow(af, bf)
		}
	}
	binOpTypeError("** or pow()", a, b)
	return nil
}

// Neg 实现一元 -.
func Neg(a any) any {
	failUndefinedOp(a)
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if _, isBool := a.(bool); aInt && !isBool {
			return -ai
		}
		if aInt {
			return -ai
		}
		return -af
	}
	RaiseType("bad operand type for unary -: " + PyStrRepr(PyTypeName(a)))
	return nil
}

// Pos 实现一元 +.
func Pos(a any) any {
	failUndefinedOp(a)
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if aInt {
			return ai
		}
		return af
	}
	RaiseType("bad operand type for unary +: " + PyStrRepr(PyTypeName(a)))
	return nil
}

// Concat 实现 ~ : 所有操作数 str() 后拼接.
// autoescape 由调用方决定使用 StrJoin 或 MarkupJoin.
func StrJoin(items []any) string {
	var b strings.Builder
	for _, item := range items {
		b.WriteString(Str(item))
	}
	return b.String()
}

// MarkupJoin 对应 markup_join: 任一项有 __html__ 则整体转义拼接.
func MarkupJoin(items []any) any {
	hasMarkup := false
	for _, item := range items {
		if _, ok := item.(HTMLer); ok {
			hasMarkup = true
			break
		}
	}
	if !hasMarkup {
		return StrJoin(items)
	}
	var b strings.Builder
	for _, item := range items {
		b.WriteString(string(Escape(item)))
	}
	return Markup(b.String())
}

// ---- 比较 ----

// Equal 实现 Python ==. 类型不匹配返回 False, 不报错.
func Equal(a, b any) bool {
	if u, ok := a.(*Undefined); ok {
		return u.Equals(b)
	}
	if u, ok := b.(*Undefined); ok {
		return u.Equals(a)
	}
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if bi, bf, bInt, bNum := asNumber(b); bNum {
			if aInt && bInt {
				return ai == bi
			}
			return af == bf
		}
		return false
	}
	switch av := a.(type) {
	case nil:
		return b == nil
	case string:
		return av == toStrOrNo(b)
	case Markup:
		return string(av) == toStrOrNo(b)
	case []any:
		bv, ok := b.([]any)
		return ok && seqEqual(av, bv)
	case Tuple:
		bv, ok := b.(Tuple)
		return ok && seqEqual(av, bv)
	case *Dict:
		bv, ok := b.(*Dict)
		if !ok || av.Len() != bv.Len() {
			return false
		}
		for _, it := range av.items {
			bVal, has := bv.Get(it.Key)
			if !has || !Equal(it.Value, bVal) {
				return false
			}
		}
		return true
	}
	// 用户 Go 值: 直接比较 (可比较类型)
	return equalFallback(a, b)
}

func equalFallback(a, b any) (eq bool) {
	defer func() {
		if recover() != nil {
			eq = false
		}
	}()
	return a == b
}

// toStrOrNo 返回字符串值; 非字符串返回不可能相等的标记.
func toStrOrNo(v any) string {
	switch tv := v.(type) {
	case string:
		return tv
	case Markup:
		return string(tv)
	}
	return "\x00\x00gojinja2-not-a-string\x00"
}

func seqEqual(a, b []any) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

// Compare 实现 < <= > >= , 返回 -1/0/1.
// 类型不可比时抛 TypeError.
func compareValues(op string, a, b any) int {
	failUndefinedOp(a)
	failUndefinedOp(b)
	if ai, af, aInt, aNum := asNumber(a); aNum {
		if bi, bf, bInt, bNum := asNumber(b); bNum {
			if aInt && bInt {
				switch {
				case ai < bi:
					return -1
				case ai > bi:
					return 1
				}
				return 0
			}
			switch {
			case af < bf:
				return -1
			case af > bf:
				return 1
			}
			return 0
		}
		compareTypeError(op, a, b)
	}
	switch av := a.(type) {
	case string, Markup:
		as := Str(av)
		switch b.(type) {
		case string, Markup:
			return strings.Compare(as, Str(b))
		}
		compareTypeError(op, a, b)
	case []any:
		if bv, ok := b.([]any); ok {
			return seqCompare(op, av, bv)
		}
		compareTypeError(op, a, b)
	case Tuple:
		if bv, ok := b.(Tuple); ok {
			return seqCompare(op, av, bv)
		}
		compareTypeError(op, a, b)
	}
	compareTypeError(op, a, b)
	return 0
}

func seqCompare(op string, a, b []any) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	for i := 0; i < n; i++ {
		if !Equal(a[i], b[i]) {
			return compareValues(op, a[i], b[i])
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

func compareTypeError(op string, a, b any) {
	RaiseType(PyStrRepr(op) + " not supported between instances of " +
		PyStrRepr(PyTypeName(a)) + " and " + PyStrRepr(PyTypeName(b)))
}

// CompareOp 按运算符名执行比较 (eq ne lt lteq gt gteq in notin).
func CompareOp(op string, a, b any) bool {
	switch op {
	case "eq":
		return Equal(a, b)
	case "ne":
		return !Equal(a, b)
	case "lt":
		return compareValues("<", a, b) < 0
	case "lteq":
		return compareValues("<=", a, b) <= 0
	case "gt":
		return compareValues(">", a, b) > 0
	case "gteq":
		return compareValues(">=", a, b) >= 0
	case "in":
		return Contains(b, a)
	case "notin":
		return !Contains(b, a)
	}
	panic("gojinja2/runtime: 未知比较运算符 " + op)
}

// Contains 实现 Python 的 `item in container`.
func Contains(container, item any) bool {
	switch cv := container.(type) {
	case string:
		return strings.Contains(cv, Str(mustStrForIn(item, container)))
	case Markup:
		return strings.Contains(string(cv), Str(mustStrForIn(item, container)))
	case []any:
		return seqContains(cv, item)
	case Tuple:
		return seqContains(cv, item)
	case *Dict:
		return cv.Has(item)
	case *Undefined:
		if cv.Kind == UndefinedStrict {
			cv.Fail()
		}
		return false
	}
	// 反射兜底: slice / array / map
	items, ok := tryIterate(container)
	if !ok {
		RaiseType("argument of type " + PyStrRepr(PyTypeName(container)) + " is not iterable")
	}
	return seqContains(items, item)
}

func mustStrForIn(item, container any) any {
	switch item.(type) {
	case string, Markup:
		return item
	}
	RaiseType("'in <string>' requires string as left operand, not " +
		PyTypeName(item))
	return nil
}

func seqContains(seq []any, item any) bool {
	for _, v := range seq {
		if Equal(v, item) {
			return true
		}
	}
	return false
}

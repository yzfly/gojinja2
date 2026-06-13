package runtime

import (
	"fmt"
	"reflect"
)

// PassArg 标记可调用对象需要注入的首参, 对应 jinja2 的
// pass_context / pass_eval_context / pass_environment.
type PassArg int

const (
	PassNone PassArg = iota
	PassContext
	PassEvalContext
	PassEnvironment
)

// Func 是引擎原生函数 (filters/tests/globals 使用的统一签名).
type Func struct {
	Name string
	Pass PassArg
	// Fn 接收位置参数与关键字参数. kwargs 可为 nil.
	Fn func(args []any, kwargs *Dict) any
}

func (f *Func) Call(args []any, kwargs *Dict) any { return f.Fn(args, kwargs) }

func (f *Func) PyRepr() string { return "<function " + f.Name + ">" }

// goFunc 包装反射得到的 Go 方法/函数.
type goFunc struct {
	fn   reflect.Value
	name string
}

func (g goFunc) PyRepr() string { return "<built-in method " + g.name + ">" }

func (g goFunc) Call(args []any, kwargs *Dict) any {
	if kwargs != nil && kwargs.Len() > 0 {
		RaiseType(g.name + "() takes no keyword arguments")
	}
	return CallGoFunc(g.fn, args)
}

// CallGoFunc 通过反射调用任意 Go 函数, 按位置绑定参数.
// 返回值约定: (T), (T, error), (T, bool), error 或无返回.
func CallGoFunc(fn reflect.Value, args []any) any {
	t := fn.Type()
	numIn := t.NumIn()
	in := make([]reflect.Value, 0, len(args))
	for i, a := range args {
		var paramType reflect.Type
		if t.IsVariadic() && i >= numIn-1 {
			paramType = t.In(numIn - 1).Elem()
		} else if i < numIn {
			paramType = t.In(i)
		} else {
			RaiseType(fmt.Sprintf("too many arguments in call (expected %d, got %d)",
				numIn, len(args)))
		}
		in = append(in, convertArg(a, paramType))
	}
	if !t.IsVariadic() && len(in) < numIn {
		RaiseType(fmt.Sprintf("missing arguments in call (expected %d, got %d)",
			numIn, len(args)))
	}
	out := fn.Call(in)
	switch len(out) {
	case 0:
		return nil
	case 1:
		if err, ok := out[0].Interface().(error); ok {
			panic(err)
		}
		return normalizeGoValue(out[0].Interface())
	default:
		// (T, error) / (T, bool)
		second := out[1].Interface()
		if err, ok := second.(error); ok && err != nil {
			panic(err)
		}
		if okFlag, ok := second.(bool); ok && !okFlag {
			return NewUndefined("", "")
		}
		return normalizeGoValue(out[0].Interface())
	}
}

func convertArg(a any, paramType reflect.Type) reflect.Value {
	if a == nil {
		return reflect.Zero(paramType)
	}
	av := reflect.ValueOf(a)
	if av.Type().AssignableTo(paramType) {
		return av
	}
	if av.Type().ConvertibleTo(paramType) {
		return av.Convert(paramType)
	}
	RaiseType(fmt.Sprintf("argument type mismatch: cannot use %s as %s",
		PyTypeName(a), paramType))
	return reflect.Value{}
}

// normalizeGoValue 把 Go 原生数值类型规约为规范类型.
func normalizeGoValue(v any) any {
	switch tv := v.(type) {
	case int:
		return int64(tv)
	case int32:
		return int64(tv)
	case int16:
		return int64(tv)
	case int8:
		return int64(tv)
	case uint:
		return int64(tv)
	case uint64:
		return int64(tv)
	case uint32:
		return int64(tv)
	case uint16:
		return int64(tv)
	case uint8:
		return int64(tv)
	case float32:
		return float64(tv)
	}
	return v
}

// Call 调用任意可调用值.
func Call(obj any, args []any, kwargs *Dict) any {
	switch fn := obj.(type) {
	case Callable:
		return fn.Call(args, kwargs)
	case *Undefined:
		fn.Fail()
	}
	rv := reflect.ValueOf(obj)
	if rv.Kind() == reflect.Func {
		if kwargs != nil && kwargs.Len() > 0 {
			RaiseType("function takes no keyword arguments")
		}
		return CallGoFunc(rv, args)
	}
	RaiseType(PyStrRepr(PyTypeName(obj)) + " object is not callable")
	return nil
}

// IsCallable 判断对象是否可调用.
func IsCallable(obj any) bool {
	switch obj.(type) {
	case Callable:
		return true
	case *Undefined:
		return false
	}
	if obj == nil {
		return false
	}
	return reflect.ValueOf(obj).Kind() == reflect.Func
}

// BoundMethod 把内建方法绑定到接收者.
type BoundMethod struct {
	Name string
	Fn   func(args []any, kwargs *Dict) any
}

func (b *BoundMethod) Call(args []any, kwargs *Dict) any { return b.Fn(args, kwargs) }

func (b *BoundMethod) PyRepr() string { return "<built-in method " + b.Name + ">" }

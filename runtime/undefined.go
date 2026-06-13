package runtime

import (
	"fmt"
)

// UndefinedKind 区分 4 种 Undefined 行为.
type UndefinedKind int

const (
	UndefinedDefault UndefinedKind = iota
	UndefinedChainable
	UndefinedDebug
	UndefinedStrict
)

// UndefinedFactory 由 Environment 持有, 创建对应种类的 Undefined.
type UndefinedFactory func(hint string, obj any, name any) *Undefined

// NewUndefinedFactory 返回指定行为的工厂.
func NewUndefinedFactory(kind UndefinedKind) UndefinedFactory {
	return func(hint string, obj any, name any) *Undefined {
		return &Undefined{Hint: hint, Obj: obj, Name: name, Kind: kind}
	}
}

// Undefined 对应 jinja2.runtime.Undefined 族.
// Obj 为 Missing 表示纯未定义名; Name 可以是 string 或任意键.
type Undefined struct {
	Hint string
	Obj  any
	Name any
	Kind UndefinedKind
}

// NewUndefined 创建默认类型的 Undefined (便捷函数).
func NewUndefined(hint string, name string) *Undefined {
	u := &Undefined{Hint: hint, Obj: Missing}
	if name != "" {
		u.Name = name
	}
	return u
}

func (u *Undefined) pyClassName() string {
	switch u.Kind {
	case UndefinedChainable:
		return "jinja2.runtime.ChainableUndefined"
	case UndefinedDebug:
		return "jinja2.runtime.DebugUndefined"
	case UndefinedStrict:
		return "jinja2.runtime.StrictUndefined"
	}
	return "jinja2.runtime.Undefined"
}

// message 对应 _undefined_message.
func (u *Undefined) message() string {
	if u.Hint != "" {
		return u.Hint
	}
	if u.Obj == Missing {
		return fmt.Sprintf("%s is undefined", Repr(u.Name))
	}
	if _, isStr := u.Name.(string); !isStr {
		return fmt.Sprintf("%s has no element %s", ObjectTypeRepr(u.Obj), Repr(u.Name))
	}
	return fmt.Sprintf("%s has no attribute %s",
		PyStrRepr(ObjectTypeRepr(u.Obj)), Repr(u.Name))
}

// Fail 抛出 UndefinedError, 对应 _fail_with_undefined_error.
func (u *Undefined) Fail() {
	RaiseUndefined(u.message())
}

func (u *Undefined) truth() bool {
	if u.Kind == UndefinedStrict {
		u.Fail()
	}
	return false
}

func (u *Undefined) str() string {
	switch u.Kind {
	case UndefinedStrict:
		u.Fail()
	case UndefinedDebug:
		var message string
		if u.Hint != "" {
			message = "undefined value printed: " + u.Hint
		} else if u.Obj == Missing {
			message = fmt.Sprint(u.Name)
		} else {
			message = fmt.Sprintf("no such element: %s[%s]",
				ObjectTypeRepr(u.Obj), Repr(u.Name))
		}
		return "{{ " + message + " }}"
	}
	return ""
}

func (u *Undefined) repr() string { return "Undefined" }

// Equals 对应 __eq__: 同类型 (同 Kind) 即相等; strict 直接失败.
func (u *Undefined) Equals(other any) bool {
	if u.Kind == UndefinedStrict {
		u.Fail()
	}
	if o, ok := other.(*Undefined); ok {
		if o.Kind == UndefinedStrict {
			o.Fail()
		}
		return o.Kind == u.Kind
	}
	return false
}

// Length 对应 __len__: 默认 0, strict 失败.
func (u *Undefined) Length() int {
	if u.Kind == UndefinedStrict {
		u.Fail()
	}
	return 0
}

// GetAttr 对应 __getattr__ 语义 (dunder 之外).
// chainable 返回自身, 其余失败.
func (u *Undefined) GetAttr(name any) any {
	if u.Kind == UndefinedChainable {
		return u
	}
	u.Fail()
	return nil
}

// GetItem 对应 __getitem__.
func (u *Undefined) GetItem(key any) any {
	if u.Kind == UndefinedChainable {
		return u
	}
	u.Fail()
	return nil
}

// Call 实现 Callable: 调用 undefined 总是失败.
func (u *Undefined) Call(args []any, kwargs *Dict) any {
	u.Fail()
	return nil
}

// IterItems: 迭代协议 (默认空序列, strict 失败).
func (u *Undefined) IterItems() []any {
	if u.Kind == UndefinedStrict {
		u.Fail()
	}
	return nil
}

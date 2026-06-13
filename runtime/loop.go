package runtime

import "fmt"

// LoopRenderFunc 渲染递归 loop 的回调.
type LoopRenderFunc func(iterable any, depth int) string

// LoopContext 对应 jinja2.runtime.LoopContext.
// 解释器将可迭代对象物化为切片, 因此 length 恒为已知.
type LoopContext struct {
	items            []any
	Index0           int
	undefined        UndefinedFactory
	recurse          LoopRenderFunc
	Depth0           int
	lastChangedValue any
	hasChangedValue  bool
}

func NewLoopContext(items []any, undefined UndefinedFactory, recurse LoopRenderFunc, depth0 int) *LoopContext {
	return &LoopContext{items: items, Index0: -1, undefined: undefined,
		recurse: recurse, Depth0: depth0}
}

// Next 前进一步; 越界返回 false.
func (l *LoopContext) Next() (any, bool) {
	if l.Index0+1 >= len(l.items) {
		return nil, false
	}
	l.Index0++
	return l.items[l.Index0], true
}

// Attr 提供 loop.xxx 的属性访问.
func (l *LoopContext) Attr(name string) (any, bool) {
	switch name {
	case "index0":
		return int64(l.Index0), true
	case "index":
		return int64(l.Index0 + 1), true
	case "revindex0":
		return int64(len(l.items) - l.Index0 - 1), true
	case "revindex":
		return int64(len(l.items) - l.Index0), true
	case "first":
		return l.Index0 == 0, true
	case "last":
		return l.Index0 == len(l.items)-1, true
	case "length":
		return int64(len(l.items)), true
	case "depth":
		return int64(l.Depth0 + 1), true
	case "depth0":
		return int64(l.Depth0), true
	case "previtem":
		if l.Index0 == 0 {
			return l.undefined("there is no previous item", Missing, ""), true
		}
		return l.items[l.Index0-1], true
	case "nextitem":
		if l.Index0 >= len(l.items)-1 {
			return l.undefined("there is no next item", Missing, ""), true
		}
		return l.items[l.Index0+1], true
	case "cycle":
		return &BoundMethod{Name: "cycle", Fn: func(args []any, kwargs *Dict) any {
			if len(args) == 0 {
				RaiseType("no items for cycling given")
			}
			return args[l.Index0%len(args)]
		}}, true
	case "changed":
		return &BoundMethod{Name: "changed", Fn: func(args []any, kwargs *Dict) any {
			val := Tuple(args)
			if !l.hasChangedValue || !Equal(l.lastChangedValue, val) {
				l.hasChangedValue = true
				l.lastChangedValue = val
				return true
			}
			return false
		}}, true
	}
	return nil, false
}

// Call 实现递归 loop 调用: {{ loop(children) }}.
func (l *LoopContext) Call(args []any, kwargs *Dict) any {
	if l.recurse == nil {
		RaiseType("The loop must have the 'recursive' marker to be called recursively.")
	}
	if len(args) != 1 {
		RaiseType("loop() takes exactly one argument")
	}
	return l.recurse(args[0], l.Depth0+1)
}

// Length 实现 len(loop).
func (l *LoopContext) Len() int { return len(l.items) }

func (l *LoopContext) PyRepr() string {
	return fmt.Sprintf("<LoopContext %d/%d>", l.Index0+1, len(l.items))
}

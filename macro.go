package gojinja2

import (
	"strings"

	"github.com/yzfly/gojinja2/nodes"
	"github.com/yzfly/gojinja2/runtime"
)

// macroValue 对应 jinja2.runtime.Macro: 闭包捕获定义处的作用域.
type macroValue struct {
	state    *evalState // 定义时的渲染状态 (env/context)
	defFrame *frame     // 闭包
	name     string
	argNames []string
	// 默认值表达式与 argNames 尾部对齐; 在调用时于 macro 帧内求值
	// (Python 编译器语义: 默认值可引用前面的参数)
	defaults []nodes.Expr
	body     []nodes.Node

	catchKwargs    bool // body 引用了 kwargs
	catchVarargs   bool // body 引用了 varargs
	usesCaller     bool // body 引用了 caller
	explicitCaller bool // caller 在参数表中
}

func newMacro(s *evalState, fr *frame, name string, args []*nodes.Name,
	defaultExprs []nodes.Expr, body []nodes.Node) *macroValue {
	m := &macroValue{state: s, defFrame: fr, name: name, body: body,
		defaults: defaultExprs}
	for _, a := range args {
		m.argNames = append(m.argNames, a.Name)
		if a.Name == "caller" {
			m.explicitCaller = true
		}
	}
	// 检测 body 是否使用 caller / kwargs / varargs (对应编译器的
	// undeclared 名称分析的近似)
	for _, n := range body {
		nodes.Walk(n, func(c nodes.Node) bool {
			if name, ok := c.(*nodes.Name); ok && name.Ctx == "load" {
				switch name.Name {
				case "caller":
					m.usesCaller = true
				case "kwargs":
					m.catchKwargs = true
				case "varargs":
					m.catchVarargs = true
				}
			}
			return true
		})
	}
	return m
}

// Call 实现 runtime.Callable.
func (m *macroValue) Call(args []any, kwargs *runtime.Dict) any {
	return m.invoke(m.state, args, kwargs)
}

// invoke 按 Macro.__call__ 的语义绑定参数并渲染.
// callState 提供调用方的 autoescape.
func (m *macroValue) invoke(callState *evalState, args []any, kwargs *runtime.Dict) any {
	if kwargs == nil {
		kwargs = runtime.NewDict()
	} else {
		kwargs = kwargs.Copy() // pop 操作不影响调用方
	}
	argc := len(m.argNames)

	// 消费位置参数
	bound := make([]any, 0, argc)
	n := len(args)
	if n > argc {
		n = argc
	}
	bound = append(bound, args[:n]...)

	foundCaller := false
	if len(bound) != argc {
		for _, name := range m.argNames[len(bound):] {
			var value any = runtime.Missing
			if v, ok := kwargs.Get(name); ok {
				value = v
				kwargs.Delete(name)
			}
			if name == "caller" {
				foundCaller = true
			}
			bound = append(bound, value)
		}
	} else {
		foundCaller = m.explicitCaller
	}

	// caller 特殊参数: 顺序为 caller, kwargs, varargs (与编译器一致)
	usesCallerArg := m.usesCaller || m.explicitCaller
	var callerValue any
	if usesCallerArg && !foundCaller {
		if v, ok := kwargs.Get("caller"); ok {
			callerValue = v
			kwargs.Delete("caller")
		} else {
			callerValue = m.state.ctx.undef("No caller defined", runtime.Missing, "caller")
		}
	}

	var kwargsValue *runtime.Dict
	if m.catchKwargs {
		kwargsValue = kwargs
	} else if kwargs.Len() > 0 {
		if kwargs.Has("caller") {
			runtime.RaiseType("macro " + runtime.PyStrRepr(m.name) +
				" was invoked with two values for the special caller argument. This is most likely a bug.")
		}
		first := kwargs.Keys()[0]
		runtime.RaiseType("macro " + runtime.PyStrRepr(m.name) +
			" takes no keyword argument " + runtime.Repr(first))
	}

	var varargsValue runtime.Tuple
	if m.catchVarargs {
		if len(args) > argc {
			varargsValue = runtime.Tuple(args[argc:])
		} else {
			varargsValue = runtime.Tuple{}
		}
	} else if len(args) > argc {
		runtime.RaiseType("macro " + runtime.PyStrRepr(m.name) +
			" takes not more than " + itoa(argc) + " argument(s)")
	}

	// 渲染 body: 新作用域, 闭包到定义处.
	// 缺省参数按声明顺序求值, 可引用前面的参数 (与编译器一致).
	fr := newFrame(m.defFrame, m.state.ctx)
	defStart := len(m.argNames) - len(m.defaults)
	for i, name := range m.argNames {
		fr.vars[name] = bound[i]
	}
	for i, name := range m.argNames {
		if bound[i] != runtime.Missing {
			continue
		}
		if i >= defStart {
			fr.vars[name] = m.state.evalExpr(m.defaults[i-defStart], fr)
		} else {
			fr.vars[name] = m.state.ctx.undef(
				"parameter "+runtime.PyStrRepr(name)+" was not provided",
				runtime.Missing, name)
		}
	}
	if usesCallerArg && !foundCaller {
		fr.vars["caller"] = callerValue
	}
	if m.catchKwargs {
		fr.vars["kwargs"] = kwargsValue
	}
	if m.catchVarargs {
		fr.vars["varargs"] = varargsValue
	}

	s := m.state
	saved := s.out
	savedSuppress := s.suppressOutput
	s.out = &strings.Builder{}
	s.suppressOutput = false
	s.execBlock(m.body, fr)
	rv := s.out.String()
	s.out = saved
	s.suppressOutput = savedSuppress

	// autoescape 取调用方的 eval context
	autoescape := m.state.ctx.autoescape
	if callState != nil {
		autoescape = callState.ctx.autoescape
	}
	if autoescape {
		return runtime.Markup(rv)
	}
	return rv
}

// JinjaGetAttr 暴露 Macro 的元属性 (name/arguments/...).
func (m *macroValue) JinjaGetAttr(name string) (any, bool) {
	switch name {
	case "name":
		return m.name, true
	case "arguments":
		args := make(runtime.Tuple, len(m.argNames))
		for i, a := range m.argNames {
			args[i] = a
		}
		return args, true
	case "catch_kwargs":
		return m.catchKwargs, true
	case "catch_varargs":
		return m.catchVarargs, true
	case "caller":
		return m.usesCaller || m.explicitCaller, true
	}
	return nil, false
}

func (m *macroValue) PyRepr() string {
	if m.name == "" {
		return "<Macro anonymous>"
	}
	return "<Macro " + runtime.PyStrRepr(m.name) + ">"
}

func (s *evalState) execMacroDef(st *nodes.Macro, fr *frame) {
	m := newMacro(s, fr, st.Name, st.Args, st.Defaults, st.Body)
	fr.set(st.Name, m)
}

// execCallBlock 实现 {% call %}: body 包装为匿名 macro 作为 caller 传入.
func (s *evalState) execCallBlock(st *nodes.CallBlock, fr *frame) {
	if s.outputSuppressed() {
		return
	}
	caller := newMacro(s, fr, "caller", st.Args, st.Defaults, st.Body)

	// 调用并注入 caller kwarg
	callee := s.evalExpr(st.Call.Node, fr)
	args, kwargs := s.evalArgs(st.Call.Args, st.Call.Kwargs,
		st.Call.DynArgs, st.Call.DynKwargs, fr)
	if kwargs == nil {
		kwargs = runtime.NewDict()
	}
	kwargs.Set("caller", caller)
	rv := s.contextCall(callee, args, kwargs, fr)

	if s.ctx.autoescape {
		s.out.WriteString(string(runtime.Escape(rv)))
	} else {
		s.out.WriteString(runtime.Str(rv))
	}
}

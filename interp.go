package gojinja2

import (
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
	"github.com/yzfly/gojinja2/nodes"
	"github.com/yzfly/gojinja2/runtime"
)

// context 对应 jinja2.runtime.Context: 模板执行的变量容器.
type context struct {
	env        *Environment
	name       string
	vars       map[string]any // 顶层 set 的变量
	parent     map[string]any // globals + 渲染入参 (只读)
	exported   map[string]bool
	autoescape bool
	blocks     map[string][]*nodes.Block // 块继承栈 (M3)
	undef      runtime.UndefinedFactory
}

// frame 是局部作用域 (for/with/macro/block 等).
// parent == nil 表示根作用域, 写操作落到 context.vars.
type frame struct {
	parent *frame
	ctx    *context
	vars   map[string]any
}

func newFrame(parent *frame, ctx *context) *frame {
	return &frame{parent: parent, ctx: ctx, vars: map[string]any{}}
}

// resolve 实现变量解析链.
func (f *frame) resolve(name string) (any, bool) {
	for fr := f; fr != nil; fr = fr.parent {
		if v, ok := fr.vars[name]; ok {
			if v == runtime.Missing {
				return nil, false
			}
			return v, true
		}
	}
	if v, ok := f.ctx.vars[name]; ok {
		return v, true
	}
	if v, ok := f.ctx.parent[name]; ok {
		return v, true
	}
	if v, ok := f.ctx.env.Globals[name]; ok {
		return v, true
	}
	return nil, false
}

// set 写变量: 根作用域写入 context 并导出.
func (f *frame) set(name string, value any) {
	if f.parent == nil {
		f.ctx.vars[name] = value
		f.ctx.exported[name] = true
		return
	}
	f.vars[name] = value
}

// evalState 携带一次渲染的全部状态.
type evalState struct {
	ctx  *context
	out  *strings.Builder
	tmpl *Template

	// 继承机制
	blockStack      map[string][]blockEntry
	parentTemplate  *Template
	currentTemplate *Template
	suppressOutput  bool // 顶层 extends 抑制子模板的直接输出
	extendsSeen     bool
	depth           int // include/extends 深度保护
}

// outputSuppressed 对应 Python 的 `if parent_template is None` 输出保护.
func (s *evalState) outputSuppressed() bool {
	return s.suppressOutput || s.parentTemplate != nil
}

// ---- 语句求值 ----

func (s *evalState) execBlock(body []nodes.Node, fr *frame) {
	for _, n := range body {
		s.execStmt(n, fr)
	}
}

func (s *evalState) execStmt(n nodes.Node, fr *frame) {
	switch st := n.(type) {
	case *nodes.Output:
		s.execOutput(st, fr)
	case *nodes.If:
		s.execIf(st, fr)
	case *nodes.For:
		s.execFor(st, fr)
	case *nodes.Assign:
		s.assign(st.Target, s.evalExpr(st.Node, fr), fr)
	case *nodes.AssignBlock:
		s.execAssignBlock(st, fr)
	case *nodes.With:
		s.execWith(st, fr)
	case *nodes.FilterBlock:
		s.execFilterBlock(st, fr)
	case *nodes.Scope:
		inner := newFrame(fr, s.ctx)
		s.execBlock(st.Body, inner)
	case *nodes.ScopedEvalContextModifier:
		s.execAutoescapeMod(st, fr)
	case *nodes.ExprStmt:
		s.evalExpr(st.Node, fr)
	case *nodes.Macro:
		s.execMacroDef(st, fr)
	case *nodes.CallBlock:
		s.execCallBlock(st, fr)
	case *nodes.Block:
		s.execBlockStmt(st, fr)
	case *nodes.Extends:
		s.execExtends(st, fr)
	case *nodes.Include:
		s.execInclude(st, fr)
	case *nodes.Import:
		s.execImport(st, fr)
	case *nodes.FromImport:
		s.execFromImport(st, fr)
	case *nodes.Break:
		panic(loopBreak{})
	case *nodes.Continue:
		panic(loopContinue{})
	default:
		s.fail("未支持的语句节点: "+nodes.TypeName(n), n.Lineno())
	}
}

type loopBreak struct{}
type loopContinue struct{}

func (s *evalState) fail(msg string, lineno int) {
	panic(&exceptions.TemplateRuntimeError{Message: msg})
}

func (s *evalState) execOutput(st *nodes.Output, fr *frame) {
	if s.outputSuppressed() {
		return
	}
	for _, expr := range st.Nodes {
		if td, ok := expr.(*nodes.TemplateData); ok {
			s.out.WriteString(td.Data)
			continue
		}
		v := s.evalExpr(expr, fr)
		if s.ctx.env.Finalize != nil {
			v = s.ctx.env.Finalize(v)
		}
		if s.ctx.autoescape {
			s.out.WriteString(string(runtime.Escape(v)))
		} else {
			s.out.WriteString(runtime.Str(v))
		}
	}
}

func (s *evalState) execIf(st *nodes.If, fr *frame) {
	if runtime.Truth(s.evalExpr(st.Test, fr)) {
		s.execBlock(st.Body, fr)
		return
	}
	for _, elif := range st.Elif {
		if runtime.Truth(s.evalExpr(elif.Test, fr)) {
			s.execBlock(elif.Body, fr)
			return
		}
	}
	s.execBlock(st.Else, fr)
}

func (s *evalState) execFor(st *nodes.For, fr *frame) {
	iter := s.evalExpr(st.Iter, fr)
	items := runtime.Iterate(iter)

	// 循环过滤条件: 在绑定目标后的临时作用域中求值
	if st.Test != nil {
		var filtered []any
		for _, item := range items {
			testFrame := newFrame(fr, s.ctx)
			s.assignLocal(st.Target, item, testFrame)
			if runtime.Truth(s.evalExpr(st.Test, testFrame)) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	if len(items) == 0 {
		if len(st.Else) > 0 {
			inner := newFrame(fr, s.ctx)
			s.execBlock(st.Else, inner)
		}
		return
	}

	s.runLoop(st, items, fr, 0)
}

func (s *evalState) runLoop(st *nodes.For, items []any, fr *frame, depth0 int) {
	var recurse runtime.LoopRenderFunc
	if st.Recursive {
		recurse = func(iterable any, depth int) string {
			sub := runtime.Iterate(iterable)
			if st.Test != nil {
				var filtered []any
				for _, item := range sub {
					testFrame := newFrame(fr, s.ctx)
					s.assignLocal(st.Target, item, testFrame)
					if runtime.Truth(s.evalExpr(st.Test, testFrame)) {
						filtered = append(filtered, item)
					}
				}
				sub = filtered
			}
			saved := s.out
			s.out = &strings.Builder{}
			s.runLoop(st, sub, fr, depth)
			rendered := s.out.String()
			s.out = saved
			if s.ctx.autoescape {
				return string(runtime.Markup(rendered))
			}
			return rendered
		}
	}

	loop := runtime.NewLoopContext(items, s.ctx.undef, recurse, depth0)
	for {
		item, ok := loop.Next()
		if !ok {
			break
		}
		inner := newFrame(fr, s.ctx)
		s.assignLocal(st.Target, item, inner)
		inner.vars["loop"] = loop
		broke := func() (broke bool) {
			defer func() {
				if r := recover(); r != nil {
					switch r.(type) {
					case loopBreak:
						broke = true
					case loopContinue:
						broke = false
					default:
						panic(r)
					}
				}
			}()
			s.execBlock(st.Body, inner)
			return false
		}()
		if broke {
			break
		}
	}
}

// assign 处理赋值目标 (Name / NSRef / Tuple).
func (s *evalState) assign(target nodes.Expr, value any, fr *frame) {
	switch t := target.(type) {
	case *nodes.Name:
		fr.set(t.Name, value)
	case *nodes.NSRef:
		base, ok := fr.resolve(t.Name)
		if !ok {
			s.ctx.undef("", runtime.Missing, t.Name).Fail()
		}
		ns, isNS := base.(*runtime.Namespace)
		if !isNS {
			panic(&exceptions.TemplateRuntimeError{
				Message: "cannot assign attribute on non-namespace object"})
		}
		ns.SetAttr(t.Attr, value)
	case *nodes.Tuple:
		s.unpack(t, value, fr, fr.set)
	default:
		s.fail("无法赋值到 "+nodes.TypeName(target), target.Lineno())
	}
}

// assignLocal 把值绑定到当前 frame (for/with 的目标).
func (s *evalState) assignLocal(target nodes.Expr, value any, fr *frame) {
	switch t := target.(type) {
	case *nodes.Name:
		fr.vars[t.Name] = value
	case *nodes.Tuple:
		s.unpack(t, value, fr, func(name string, v any) { fr.vars[name] = v })
	default:
		s.fail("无法赋值到 "+nodes.TypeName(target), target.Lineno())
	}
}

func (s *evalState) unpack(t *nodes.Tuple, value any, fr *frame, set func(string, any)) {
	items := runtime.Iterate(value)
	if len(items) < len(t.Items) {
		panic(&exceptions.TemplateRuntimeError{Message:
			"not enough values to unpack (expected " + itoa(len(t.Items)) +
				", got " + itoa(len(items)) + ")"})
	}
	if len(items) > len(t.Items) {
		panic(&exceptions.TemplateRuntimeError{Message:
			"too many values to unpack (expected " + itoa(len(t.Items)) + ")"})
	}
	for i, sub := range t.Items {
		switch st := sub.(type) {
		case *nodes.Name:
			set(st.Name, items[i])
		case *nodes.Tuple:
			s.unpack(st, items[i], fr, set)
		default:
			s.fail("无法赋值到 "+nodes.TypeName(sub), sub.Lineno())
		}
	}
}

func (s *evalState) execAssignBlock(st *nodes.AssignBlock, fr *frame) {
	saved := s.out
	savedSuppress := s.suppressOutput
	s.out = &strings.Builder{}
	s.suppressOutput = false
	inner := newFrame(fr, s.ctx)
	s.execBlock(st.Body, inner)
	var rv any = s.out.String()
	s.out = saved
	s.suppressOutput = savedSuppress

	if st.Filter != nil {
		rv = s.applyFilterSeed(st.Filter, rv, fr)
	}
	if s.ctx.autoescape {
		rv = runtime.Markup(runtime.Str(rv))
	}
	s.assign(st.Target, rv, fr)
}

// applyFilterSeed 把捕获的内容作为 filter 链最内层的输入
// ({% filter %} 与 {% set x | f %} 的链最内层 Node 为 nil).
func (s *evalState) applyFilterSeed(ex *nodes.Filter, seed any, fr *frame) any {
	var value any
	if nodes.IsNil(ex.Node) {
		value = seed
	} else if inner, ok := ex.Node.(*nodes.Filter); ok {
		value = s.applyFilterSeed(inner, seed, fr)
	} else {
		value = s.evalExpr(ex.Node, fr)
	}
	return s.evalFilter(ex, value, fr)
}

func (s *evalState) execWith(st *nodes.With, fr *frame) {
	inner := newFrame(fr, s.ctx)
	for i, target := range st.Targets {
		// 值表达式在外层作用域求值: {% with a=1, b=a %} 中 b 看不到新 a
		value := s.evalExpr(st.Values[i], fr)
		s.assignLocal(target, value, inner)
	}
	s.execBlock(st.Body, inner)
}

func (s *evalState) execFilterBlock(st *nodes.FilterBlock, fr *frame) {
	if s.outputSuppressed() {
		return
	}
	saved := s.out
	s.out = &strings.Builder{}
	inner := newFrame(fr, s.ctx)
	s.execBlock(st.Body, inner)
	var rv any = s.out.String()
	if s.ctx.autoescape {
		rv = runtime.Markup(runtime.Str(rv))
	}
	s.out = saved
	rv = s.applyFilterSeed(st.Filter, rv, fr)
	if s.ctx.autoescape {
		s.out.WriteString(string(runtime.Escape(rv)))
	} else {
		s.out.WriteString(runtime.Str(rv))
	}
}

func (s *evalState) execAutoescapeMod(st *nodes.ScopedEvalContextModifier, fr *frame) {
	saved := s.ctx.autoescape
	defer func() { s.ctx.autoescape = saved }()
	for _, opt := range st.Options {
		if opt.Key == "autoescape" {
			s.ctx.autoescape = runtime.Truth(s.evalExpr(opt.Value, fr))
		}
	}
	s.execBlock(st.Body, fr)
}

// ---- 表达式求值 ----

func (s *evalState) evalExpr(e nodes.Expr, fr *frame) any {
	switch ex := e.(type) {
	case *nodes.Const:
		return constValue(ex.Value)
	case *nodes.TemplateData:
		if s.ctx.autoescape {
			return runtime.Markup(ex.Data)
		}
		return ex.Data
	case *nodes.Name:
		if v, ok := fr.resolve(ex.Name); ok {
			return v
		}
		return s.ctx.undef("", runtime.Missing, ex.Name)
	case *nodes.NSRef:
		base, ok := fr.resolve(ex.Name)
		if !ok {
			return s.ctx.undef("", runtime.Missing, ex.Name)
		}
		return runtime.GetAttr(s.ctx.undef, base, ex.Attr)
	case *nodes.Tuple:
		items := make(runtime.Tuple, len(ex.Items))
		for i, item := range ex.Items {
			items[i] = s.evalExpr(item, fr)
		}
		return items
	case *nodes.List:
		items := make([]any, len(ex.Items))
		for i, item := range ex.Items {
			items[i] = s.evalExpr(item, fr)
		}
		return items
	case *nodes.Dict:
		d := runtime.NewDict()
		for _, pair := range ex.Items {
			d.Set(s.evalExpr(pair.Key, fr), s.evalExpr(pair.Value, fr))
		}
		return d
	case *nodes.BinExpr:
		return s.evalBinExpr(ex, fr)
	case *nodes.UnaryExpr:
		switch ex.Op {
		case "not":
			return !runtime.Truth(s.evalExpr(ex.Node, fr))
		case "-":
			return runtime.Neg(s.evalExpr(ex.Node, fr))
		case "+":
			return runtime.Pos(s.evalExpr(ex.Node, fr))
		}
	case *nodes.CondExpr:
		if runtime.Truth(s.evalExpr(ex.Test, fr)) {
			return s.evalExpr(ex.Expr1, fr)
		}
		if ex.Expr2 != nil {
			return s.evalExpr(ex.Expr2, fr)
		}
		return s.ctx.undef("the inline if-expression on line "+itoa(ex.Line)+
			" evaluated to false and no else section was defined.", runtime.Missing, nil)
	case *nodes.Compare:
		return s.evalCompare(ex, fr)
	case *nodes.Concat:
		items := make([]any, len(ex.Nodes))
		for i, n := range ex.Nodes {
			items[i] = s.evalExpr(n, fr)
		}
		if s.ctx.autoescape {
			return runtime.MarkupJoin(items)
		}
		return runtime.StrJoin(items)
	case *nodes.Getattr:
		obj := s.evalExpr(ex.Node, fr)
		return runtime.GetAttr(s.ctx.undef, obj, ex.Attr)
	case *nodes.Getitem:
		obj := s.evalExpr(ex.Node, fr)
		arg := s.evalSubscript(ex.Arg, fr)
		return runtime.GetItem(s.ctx.undef, obj, arg)
	case *nodes.Slice:
		return s.evalSlice(ex, fr)
	case *nodes.Call:
		return s.evalCall(ex, fr)
	case *nodes.Filter:
		var value any
		if ex.Node != nil {
			value = s.evalExpr(ex.Node, fr)
		}
		return s.evalFilter(ex, value, fr)
	case *nodes.Test:
		return s.evalTest(ex, fr)
	case *nodes.MarkSafe:
		return runtime.Markup(runtime.Str(s.evalExpr(ex.Expr, fr)))
	case *nodes.MarkSafeIfAutoescape:
		v := s.evalExpr(ex.Expr, fr)
		if s.ctx.autoescape {
			return runtime.Markup(runtime.Str(v))
		}
		return v
	}
	s.fail("未支持的表达式节点: "+nodes.TypeName(e), e.Lineno())
	return nil
}

// constValue 把解析期常量转为运行时值 (字符串常量在 autoescape 下不转 Markup,
// 与 Python 一致: 常量折叠时按原值).
func constValue(v any) any { return v }

func (s *evalState) evalBinExpr(ex *nodes.BinExpr, fr *frame) any {
	switch ex.Op {
	case "and":
		left := s.evalExpr(ex.Left, fr)
		if !runtime.Truth(left) {
			return left
		}
		return s.evalExpr(ex.Right, fr)
	case "or":
		left := s.evalExpr(ex.Left, fr)
		if runtime.Truth(left) {
			return left
		}
		return s.evalExpr(ex.Right, fr)
	}
	a := s.evalExpr(ex.Left, fr)
	b := s.evalExpr(ex.Right, fr)
	switch ex.Op {
	case "+":
		return runtime.Add(a, b)
	case "-":
		return runtime.Sub(a, b)
	case "*":
		return runtime.Mul(a, b)
	case "/":
		return runtime.TrueDiv(a, b)
	case "//":
		return runtime.FloorDiv(a, b)
	case "%":
		return runtime.Mod(a, b)
	case "**":
		return runtime.Pow(a, b)
	}
	s.fail("未知二元运算符 "+ex.Op, ex.Line)
	return nil
}

func (s *evalState) evalCompare(ex *nodes.Compare, fr *frame) any {
	left := s.evalExpr(ex.Expr, fr)
	for _, op := range ex.Ops {
		right := s.evalExpr(op.Expr, fr)
		if !runtime.CompareOp(op.Op, left, right) {
			return false
		}
		left = right
	}
	return true
}

func (s *evalState) evalSubscript(e nodes.Expr, fr *frame) any {
	if sl, ok := e.(*nodes.Slice); ok {
		return s.evalSlice(sl, fr)
	}
	return s.evalExpr(e, fr)
}

func (s *evalState) evalSlice(ex *nodes.Slice, fr *frame) any {
	sl := &runtime.PySlice{}
	if ex.Start != nil {
		sl.Start = s.evalExpr(ex.Start, fr)
	}
	if ex.Stop != nil {
		sl.Stop = s.evalExpr(ex.Stop, fr)
	}
	if ex.Step != nil {
		sl.Step = s.evalExpr(ex.Step, fr)
	}
	return sl
}

// evalArgs 求值调用参数: 位置参数 + *args + kwargs + **kwargs.
func (s *evalState) evalArgs(argNodes []nodes.Expr, kwargNodes []*nodes.Keyword,
	dynArgs, dynKwargs nodes.Expr, fr *frame) ([]any, *runtime.Dict) {
	args := make([]any, 0, len(argNodes))
	for _, a := range argNodes {
		args = append(args, s.evalExpr(a, fr))
	}
	if dynArgs != nil {
		args = append(args, runtime.Iterate(s.evalExpr(dynArgs, fr))...)
	}
	var kwargs *runtime.Dict
	if len(kwargNodes) > 0 || dynKwargs != nil {
		kwargs = runtime.NewDict()
		for _, kw := range kwargNodes {
			kwargs.Set(kw.Key, s.evalExpr(kw.Value, fr))
		}
		if dynKwargs != nil {
			dv := s.evalExpr(dynKwargs, fr)
			d, ok := dv.(*runtime.Dict)
			if !ok {
				runtime.RaiseType("argument after ** must be a mapping, not " +
					runtime.PyTypeName(dv))
			}
			for _, it := range d.Items() {
				kwargs.Set(it.Key, it.Value)
			}
		}
	}
	return args, kwargs
}

func (s *evalState) evalCall(ex *nodes.Call, fr *frame) any {
	callee := s.evalExpr(ex.Node, fr)
	args, kwargs := s.evalArgs(ex.Args, ex.Kwargs, ex.DynArgs, ex.DynKwargs, fr)
	return s.contextCall(callee, args, kwargs, fr)
}

// contextCall 对应 Context.call: 注入 pass_context 等首参.
func (s *evalState) contextCall(callee any, args []any, kwargs *runtime.Dict, fr *frame) any {
	if fn, ok := callee.(*runtime.Func); ok && fn.Pass != runtime.PassNone {
		args = s.injectPassArg(fn.Pass, args, fr)
	}
	return runtime.Call(callee, args, kwargs)
}

func (s *evalState) injectPassArg(pass runtime.PassArg, args []any, fr *frame) []any {
	switch pass {
	case runtime.PassContext:
		return append([]any{&contextProxy{state: s, frame: fr}}, args...)
	case runtime.PassEvalContext:
		return append([]any{&evalCtxProxy{autoescape: s.ctx.autoescape, env: s.ctx.env}}, args...)
	case runtime.PassEnvironment:
		return append([]any{s.ctx.env}, args...)
	}
	return args
}

func (s *evalState) evalFilter(ex *nodes.Filter, value any, fr *frame) any {
	// 链式 filter: ex.Node 可能也是 Filter (在 evalExpr 已展开),
	// 这里 value 已是上游结果.
	fn, ok := s.ctx.env.Filters[ex.Name]
	if !ok {
		panic(&exceptions.TemplateRuntimeError{
			Message: "No filter named " + runtime.PyStrRepr(ex.Name) + "."})
	}
	args, kwargs := s.evalArgs(ex.Args, ex.Kwargs, ex.DynArgs, ex.DynKwargs, fr)
	all := append([]any{value}, args...)
	all = s.injectPassArg(fn.Pass, all, fr)
	return fn.Fn(all, kwargs)
}

func (s *evalState) evalTest(ex *nodes.Test, fr *frame) any {
	fn, ok := s.ctx.env.Tests[ex.Name]
	if !ok {
		panic(&exceptions.TemplateRuntimeError{
			Message: "No test named " + runtime.PyStrRepr(ex.Name) + "."})
	}
	value := s.evalExpr(ex.Node, fr)
	args, kwargs := s.evalArgs(ex.Args, ex.Kwargs, ex.DynArgs, ex.DynKwargs, fr)
	all := append([]any{value}, args...)
	all = s.injectPassArg(fn.Pass, all, fr)
	return runtime.Truth(fn.Fn(all, kwargs))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// contextProxy 暴露给 pass_context 函数的上下文视图.
type contextProxy struct {
	state *evalState
	frame *frame
}

// Resolve 供 filter 实现解析模板变量.
func (c *contextProxy) Resolve(name string) any {
	if v, ok := c.frame.resolve(name); ok {
		return v
	}
	return c.state.ctx.undef("", runtime.Missing, name)
}

// Environment 返回环境.
func (c *contextProxy) Environment() *Environment { return c.state.ctx.env }

// Autoescape 返回 eval context 的 autoescape.
func (c *contextProxy) Autoescape() bool { return c.state.ctx.autoescape }

// evalCtxProxy 暴露给 pass_eval_context 函数.
type evalCtxProxy struct {
	autoescape bool
	env        *Environment
}

func (e *evalCtxProxy) Autoescape() bool          { return e.autoescape }
func (e *evalCtxProxy) Environment() *Environment { return e.env }

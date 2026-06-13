package gojinja2

import (
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
	"github.com/yzfly/gojinja2/nodes"
	"github.com/yzfly/gojinja2/runtime"
)

// loadTemplateValue 把 extends/include/import 的模板表达式值解析为模板.
// 接受字符串, *Template 或名字列表.
func (s *evalState) loadTemplateValue(v any, ignoreMissing bool) *Template {
	switch tv := v.(type) {
	case *Template:
		return tv
	case string, runtime.Markup:
		t, err := s.ctx.env.GetTemplate(runtime.Str(tv))
		if err != nil {
			if ignoreMissing && isNotFound(err) {
				return nil
			}
			panic(err)
		}
		return t
	case []any:
		t, err := s.ctx.env.SelectTemplate(tv)
		if err != nil {
			if ignoreMissing && isNotFound(err) {
				return nil
			}
			panic(err)
		}
		return t
	case runtime.Tuple:
		return s.loadTemplateValue([]any(tv), ignoreMissing)
	case *runtime.Undefined:
		tv.Fail()
	}
	panic(&exceptions.TemplateRuntimeError{
		Message: "template name must be a string or list, got " + runtime.PyTypeName(v)})
}

// execExtends 对应 {% extends %}.
func (s *evalState) execExtends(st *nodes.Extends, fr *frame) {
	if s.extendsSeen || s.parentTemplate != nil {
		panic(&exceptions.TemplateRuntimeError{Message: "extended multiple times"})
	}
	v := s.evalExpr(st.Template, fr)
	parent := s.loadTemplateValue(v, false)
	s.parentTemplate = parent
	s.extendsSeen = true
	// 把父模板的块追加到继承栈 (子模板覆盖优先)
	for name, b := range parent.blocks {
		s.blockStack[name] = append(s.blockStack[name], blockEntry{block: b, tmpl: parent})
	}
}

// execBlockStmt 渲染块: 取继承栈顶 (最具体的覆盖).
func (s *evalState) execBlockStmt(st *nodes.Block, fr *frame) {
	if s.outputSuppressed() {
		return
	}
	entries := s.blockStack[st.Name]
	if len(entries) == 0 {
		entries = []blockEntry{{block: st, tmpl: s.currentTemplate}}
	}
	s.renderBlockEntry(st.Name, entries, 0, fr, st.Scoped)
}

func (s *evalState) renderBlockEntry(name string, entries []blockEntry, idx int,
	siteFrame *frame, scoped bool) {
	entry := entries[idx]
	if entry.block.Required && idx == 0 && len(entries) == 1 {
		panic(&exceptions.TemplateRuntimeError{
			Message: "Required block " + runtime.PyStrRepr(name) + " not found"})
	}
	// 块函数的作用域: 非 scoped 块只看到 context 级变量;
	// scoped 块额外看到块所在位置的局部变量 (loop 等).
	var fr *frame
	if scoped || entry.block.Scoped {
		fr = newFrame(siteFrame, s.ctx)
	} else {
		fr = &frame{parent: nil, ctx: s.ctx, vars: map[string]any{}}
		fr = newFrame(fr, s.ctx) // 包一层避免顶层写入逃逸
	}
	fr.vars["super"] = s.makeSuper(name, entries, idx, siteFrame, scoped)
	s.execBlock(entry.block.Body, fr)
}

// makeSuper 构造 super() 可调用对象 (支持 super.super).
func (s *evalState) makeSuper(name string, entries []blockEntry, idx int,
	siteFrame *frame, scoped bool) any {
	if idx+1 >= len(entries) {
		return s.ctx.undef("there is no parent block called "+runtime.PyStrRepr(name)+".",
			runtime.Missing, "super")
	}
	return &blockRefValue{state: s, name: name, entries: entries, idx: idx + 1,
		siteFrame: siteFrame, scoped: scoped}
}

// blockRefValue 对应 jinja2.runtime.BlockReference.
type blockRefValue struct {
	state     *evalState
	name      string
	entries   []blockEntry
	idx       int
	siteFrame *frame
	scoped    bool
}

func (b *blockRefValue) Call(args []any, kwargs *runtime.Dict) any {
	s := b.state
	saved := s.out
	savedSuppress := s.suppressOutput
	s.out = &strings.Builder{}
	s.suppressOutput = false
	s.renderBlockEntry(b.name, b.entries, b.idx, b.siteFrame, b.scoped)
	rv := s.out.String()
	s.out = saved
	s.suppressOutput = savedSuppress
	if s.ctx.autoescape {
		return runtime.Markup(rv)
	}
	return rv
}

// JinjaGetAttr 支持 super.super.
func (b *blockRefValue) JinjaGetAttr(name string) (any, bool) {
	if name != "super" {
		return nil, false
	}
	return b.state.makeSuper(b.name, b.entries, b.idx, b.siteFrame, b.scoped), true
}

func (b *blockRefValue) PyRepr() string {
	return "<BlockReference " + runtime.PyStrRepr(b.name) + ">"
}

// selfRefValue 对应 TemplateReference ({{ self.block_name() }}).
type selfRefValue struct {
	state *evalState
}

func (r *selfRefValue) JinjaGetAttr(name string) (any, bool) {
	entries := r.state.blockStack[name]
	if len(entries) == 0 {
		return nil, false
	}
	return &blockRefValue{state: r.state, name: name, entries: entries, idx: 0,
		siteFrame: nil, scoped: false}, true
}

func (r *selfRefValue) PyRepr() string {
	return "<TemplateReference " + runtime.PyStrRepr(r.state.ctx.name) + ">"
}

// execInclude 对应 {% include %}.
func (s *evalState) execInclude(st *nodes.Include, fr *frame) {
	if s.outputSuppressed() {
		return
	}
	v := s.evalExpr(st.Template, fr)
	t := s.loadTemplateValue(v, st.IgnoreMissing)
	if t == nil {
		return // ignore missing
	}
	if s.depth > 500 {
		panic(&exceptions.TemplateRuntimeError{Message: "include 嵌套过深 (疑似循环 include)"})
	}

	var parent map[string]any
	if st.WithContext {
		parent = s.ctx.getAll(fr)
	} else {
		parent = map[string]any{}
	}
	sub := &evalState{ctx: t.newContext(parent), out: s.out, tmpl: t,
		depth: s.depth + 1}
	sub.blockStack = map[string][]blockEntry{}
	for name, b := range t.blocks {
		sub.blockStack[name] = []blockEntry{{block: b, tmpl: t}}
	}
	sub.execRoot(t)
}

// getAll 收集当前可见的全部变量 (include with context 用).
func (c *context) getAll(fr *frame) map[string]any {
	out := map[string]any{}
	for k, v := range c.parent {
		out[k] = v
	}
	for k, v := range c.vars {
		out[k] = v
	}
	// frame 链上的局部变量 (近端覆盖远端)
	var frames []*frame
	for f := fr; f != nil; f = f.parent {
		frames = append(frames, f)
	}
	for i := len(frames) - 1; i >= 0; i-- {
		for k, v := range frames[i].vars {
			if v != runtime.Missing {
				out[k] = v
			}
		}
	}
	return out
}

// execImport 对应 {% import ... as x %}.
func (s *evalState) execImport(st *nodes.Import, fr *frame) {
	v := s.evalExpr(st.Template, fr)
	t := s.loadTemplateValue(v, false)
	mod := s.makeModule(t, st.WithContext, fr)
	fr.set(st.Target, mod)
}

// execFromImport 对应 {% from ... import a, b %}.
func (s *evalState) execFromImport(st *nodes.FromImport, fr *frame) {
	v := s.evalExpr(st.Template, fr)
	t := s.loadTemplateValue(v, false)
	mod := s.makeModule(t, st.WithContext, fr)
	for _, im := range st.Names {
		alias := im.Alias
		if alias == "" {
			alias = im.Name
		}
		if v, ok := mod.JinjaGetAttr(im.Name); ok {
			fr.set(alias, v)
		} else {
			fr.set(alias, s.ctx.undef(
				"the template "+runtime.PyStrRepr(t.Name)+
					" (imported on line "+itoa(st.Line)+") does not export the requested name "+
					runtime.PyStrRepr(im.Name), runtime.Missing, im.Name))
		}
	}
}

func (s *evalState) makeModule(t *Template, withContext bool, fr *frame) *templateModule {
	var parent map[string]any
	if withContext {
		parent = s.ctx.getAll(fr)
	} else {
		parent = map[string]any{}
	}
	return t.module(parent)
}

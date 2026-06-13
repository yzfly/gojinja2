package gojinja2

import (
	"github.com/yzfly/gojinja2/exceptions"
	"fmt"
	"strings"

	"github.com/yzfly/gojinja2/nodes"
	"github.com/yzfly/gojinja2/runtime"
)

// Template 对应 jinja2.Template.
type Template struct {
	env      *Environment
	Name     string
	Filename string
	ast      *nodes.Template
	blocks   map[string]*nodes.Block
	// hasTopLevelExtends: 编译期可确定的 extends (顶层), 输出全部抑制
	hasTopLevelExtends bool
	// Globals 是模板级全局变量 (get_template(globals=...))
	Globals map[string]any
}

func newTemplate(env *Environment, ast *nodes.Template, name, filename string) *Template {
	t := &Template{env: env, ast: ast, Name: name, Filename: filename,
		blocks: map[string]*nodes.Block{}}
	nodes.Walk(ast, func(n nodes.Node) bool {
		if b, ok := n.(*nodes.Block); ok {
			t.blocks[b.Name] = b
		}
		return true
	})
	for _, n := range ast.Body {
		if _, ok := n.(*nodes.Extends); ok {
			t.hasTopLevelExtends = true
		}
	}
	return t
}

// Render 渲染模板. vars 可省略.
func (t *Template) Render(vars ...map[string]any) (out string, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch v := r.(type) {
			case error:
				err = v
			case loopBreak, loopContinue:
				// break/continue 出现在循环外 (Python 中是生成代码的
				// SyntaxError, 这里转为运行时错误)
				err = &exceptions.TemplateRuntimeError{
					Message: "'break' or 'continue' outside loop"}
			default:
				panic(r)
			}
		}
	}()
	merged := map[string]any{}
	for k, v := range t.Globals {
		merged[k] = v
	}
	for _, m := range vars {
		for k, v := range m {
			merged[k] = normalizeIn(v)
		}
	}
	return t.render(merged), nil
}

// normalizeIn 把用户传入值规约为规范类型 (数值统一 int64/float64).
func normalizeIn(v any) any {
	switch tv := v.(type) {
	case int:
		return int64(tv)
	case int8:
		return int64(tv)
	case int16:
		return int64(tv)
	case int32:
		return int64(tv)
	case uint:
		return int64(tv)
	case uint8:
		return int64(tv)
	case uint16:
		return int64(tv)
	case uint32:
		return int64(tv)
	case uint64:
		return int64(tv)
	case float32:
		return float64(tv)
	case []string:
		out := make([]any, len(tv))
		for i, s := range tv {
			out[i] = s
		}
		return out
	case []int:
		out := make([]any, len(tv))
		for i, n := range tv {
			out[i] = int64(n)
		}
		return out
	case map[string]any:
		d := runtime.NewDict()
		for _, k := range sortedKeys(tv) {
			d.Set(k, normalizeIn(tv[k]))
		}
		return d
	case []any:
		out := make([]any, len(tv))
		for i, item := range tv {
			out[i] = normalizeIn(item)
		}
		return out
	}
	return v
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[j] < keys[i] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}

// blockEntry 是块继承栈中的一项.
type blockEntry struct {
	block *nodes.Block
	tmpl  *Template
}

func (t *Template) newContext(parent map[string]any) *context {
	ctx := &context{
		env:        t.env,
		name:       t.Name,
		vars:       map[string]any{},
		parent:     parent,
		exported:   map[string]bool{},
		autoescape: t.env.autoescapeFor(t.Name),
		blocks:     map[string][]*nodes.Block{},
		undef:      t.env.undefinedFactory(),
	}
	return ctx
}

func (t *Template) render(vars map[string]any) string {
	ctx := t.newContext(vars)
	s := &evalState{ctx: ctx, out: &strings.Builder{}, tmpl: t}
	if _, ok := vars["self"]; !ok {
		vars["self"] = &selfRefValue{state: s}
	}
	s.blockStack = map[string][]blockEntry{}
	for name, b := range t.blocks {
		s.blockStack[name] = []blockEntry{{block: b, tmpl: t}}
	}
	s.execRoot(t)
	return s.out.String()
}

// execRoot 执行模板主体并处理 extends 链.
func (s *evalState) execRoot(t *Template) {
	root := &frame{parent: nil, ctx: s.ctx, vars: map[string]any{}}
	s.currentTemplate = t
	s.suppressOutput = t.hasTopLevelExtends
	s.execBlock(t.ast.Body, root)

	// 沿 extends 链向上渲染父模板; 记录已访问模板以检测循环继承
	seen := map[*Template]bool{t: true}
	for s.parentTemplate != nil {
		p := s.parentTemplate
		if seen[p] {
			s.fail("检测到循环模板继承 (extends): "+runtime.PyStrRepr(p.Name), 0)
		}
		seen[p] = true
		s.parentTemplate = nil
		s.currentTemplate = p
		s.suppressOutput = p.hasTopLevelExtends
		s.extendsSeen = false
		proot := &frame{parent: nil, ctx: s.ctx, vars: map[string]any{}}
		s.execBlock(p.ast.Body, proot)
	}
}

// module 渲染模板并返回导出模块 (import 使用).
func (t *Template) module(parent map[string]any) *templateModule {
	ctx := t.newContext(parent)
	s := &evalState{ctx: ctx, out: &strings.Builder{}, tmpl: t}
	// self 须指向被导入模板自身 (with context 导入时覆盖外层的 self)
	if ctx.parent == nil {
		ctx.parent = map[string]any{}
	}
	ctx.parent["self"] = &selfRefValue{state: s}
	s.blockStack = map[string][]blockEntry{}
	for name, b := range t.blocks {
		s.blockStack[name] = []blockEntry{{block: b, tmpl: t}}
	}
	s.execRoot(t)

	mod := &templateModule{name: t.Name, vars: runtime.NewDict(),
		body: s.out.String()}
	for name := range ctx.exported {
		mod.vars.Set(name, ctx.vars[name])
	}
	return mod
}

// templateModule 对应 jinja2 的 TemplateModule ({% import %} 的结果).
type templateModule struct {
	name string
	vars *runtime.Dict
	body string
}

func (m *templateModule) JinjaGetAttr(name string) (any, bool) {
	return m.vars.Get(name)
}

func (m *templateModule) PyStr() string { return m.body }

func (m *templateModule) PyRepr() string {
	return fmt.Sprintf("<TemplateModule %s>", runtime.PyStrRepr(m.name))
}

package gojinja2

import (
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
	"github.com/yzfly/gojinja2/runtime"
)

// argOr 取位置参数或 kwarg 或默认值.
// 注意位置 pos 以"已含注入参数的 args 切片"为基准.
func argOr(args []any, kwargs *runtime.Dict, pos int, name string, def any) any {
	if pos < len(args) {
		return args[pos]
	}
	if kwargs != nil {
		if v, ok := kwargs.Get(name); ok {
			return v
		}
	}
	return def
}

// softStr 对应 markupsafe.soft_str: Markup 保持 Markup, 其余转 str.
func softStr(v any) any {
	switch v.(type) {
	case string, runtime.Markup:
		return v
	}
	return runtime.Str(v)
}

func joinMarkup(items []any, sep string) any {
	hasMarkup := false
	for _, it := range items {
		if _, ok := it.(runtime.HTMLer); ok {
			hasMarkup = true
			break
		}
	}
	if !hasMarkup {
		strs := make([]string, len(items))
		for i, it := range items {
			strs[i] = runtime.Str(it)
		}
		return strings.Join(strs, sep)
	}
	var b strings.Builder
	sepEsc := string(runtime.Escape(sep))
	for i, it := range items {
		if i > 0 {
			b.WriteString(sepEsc)
		}
		b.WriteString(string(runtime.Escape(it)))
	}
	return runtime.Markup(b.String())
}

func intFromStr(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	var n int64
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + int64(c-'0')
	}
	return n, true
}

func filterInt(a []any, k *runtime.Dict) any {
	def := argOr(a, k, 1, "default", int64(0))
	base := argOr(a, k, 2, "base", int64(10))
	switch v := a[0].(type) {
	case int64:
		return v
	case bool:
		if v {
			return int64(1)
		}
		return int64(0)
	case float64:
		return int64(v)
	case string, runtime.Markup:
		s := strings.TrimSpace(runtime.Str(v))
		b, _ := base.(int64)
		if n, ok := parsePyInt(s, int(b)); ok {
			return n
		}
		// Python 行为: int() 失败后尝试 float 再取整
		if f, ok := parsePyFloat(s); ok && b == 10 {
			return int64(f)
		}
		return def
	}
	return def
}

func filterFloat(a []any, k *runtime.Dict) any {
	def := argOr(a, k, 1, "default", 0.0)
	switch v := a[0].(type) {
	case float64:
		return v
	case int64:
		return float64(v)
	case bool:
		if v {
			return 1.0
		}
		return 0.0
	case string, runtime.Markup:
		if f, ok := parsePyFloat(strings.TrimSpace(runtime.Str(v))); ok {
			return f
		}
		return def
	}
	return def
}

func registerDefaultFilters(m map[string]*runtime.Func) {
	registerAllFilters(m)
}

func registerDefaultTests(m map[string]*runtime.Func) {
	reg := func(name string, fn func(args []any, kwargs *runtime.Dict) any) {
		m[name] = &runtime.Func{Name: name, Fn: fn}
	}
	reg("defined", func(a []any, k *runtime.Dict) any {
		_, isU := a[0].(*runtime.Undefined)
		return !isU
	})
	reg("undefined", func(a []any, k *runtime.Dict) any {
		_, isU := a[0].(*runtime.Undefined)
		return isU
	})
	reg("filter", nil)
	reg("test", nil)
	m["filter"] = &runtime.Func{Name: "filter", Pass: runtime.PassEnvironment,
		Fn: func(a []any, k *runtime.Dict) any {
			env := a[0].(*Environment)
			_, ok := env.Filters[runtime.Str(a[1])]
			return ok
		}}
	m["test"] = &runtime.Func{Name: "test", Pass: runtime.PassEnvironment,
		Fn: func(a []any, k *runtime.Dict) any {
			env := a[0].(*Environment)
			_, ok := env.Tests[runtime.Str(a[1])]
			return ok
		}}
	reg("none", func(a []any, k *runtime.Dict) any { return a[0] == nil })
	reg("boolean", func(a []any, k *runtime.Dict) any {
		_, ok := a[0].(bool)
		return ok
	})
	reg("false", func(a []any, k *runtime.Dict) any { return a[0] == any(false) })
	reg("true", func(a []any, k *runtime.Dict) any { return a[0] == any(true) })
	reg("integer", func(a []any, k *runtime.Dict) any {
		_, ok := a[0].(int64)
		return ok
	})
	reg("float", func(a []any, k *runtime.Dict) any {
		_, ok := a[0].(float64)
		return ok
	})
	reg("number", func(a []any, k *runtime.Dict) any {
		switch a[0].(type) {
		case int64, float64, bool:
			return true
		}
		return false
	})
	reg("string", func(a []any, k *runtime.Dict) any {
		switch a[0].(type) {
		case string, runtime.Markup:
			return true
		}
		return false
	})
	reg("sequence", func(a []any, k *runtime.Dict) any {
		_, lenOK := tryLen(a[0])
		_, iterOK := tryIter(a[0])
		return lenOK && iterOK
	})
	reg("iterable", func(a []any, k *runtime.Dict) any {
		_, ok := tryIter(a[0])
		return ok
	})
	reg("mapping", func(a []any, k *runtime.Dict) any {
		_, ok := a[0].(*runtime.Dict)
		return ok
	})
	reg("callable", func(a []any, k *runtime.Dict) any {
		return runtime.IsCallable(a[0])
	})
	reg("sameas", func(a []any, k *runtime.Dict) any {
		return sameAs(a[0], argOr(a, k, 1, "other", nil))
	})
	reg("divisibleby", func(a []any, k *runtime.Dict) any {
		num := argOr(a, k, 1, "num", nil)
		return runtime.Truth(runtime.Equal(runtime.Mod(a[0], num), int64(0)))
	})
	reg("even", func(a []any, k *runtime.Dict) any {
		return runtime.Truth(runtime.Equal(runtime.Mod(a[0], int64(2)), int64(0)))
	})
	reg("odd", func(a []any, k *runtime.Dict) any {
		return runtime.Truth(runtime.Equal(runtime.Mod(a[0], int64(2)), int64(1)))
	})
	reg("lower", func(a []any, k *runtime.Dict) any {
		s := runtime.Str(a[0])
		return s == strings.ToLower(s)
	})
	reg("upper", func(a []any, k *runtime.Dict) any {
		s := runtime.Str(a[0])
		return s == strings.ToUpper(s)
	})
	cmp := func(name, op string) {
		reg(name, func(a []any, k *runtime.Dict) any {
			return runtime.CompareOp(op, a[0], argOr(a, k, 1, "other", nil))
		})
	}
	cmp("eq", "eq")
	cmp("==", "eq")
	cmp("equalto", "eq")
	cmp("ne", "ne")
	cmp("!=", "ne")
	cmp("lt", "lt")
	cmp("<", "lt")
	cmp("lessthan", "lt")
	cmp("le", "lteq")
	cmp("<=", "lteq")
	cmp("gt", "gt")
	cmp(">", "gt")
	cmp("greaterthan", "gt")
	cmp("ge", "gteq")
	cmp(">=", "gteq")
	reg("in", func(a []any, k *runtime.Dict) any {
		return runtime.Contains(argOr(a, k, 1, "seq", nil), a[0])
	})
	reg("escaped", func(a []any, k *runtime.Dict) any {
		_, ok := a[0].(runtime.HTMLer)
		return ok
	})
}

func sameAs(a, b any) bool {
	switch av := a.(type) {
	case nil:
		return b == nil
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	case int64:
		bv, ok := b.(int64)
		return ok && av == bv
	}
	return a == b
}

func tryIter(v any) (items []any, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	switch v.(type) {
	case int64, float64, bool, nil:
		return nil, false
	}
	return runtime.Iterate(v), true
}

func tryLen(v any) (n int, ok bool) {
	defer func() {
		if recover() != nil {
			ok = false
		}
	}()
	return runtime.Length(v), true
}

// cyclerValue 对应 jinja2.utils.Cycler.
type cyclerValue struct {
	items []any
	pos   int
}

func (c *cyclerValue) Call(args []any, kwargs *runtime.Dict) any {
	return c.Next()
}

func (c *cyclerValue) Next() any {
	rv := c.items[c.pos]
	c.pos = (c.pos + 1) % len(c.items)
	return rv
}

func (c *cyclerValue) JinjaGetAttr(name string) (any, bool) {
	switch name {
	case "current":
		return c.items[c.pos], true
	case "next":
		return &runtime.BoundMethod{Name: "next", Fn: func(a []any, k *runtime.Dict) any {
			return c.Next()
		}}, true
	case "reset":
		return &runtime.BoundMethod{Name: "reset", Fn: func(a []any, k *runtime.Dict) any {
			c.pos = 0
			return nil
		}}, true
	}
	return nil, false
}

// joinerValue 对应 jinja2.utils.Joiner.
type joinerValue struct {
	sep  string
	used bool
}

func (j *joinerValue) Call(args []any, kwargs *runtime.Dict) any {
	if !j.used {
		j.used = true
		return ""
	}
	return j.sep
}

const lipsumText = "Lorem ipsum dolor sit amet, consectetur adipiscing elit, sed do eiusmod tempor incididunt ut labore et dolore magna aliqua. Ut enim ad minim veniam, quis nostrud exercitation ullamco laboris nisi ut aliquip ex ea commodo consequat."

func registerDefaultGlobals(m map[string]any) {
	m["range"] = &runtime.Func{Name: "range", Fn: func(a []any, k *runtime.Dict) any {
		var start, stop, step int64 = 0, 0, 1
		switch len(a) {
		case 1:
			stop = mustInt(a[0])
		case 2:
			start, stop = mustInt(a[0]), mustInt(a[1])
		case 3:
			start, stop, step = mustInt(a[0]), mustInt(a[1]), mustInt(a[2])
		default:
			runtime.RaiseType("range expected at most 3 arguments, got " + itoa(len(a)))
		}
		if step == 0 {
			runtime.RaiseType("range() arg 3 must not be zero")
		}
		var out []any
		for i := start; (step > 0 && i < stop) || (step < 0 && i > stop); i += step {
			out = append(out, i)
		}
		if out == nil {
			out = []any{}
		}
		return out
	}}
	m["dict"] = &runtime.Func{Name: "dict", Fn: func(a []any, k *runtime.Dict) any {
		d := runtime.NewDict()
		if len(a) > 0 {
			if src, ok := a[0].(*runtime.Dict); ok {
				for _, it := range src.Items() {
					d.Set(it.Key, it.Value)
				}
			}
		}
		if k != nil {
			for _, it := range k.Items() {
				d.Set(it.Key, it.Value)
			}
		}
		return d
	}}
	m["namespace"] = &runtime.Func{Name: "namespace", Fn: func(a []any, k *runtime.Dict) any {
		return runtime.NewNamespace(a, k)
	}}
	m["cycler"] = &runtime.Func{Name: "cycler", Fn: func(a []any, k *runtime.Dict) any {
		if len(a) == 0 {
			runtime.RaiseType("at least one item has to be passed to cycler.")
		}
		return &cyclerValue{items: a}
	}}
	m["joiner"] = &runtime.Func{Name: "joiner", Fn: func(a []any, k *runtime.Dict) any {
		sep := ", "
		if len(a) > 0 {
			sep = runtime.Str(a[0])
		}
		return &joinerValue{sep: sep}
	}}
	m["lipsum"] = &runtime.Func{Name: "lipsum", Fn: func(a []any, k *runtime.Dict) any {
		n := int(toInt(argOr(a, k, 0, "n", int64(5))))
		htmlOut := runtime.Truth(argOr(a, k, 1, "html", true))
		var paras []string
		for i := 0; i < n; i++ {
			paras = append(paras, lipsumText)
		}
		if htmlOut {
			return runtime.Markup("<p>" + strings.Join(paras, "</p>\n\n<p>") + "</p>")
		}
		return strings.Join(paras, "\n\n")
	}}
}

func mustInt(v any) int64 {
	switch tv := v.(type) {
	case int64:
		return tv
	case bool:
		if tv {
			return 1
		}
		return 0
	case float64:
		runtime.RaiseType("'float' object cannot be interpreted as an integer")
	case *runtime.Undefined:
		tv.Fail()
	}
	runtime.RaiseType(runtime.PyStrRepr(runtime.PyTypeName(v)) +
		" object cannot be interpreted as an integer")
	return 0
}

// DictLoader 对应 jinja2.DictLoader.
type DictLoader struct {
	Templates map[string]string
}

func NewDictLoader(templates map[string]string) *DictLoader {
	return &DictLoader{Templates: templates}
}

func (l *DictLoader) GetSource(env *Environment, name string) (string, string, error) {
	if src, ok := l.Templates[name]; ok {
		return src, "", nil
	}
	return "", "", &exceptions.TemplateNotFound{TemplateName: name}
}

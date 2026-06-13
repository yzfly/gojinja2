package gojinja2

import (
	"regexp"
	"strings"

	"github.com/yzfly/gojinja2/lexer"
	"github.com/yzfly/gojinja2/nodes"
	"github.com/yzfly/gojinja2/parser"
	"github.com/yzfly/gojinja2/runtime"
)

// 本文件对应 jinja2/ext.py 的内置扩展:
// do, loopcontrols (break/continue), i18n (trans).

// AddExtension 启用扩展. 接受 "do" / "loopcontrols" / "i18n"
// 或 Python 风格全名 "jinja2.ext.do" 等.
func (e *Environment) AddExtension(name string) {
	short := name
	if idx := strings.LastIndex(name, "."); idx >= 0 {
		short = name[idx+1:]
	}
	if e.extensions == nil {
		e.extensions = map[string]bool{}
	}
	e.extensions[short] = true
}

// parserExtensions 构建标签解析表.
func (e *Environment) parserExtensions() map[string]parser.ExtensionParser {
	if len(e.extensions) == 0 {
		return nil
	}
	out := map[string]parser.ExtensionParser{}
	if e.extensions["do"] {
		out["do"] = parseDo
	}
	if e.extensions["loopcontrols"] {
		out["break"] = parseBreak
		out["continue"] = parseContinue
	}
	if e.extensions["i18n"] {
		out["trans"] = e.parseTrans
		out["pluralize"] = nil // 由 trans 内部消费; 单独出现时报未知 tag
		delete(out, "pluralize")
	}
	return out
}

// InstallNullTranslations 对应 install_null_translations (newstyle=False).
func (e *Environment) InstallNullTranslations() {
	e.AddExtension("i18n")
	e.Globals["gettext"] = &runtime.Func{Name: "gettext",
		Fn: func(a []any, k *runtime.Dict) any { return runtime.Str(a[0]) }}
	e.Globals["ngettext"] = &runtime.Func{Name: "ngettext",
		Fn: func(a []any, k *runtime.Dict) any {
			if runtime.Truth(runtime.Equal(a[2], int64(1))) {
				return runtime.Str(a[0])
			}
			return runtime.Str(a[1])
		}}
	e.Globals["pgettext"] = &runtime.Func{Name: "pgettext",
		Fn: func(a []any, k *runtime.Dict) any { return runtime.Str(a[1]) }}
	e.Globals["npgettext"] = &runtime.Func{Name: "npgettext",
		Fn: func(a []any, k *runtime.Dict) any {
			if runtime.Truth(runtime.Equal(a[3], int64(1))) {
				return runtime.Str(a[1])
			}
			return runtime.Str(a[2])
		}}
}

// InstallGettext 安装自定义翻译函数.
func (e *Environment) InstallGettext(
	gettext func(string) string,
	ngettext func(singular, plural string, n int64) string) {
	e.AddExtension("i18n")
	e.Globals["gettext"] = &runtime.Func{Name: "gettext",
		Fn: func(a []any, k *runtime.Dict) any { return gettext(runtime.Str(a[0])) }}
	e.Globals["ngettext"] = &runtime.Func{Name: "ngettext",
		Fn: func(a []any, k *runtime.Dict) any {
			n := toInt(a[2])
			return ngettext(runtime.Str(a[0]), runtime.Str(a[1]), n)
		}}
}

// ---- do ----

func parseDo(p *parser.Parser) []nodes.Node {
	lineno := p.Stream.Next().Lineno
	node := &nodes.ExprStmt{Node: p.ParseTuple(false, true, nil, false)}
	node.Line = lineno
	return []nodes.Node{node}
}

// ---- loopcontrols ----

func parseBreak(p *parser.Parser) []nodes.Node {
	n := &nodes.Break{}
	n.Line = p.Stream.Next().Lineno
	return []nodes.Node{n}
}

func parseContinue(p *parser.Parser) []nodes.Node {
	n := &nodes.Continue{}
	n.Line = p.Stream.Next().Lineno
	return []nodes.Node{n}
}

// ---- i18n: {% trans %} ----

var transTrimRe = regexp.MustCompile(`\s*\n\s*`)

func (e *Environment) parseTrans(p *parser.Parser) []nodes.Node {
	lineno := p.Stream.Next().Lineno

	// 可选的 pgettext 上下文字符串
	var hasContext bool
	var contextStr string
	if tok := p.Stream.NextIf("string"); tok != nil {
		hasContext = true
		contextStr = tok.Value
	}

	var pluralExpr nodes.Expr
	var pluralExprAssignment *nodes.Assign
	numCalledNum := false
	varNames := []string{}
	varMap := map[string]nodes.Expr{}
	var trimmed *bool

	setVar := func(name string, expr nodes.Expr) {
		if _, dup := varMap[name]; !dup {
			varNames = append(varNames, name)
		}
		varMap[name] = expr
	}

	for p.Stream.Current.Type != lexer.TokenBlockEnd {
		if len(varMap) > 0 {
			if _, err := p.Stream.Expect("comma"); err != nil {
				panic(err)
			}
		}
		if p.Stream.SkipIf("colon") {
			break
		}
		tok, err := p.Stream.Expect("name")
		if err != nil {
			panic(err)
		}
		if _, dup := varMap[tok.Value]; dup {
			p.Fail("translatable variable "+nodes.PyStrRepr(tok.Value)+
				" defined twice.", tok.Lineno)
		}
		var v nodes.Expr
		if p.Stream.Current.Type == lexer.TokenAssign {
			p.Stream.Next()
			v = p.ParseExpression(true)
			setVar(tok.Value, v)
		} else if trimmed == nil && (tok.Value == "trimmed" || tok.Value == "notrimmed") {
			t := tok.Value == "trimmed"
			trimmed = &t
			continue
		} else {
			name := &nodes.Name{Name: tok.Value, Ctx: "load"}
			name.Line = tok.Lineno
			v = name
			setVar(tok.Value, v)
		}
		if pluralExpr == nil {
			if call, isCall := v.(*nodes.Call); isCall {
				trans := &nodes.Name{Name: "_trans", Ctx: "load"}
				pluralExpr = trans
				varMap[tok.Value] = trans
				store := &nodes.Name{Name: "_trans", Ctx: "store"}
				pluralExprAssignment = &nodes.Assign{Target: store, Node: call}
			} else {
				pluralExpr = v
			}
			numCalledNum = tok.Value == "num"
		}
	}
	if _, err := p.Stream.Expect("block_end"); err != nil {
		panic(err)
	}

	havePlural := false
	var referenced []string

	singularNames, singular := transParseBlock(p, true)
	referenced = append(referenced, singularNames...)
	if len(singularNames) > 0 && pluralExpr == nil {
		n := &nodes.Name{Name: singularNames[0], Ctx: "load"}
		pluralExpr = n
		numCalledNum = singularNames[0] == "num"
	}

	var plural string
	if p.Stream.Current.Test("name:pluralize") {
		havePlural = true
		p.Stream.Next()
		if p.Stream.Current.Type != lexer.TokenBlockEnd {
			tok, err := p.Stream.Expect("name")
			if err != nil {
				panic(err)
			}
			v, ok := varMap[tok.Value]
			if !ok {
				p.Fail("unknown variable "+nodes.PyStrRepr(tok.Value)+
					" for pluralization", tok.Lineno)
			}
			pluralExpr = v
			numCalledNum = tok.Value == "num"
		}
		if _, err := p.Stream.Expect("block_end"); err != nil {
			panic(err)
		}
		var pluralNames []string
		pluralNames, plural = transParseBlock(p, false)
		p.Stream.Next()
		referenced = append(referenced, pluralNames...)
	} else {
		p.Stream.Next()
	}

	for _, name := range referenced {
		if _, ok := varMap[name]; !ok {
			n := &nodes.Name{Name: name, Ctx: "load"}
			setVar(name, n)
		}
	}

	if !havePlural {
		pluralExpr = nil
	} else if pluralExpr == nil {
		p.Fail("pluralize without variables", lineno)
	}

	useTrimmed := false
	if trimmed != nil {
		useTrimmed = *trimmed
	} else if v, ok := e.Policies["ext.i18n.trimmed"].(bool); ok {
		useTrimmed = v
	}
	if useTrimmed {
		singular = transTrimRe.ReplaceAllString(strings.TrimSpace(singular), " ")
		if plural != "" {
			plural = transTrimRe.ReplaceAllString(strings.TrimSpace(plural), " ")
		}
	}

	node := e.makeTransNode(singular, plural, hasContext, contextStr,
		varNames, varMap, pluralExpr, len(referenced) > 0, numCalledNum && havePlural)
	node.Line = lineno
	if pluralExprAssignment != nil {
		return []nodes.Node{pluralExprAssignment, node}
	}
	return []nodes.Node{node}
}

// transParseBlock 对应 _parse_block: 收集 data 与 {{ name }} 变量.
func transParseBlock(p *parser.Parser, allowPluralize bool) ([]string, string) {
	var referenced []string
	var buf strings.Builder
	for {
		switch p.Stream.Current.Type {
		case lexer.TokenData:
			buf.WriteString(strings.ReplaceAll(p.Stream.Current.Value, "%", "%%"))
			p.Stream.Next()
		case lexer.TokenVariableBegin:
			p.Stream.Next()
			tok, err := p.Stream.Expect("name")
			if err != nil {
				panic(err)
			}
			referenced = append(referenced, tok.Value)
			buf.WriteString("%(" + tok.Value + ")s")
			if _, err := p.Stream.Expect("variable_end"); err != nil {
				panic(err)
			}
		case lexer.TokenBlockBegin:
			p.Stream.Next()
			blockName := ""
			if p.Stream.Current.Type == lexer.TokenName {
				blockName = p.Stream.Current.Value
			}
			switch blockName {
			case "endtrans":
				return referenced, buf.String()
			case "pluralize":
				if allowPluralize {
					return referenced, buf.String()
				}
				p.Fail("a translatable section can have only one pluralize section", 0)
			case "trans":
				p.Fail("trans blocks can't be nested; did you mean `endtrans`?", 0)
			}
			p.Fail("control structures in translatable sections are not allowed; "+
				"saw `"+blockName+"`", 0)
		case lexer.TokenEOF:
			p.Fail("unclosed translation block", 0)
		default:
			panic("gojinja2: internal parser error in trans block")
		}
	}
}

// makeTransNode 对应 _make_node (old-style gettext 路径).
func (e *Environment) makeTransNode(singular, plural string, hasContext bool,
	contextStr string, varNames []string, varMap map[string]nodes.Expr,
	pluralExpr nodes.Expr, varsReferenced, numCalledNum bool) *nodes.Output {

	// 无变量引用时, old-style 不做 % 格式化, %% 还原为 %
	if !varsReferenced {
		singular = strings.ReplaceAll(singular, "%%", "%")
		if plural != "" {
			plural = strings.ReplaceAll(plural, "%%", "%")
		}
	}

	funcName := "gettext"
	var funcArgs []nodes.Expr
	sgConst := &nodes.Const{Value: singular}
	funcArgs = append(funcArgs, sgConst)

	if hasContext {
		ctxConst := &nodes.Const{Value: contextStr}
		funcArgs = append([]nodes.Expr{ctxConst}, funcArgs...)
		funcName = "p" + funcName
	}
	if pluralExpr != nil {
		funcName = "n" + funcName
		plConst := &nodes.Const{Value: plural}
		funcArgs = append(funcArgs, plConst, pluralExpr)
	}

	fnName := &nodes.Name{Name: funcName, Ctx: "load"}
	call := &nodes.Call{Node: fnName, Args: funcArgs}

	var node nodes.Expr = &nodes.MarkSafeIfAutoescape{Expr: call}
	if len(varMap) > 0 {
		var pairs []*nodes.Pair
		for _, key := range varNames {
			pairs = append(pairs, &nodes.Pair{
				Key: &nodes.Const{Value: key}, Value: varMap[key]})
		}
		node = &nodes.BinExpr{Op: "%", Left: node,
			Right: &nodes.Dict{Items: pairs}}
	}
	out := &nodes.Output{Nodes: []nodes.Expr{node}}
	return out
}

// Package parser 把 token 流解析为 AST, 移植自 jinja2/parser.py (3.1.6).
//
// 错误处理: 包内部用 panic(*TemplateSyntaxError) 模拟 Python 异常,
// 在公共入口 Parse 处 recover 并转为 error 返回.
package parser

import (
	"strconv"
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
	"github.com/yzfly/gojinja2/lexer"
	"github.com/yzfly/gojinja2/nodes"
)

var statementKeywords = map[string]bool{
	"for": true, "if": true, "block": true, "extends": true, "print": true,
	"macro": true, "include": true, "from": true, "import": true,
	"set": true, "with": true, "autoescape": true,
}

var compareOperators = map[lexer.Type]bool{
	"eq": true, "ne": true, "lt": true, "lteq": true, "gt": true, "gteq": true,
}

// ExtensionParser 是扩展标签的解析函数 (M6 使用).
type ExtensionParser func(*Parser) []nodes.Node

// Parser 是核心解析器.
type Parser struct {
	Stream     *lexer.TokenStream
	Name       string
	Filename   string
	Extensions map[string]ExtensionParser

	lastIdentifier int
	tagStack       []string
	endTokenStack  [][]string
}

// New 构造 Parser.
func New(stream *lexer.TokenStream, name, filename string, extensions map[string]ExtensionParser) *Parser {
	return &Parser{Stream: stream, Name: name, Filename: filename, Extensions: extensions}
}

// Parse 解析整个模板. 对应 Parser.parse.
func (p *Parser) Parse() (tpl *nodes.Template, err error) {
	defer func() {
		if r := recover(); r != nil {
			if se, ok := r.(*exceptions.TemplateSyntaxError); ok {
				err = se
				return
			}
			panic(r)
		}
	}()
	body := p.subparse(nil)
	tpl = &nodes.Template{Body: body}
	tpl.Line = 1
	return tpl, nil
}

// ParseExpressionOnly 供 API (compile_expression) 使用.
func (p *Parser) ParseExpressionOnly() (expr nodes.Expr, err error) {
	defer func() {
		if r := recover(); r != nil {
			if se, ok := r.(*exceptions.TemplateSyntaxError); ok {
				err = se
				return
			}
			panic(r)
		}
	}()
	expr = p.ParseExpression(true)
	return expr, nil
}

// Fail 抛出语法错误, 对应 Parser.fail.
func (p *Parser) Fail(msg string, lineno int) {
	if lineno == 0 {
		lineno = p.Stream.Current.Lineno
	}
	panic(exceptions.NewSyntaxError(msg, lineno, p.Name, p.Filename))
}

func (p *Parser) failAssertion(msg string, lineno int) {
	e := exceptions.NewSyntaxError(msg, lineno, p.Name, p.Filename)
	e.IsAssertion = true
	panic(e)
}

func (p *Parser) failUtEOF(name string, endTokenStack [][]string, lineno int) {
	expected := map[string]bool{}
	for _, exprs := range endTokenStack {
		for _, e := range exprs {
			expected[lexer.DescribeTokenExpr(e)] = true
		}
	}
	currentlyLooking := ""
	if len(endTokenStack) > 0 {
		last := endTokenStack[len(endTokenStack)-1]
		descs := make([]string, len(last))
		for i, e := range last {
			descs[i] = nodes.PyStrRepr(lexer.DescribeTokenExpr(e))
		}
		currentlyLooking = strings.Join(descs, " or ")
	}

	var message []string
	if name == "" {
		message = append(message, "Unexpected end of template.")
	} else {
		message = append(message, "Encountered unknown tag "+nodes.PyStrRepr(name)+".")
	}
	if currentlyLooking != "" {
		if name != "" && expected[name] {
			message = append(message,
				"You probably made a nesting mistake. Jinja is expecting this tag,"+
					" but currently looking for "+currentlyLooking+".")
		} else {
			message = append(message,
				"Jinja was looking for the following tags: "+currentlyLooking+".")
		}
	}
	if len(p.tagStack) > 0 {
		message = append(message,
			"The innermost block that needs to be closed is "+
				nodes.PyStrRepr(p.tagStack[len(p.tagStack)-1])+".")
	}
	p.Fail(strings.Join(message, " "), lineno)
}

// FailUnknownTag 对应 fail_unknown_tag.
func (p *Parser) FailUnknownTag(name string, lineno int) {
	p.failUtEOF(name, p.endTokenStack, lineno)
}

// failEOF 对应 fail_eof.
func (p *Parser) failEOF(endTokens []string, lineno int) {
	stack := make([][]string, len(p.endTokenStack))
	copy(stack, p.endTokenStack)
	if endTokens != nil {
		stack = append(stack, endTokens)
	}
	p.failUtEOF("", stack, lineno)
}

func (p *Parser) isTupleEnd(extraEndRules []string) bool {
	switch p.Stream.Current.Type {
	case lexer.TokenVariableEnd, lexer.TokenBlockEnd, lexer.TokenRparen:
		return true
	}
	if extraEndRules != nil {
		return p.Stream.Current.TestAny(extraEndRules...)
	}
	return false
}

// FreeIdentifier 返回一个新的内部标识符, 对应 free_identifier.
func (p *Parser) FreeIdentifier(lineno int) *nodes.InternalName {
	p.lastIdentifier++
	n := &nodes.InternalName{Name: "fi" + itoa(p.lastIdentifier)}
	n.Line = lineno
	return n
}

func itoa(i int) string { return strconv.Itoa(i) }

// expect 包装 TokenStream.Expect, 失败时 panic.
func (p *Parser) expect(expr string) lexer.Token {
	t, err := p.Stream.Expect(expr)
	if err != nil {
		panic(err)
	}
	return t
}

func (p *Parser) next() lexer.Token { return p.Stream.Next() }

// parseStatement 对应 parse_statement. 统一返回节点列表.
func (p *Parser) parseStatement() []nodes.Node {
	token := p.Stream.Current
	if token.Type != lexer.TokenName {
		p.Fail("tag name expected", token.Lineno)
	}
	p.tagStack = append(p.tagStack, token.Value)
	popTag := true
	defer func() {
		if popTag {
			p.tagStack = p.tagStack[:len(p.tagStack)-1]
		}
	}()

	if statementKeywords[token.Value] {
		var node nodes.Node
		switch token.Value {
		case "for":
			node = p.parseFor()
		case "if":
			node = p.parseIf()
		case "block":
			node = p.parseBlock()
		case "extends":
			node = p.parseExtends()
		case "print":
			node = p.parsePrint()
		case "macro":
			node = p.parseMacro()
		case "include":
			node = p.parseInclude()
		case "from":
			node = p.parseFrom()
		case "import":
			node = p.parseImport()
		case "set":
			node = p.parseSet()
		case "with":
			node = p.parseWith()
		case "autoescape":
			node = p.parseAutoescape()
		}
		return []nodes.Node{node}
	}
	if token.Value == "call" {
		return []nodes.Node{p.parseCallBlock()}
	}
	if token.Value == "filter" {
		return []nodes.Node{p.parseFilterBlock()}
	}
	if ext, ok := p.Extensions[token.Value]; ok {
		return ext(p)
	}

	// 未知 tag: 先弹出误入栈的 tag 名, 以便错误信息正确
	p.tagStack = p.tagStack[:len(p.tagStack)-1]
	popTag = false
	p.FailUnknownTag(token.Value, token.Lineno)
	return nil
}

// ParseStatements 对应 parse_statements.
func (p *Parser) ParseStatements(endTokens []string, dropNeedle bool) []nodes.Node {
	// 第一个 token 可以是冒号 (python 兼容)
	p.Stream.SkipIf("colon")
	p.expect("block_end")
	result := p.subparse(endTokens)

	// 模板提前结束: subparse 不检查, 这里补上
	if p.Stream.Current.Type == lexer.TokenEOF {
		p.failEOF(endTokens, 0)
	}

	if dropNeedle {
		p.next()
	}
	return result
}

func (p *Parser) parseSet() nodes.Node {
	lineno := p.next().Lineno
	target := p.parseAssignTarget(true, false, nil, true)
	if p.Stream.SkipIf("assign") {
		expr := p.ParseTuple(false, true, nil, false)
		n := &nodes.Assign{Target: target, Node: expr}
		n.Line = lineno
		return n
	}
	filterNode := p.parseFilterChain(nil, false)
	body := p.ParseStatements([]string{"name:endset"}, true)
	n := &nodes.AssignBlock{Target: target, Filter: filterNode, Body: body}
	n.Line = lineno
	return n
}

func (p *Parser) parseFor() nodes.Node {
	lineno := p.expect("name:for").Lineno
	target := p.parseAssignTarget(true, false, []string{"name:in"}, false)
	p.expect("name:in")
	iter := p.ParseTuple(false, false, []string{"name:recursive"}, false)
	var test nodes.Expr
	if p.Stream.SkipIf("name:if") {
		test = p.ParseExpression(true)
	}
	recursive := p.Stream.SkipIf("name:recursive")
	body := p.ParseStatements([]string{"name:endfor", "name:else"}, false)
	var else_ []nodes.Node
	if p.next().Value != "endfor" {
		else_ = p.ParseStatements([]string{"name:endfor"}, true)
	}
	n := &nodes.For{Target: target, Iter: iter, Body: body, Else: else_,
		Test: test, Recursive: recursive}
	n.Line = lineno
	return n
}

func (p *Parser) parseIf() nodes.Node {
	result := &nodes.If{}
	result.Line = p.expect("name:if").Lineno
	node := result
	for {
		node.Test = p.ParseTuple(false, false, nil, false)
		node.Body = p.ParseStatements([]string{"name:elif", "name:else", "name:endif"}, false)
		node.Elif = []*nodes.If{}
		node.Else = nil
		token := p.next()
		if token.Test("name:elif") {
			node = &nodes.If{}
			node.Line = p.Stream.Current.Lineno
			result.Elif = append(result.Elif, node)
			continue
		} else if token.Test("name:else") {
			result.Else = p.ParseStatements([]string{"name:endif"}, true)
		}
		break
	}
	return result
}

func (p *Parser) parseWith() nodes.Node {
	node := &nodes.With{}
	node.Line = p.next().Lineno
	var targets, values []nodes.Expr
	for p.Stream.Current.Type != lexer.TokenBlockEnd {
		if len(targets) > 0 {
			p.expect("comma")
		}
		target := p.parseAssignTarget(true, false, nil, false)
		nodes.SetCtx(target, "param")
		targets = append(targets, target)
		p.expect("assign")
		values = append(values, p.ParseExpression(true))
	}
	node.Targets = targets
	node.Values = values
	node.Body = p.ParseStatements([]string{"name:endwith"}, true)
	return node
}

func (p *Parser) parseAutoescape() nodes.Node {
	mod := &nodes.ScopedEvalContextModifier{}
	mod.Line = p.next().Lineno
	kw := &nodes.Keyword{Key: "autoescape", Value: p.ParseExpression(true)}
	kw.Line = mod.Line
	mod.Options = []*nodes.Keyword{kw}
	mod.Body = p.ParseStatements([]string{"name:endautoescape"}, true)
	scope := &nodes.Scope{Body: []nodes.Node{mod}}
	scope.Line = mod.Line
	return scope
}

func (p *Parser) parseBlock() nodes.Node {
	node := &nodes.Block{}
	node.Line = p.next().Lineno
	node.Name = p.expect("name").Value
	node.Scoped = p.Stream.SkipIf("name:scoped")
	node.Required = p.Stream.SkipIf("name:required")

	// django 迁移者常见问题: block 名不允许连字符
	if p.Stream.Current.Type == lexer.TokenSub {
		p.Fail("Block names in Jinja have to be valid Python identifiers and may not"+
			" contain hyphens, use an underscore instead.", 0)
	}

	node.Body = p.ParseStatements([]string{"name:endblock"}, true)

	// required block 只允许空白和注释
	if node.Required {
		for _, bodyNode := range node.Body {
			output, ok := bodyNode.(*nodes.Output)
			valid := ok
			if ok {
				for _, outputNode := range output.Nodes {
					td, isData := outputNode.(*nodes.TemplateData)
					if !isData || !isAllWhitespace(td.Data) {
						valid = false
						break
					}
				}
			}
			if !valid {
				p.Fail("Required blocks can only contain comments or whitespace", 0)
			}
		}
	}

	p.Stream.SkipIf("name:" + node.Name)
	return node
}

func isAllWhitespace(s string) bool {
	if s == "" {
		return false // Python str.isspace() 对空串为 False
	}
	for _, r := range s {
		if !isPySpace(r) {
			return false
		}
	}
	return true
}

func isPySpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x1f, 0x85:
		return true
	}
	return r == 0xa0 || (r >= 0x2000 && r <= 0x200a) ||
		r == 0x1680 || r == 0x2028 || r == 0x2029 || r == 0x202f ||
		r == 0x205f || r == 0x3000
}

func (p *Parser) parseExtends() nodes.Node {
	node := &nodes.Extends{}
	node.Line = p.next().Lineno
	node.Template = p.ParseExpression(true)
	return node
}

// parseImportContext 对应 parse_import_context.
// setter 回调把解析到的 with_context 写回节点.
func (p *Parser) parseImportContext(defaultValue bool) bool {
	if p.Stream.Current.TestAny("name:with", "name:without") &&
		p.Stream.Look().Test("name:context") {
		withContext := p.next().Value == "with"
		p.Stream.Skip(1)
		return withContext
	}
	return defaultValue
}

func (p *Parser) parseInclude() nodes.Node {
	node := &nodes.Include{}
	node.Line = p.next().Lineno
	node.Template = p.ParseExpression(true)
	if p.Stream.Current.Test("name:ignore") && p.Stream.Look().Test("name:missing") {
		node.IgnoreMissing = true
		p.Stream.Skip(2)
	}
	node.WithContext = p.parseImportContext(true)
	return node
}

func (p *Parser) parseImport() nodes.Node {
	node := &nodes.Import{}
	node.Line = p.next().Lineno
	node.Template = p.ParseExpression(true)
	p.expect("name:as")
	node.Target = p.parseNameOnlyTarget().Name
	node.WithContext = p.parseImportContext(false)
	return node
}

func (p *Parser) parseFrom() nodes.Node {
	node := &nodes.FromImport{}
	node.Line = p.next().Lineno
	node.Template = p.ParseExpression(true)
	p.expect("name:import")

	parseContext := func() bool {
		if (p.Stream.Current.Value == "with" || p.Stream.Current.Value == "without") &&
			p.Stream.Look().Test("name:context") {
			node.WithContext = p.next().Value == "with"
			p.Stream.Skip(1)
			return true
		}
		return false
	}

	for {
		if len(node.Names) > 0 {
			p.expect("comma")
		}
		if p.Stream.Current.Type == lexer.TokenName {
			if parseContext() {
				break
			}
			target := p.parseNameOnlyTarget()
			if strings.HasPrefix(target.Name, "_") {
				p.failAssertion("names starting with an underline can not be imported",
					target.Line)
			}
			if p.Stream.SkipIf("name:as") {
				alias := p.parseNameOnlyTarget()
				node.Names = append(node.Names, nodes.ImportName{Name: target.Name, Alias: alias.Name})
			} else {
				node.Names = append(node.Names, nodes.ImportName{Name: target.Name})
			}
			if parseContext() || p.Stream.Current.Type != lexer.TokenComma {
				break
			}
		} else {
			p.expect("name")
		}
	}
	return node
}

// parseSignature 解析 macro / call block 的参数签名.
func (p *Parser) parseSignature() (args []*nodes.Name, defaults []nodes.Expr) {
	p.expect("lparen")
	for p.Stream.Current.Type != lexer.TokenRparen {
		if len(args) > 0 {
			p.expect("comma")
		}
		arg := p.parseNameOnlyTarget()
		arg.Ctx = "param"
		if p.Stream.SkipIf("assign") {
			defaults = append(defaults, p.ParseExpression(true))
		} else if len(defaults) > 0 {
			p.Fail("non-default argument follows default argument", 0)
		}
		args = append(args, arg)
	}
	p.expect("rparen")
	return args, defaults
}

func (p *Parser) parseCallBlock() nodes.Node {
	node := &nodes.CallBlock{}
	node.Line = p.next().Lineno
	if p.Stream.Current.Type == lexer.TokenLparen {
		node.Args, node.Defaults = p.parseSignature()
	}

	callNode, ok := p.ParseExpression(true).(*nodes.Call)
	if !ok {
		p.Fail("expected call", node.Line)
	}
	node.Call = callNode
	node.Body = p.ParseStatements([]string{"name:endcall"}, true)
	return node
}

func (p *Parser) parseFilterBlock() nodes.Node {
	node := &nodes.FilterBlock{}
	node.Line = p.next().Lineno
	node.Filter = p.parseFilterChain(nil, true)
	node.Body = p.ParseStatements([]string{"name:endfilter"}, true)
	return node
}

func (p *Parser) parseMacro() nodes.Node {
	node := &nodes.Macro{}
	node.Line = p.next().Lineno
	node.Name = p.parseNameOnlyTarget().Name
	node.Args, node.Defaults = p.parseSignature()
	node.Body = p.ParseStatements([]string{"name:endmacro"}, true)
	return node
}

func (p *Parser) parsePrint() nodes.Node {
	node := &nodes.Output{}
	node.Line = p.next().Lineno
	for p.Stream.Current.Type != lexer.TokenBlockEnd {
		if len(node.Nodes) > 0 {
			p.expect("comma")
		}
		node.Nodes = append(node.Nodes, p.ParseExpression(true))
	}
	return node
}

// parseNameOnlyTarget 对应 parse_assign_target(name_only=True).
func (p *Parser) parseNameOnlyTarget() *nodes.Name {
	token := p.expect("name")
	target := &nodes.Name{Name: token.Value, Ctx: "store"}
	target.Line = token.Lineno
	if !nodes.CanAssign(target) {
		p.Fail("can't assign to "+nodes.PyStrRepr(strings.ToLower(nodes.TypeName(target))),
			target.Line)
	}
	return target
}

// parseAssignTarget 对应 parse_assign_target.
func (p *Parser) parseAssignTarget(withTuple, nameOnly bool, extraEndRules []string,
	withNamespace bool) nodes.Expr {
	if nameOnly {
		return p.parseNameOnlyTarget()
	}
	var target nodes.Expr
	if withTuple {
		target = p.ParseTuple(true, true, extraEndRules, withNamespace)
	} else {
		target = p.parsePrimary(withNamespace)
	}
	nodes.SetCtx(target, "store")
	if !nodes.CanAssign(target) {
		p.Fail("can't assign to "+nodes.PyStrRepr(strings.ToLower(nodes.TypeName(target))),
			target.Lineno())
	}
	return target
}

// ParseExpression 对应 parse_expression.
func (p *Parser) ParseExpression(withCondexpr bool) nodes.Expr {
	if withCondexpr {
		return p.parseCondexpr()
	}
	return p.parseOr()
}

func (p *Parser) parseCondexpr() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	expr1 := p.parseOr()
	for p.Stream.SkipIf("name:if") {
		expr2 := p.parseOr()
		var expr3 nodes.Expr
		if p.Stream.SkipIf("name:else") {
			expr3 = p.parseCondexpr()
		}
		n := &nodes.CondExpr{Test: expr2, Expr1: expr1, Expr2: expr3}
		n.Line = lineno
		expr1 = n
		lineno = p.Stream.Current.Lineno
	}
	return expr1
}

func (p *Parser) parseOr() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	left := p.parseAnd()
	for p.Stream.SkipIf("name:or") {
		right := p.parseAnd()
		n := &nodes.BinExpr{Op: "or", Left: left, Right: right}
		n.Line = lineno
		left = n
		lineno = p.Stream.Current.Lineno
	}
	return left
}

func (p *Parser) parseAnd() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	left := p.parseNot()
	for p.Stream.SkipIf("name:and") {
		right := p.parseNot()
		n := &nodes.BinExpr{Op: "and", Left: left, Right: right}
		n.Line = lineno
		left = n
		lineno = p.Stream.Current.Lineno
	}
	return left
}

func (p *Parser) parseNot() nodes.Expr {
	if p.Stream.Current.Test("name:not") {
		lineno := p.next().Lineno
		n := &nodes.UnaryExpr{Op: "not", Node: p.parseNot()}
		n.Line = lineno
		return n
	}
	return p.parseCompare()
}

func (p *Parser) parseCompare() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	expr := p.parseMath1()
	var ops []*nodes.Operand
	addOp := func(op string) {
		operand := &nodes.Operand{Op: op, Expr: p.parseMath1()}
		operand.Line = p.Stream.Current.Lineno
		ops = append(ops, operand)
	}
	for {
		tokenType := p.Stream.Current.Type
		if compareOperators[tokenType] {
			p.next()
			addOp(string(tokenType))
		} else if p.Stream.SkipIf("name:in") {
			addOp("in")
		} else if p.Stream.Current.Test("name:not") && p.Stream.Look().Test("name:in") {
			p.Stream.Skip(2)
			addOp("notin")
		} else {
			break
		}
		lineno = p.Stream.Current.Lineno
	}
	if len(ops) == 0 {
		return expr
	}
	n := &nodes.Compare{Expr: expr, Ops: ops}
	n.Line = lineno
	return n
}

func (p *Parser) parseMath1() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	left := p.parseConcat()
	for p.Stream.Current.Type == lexer.TokenAdd || p.Stream.Current.Type == lexer.TokenSub {
		op := "+"
		if p.Stream.Current.Type == lexer.TokenSub {
			op = "-"
		}
		p.next()
		right := p.parseConcat()
		n := &nodes.BinExpr{Op: op, Left: left, Right: right}
		n.Line = lineno
		left = n
		lineno = p.Stream.Current.Lineno
	}
	return left
}

func (p *Parser) parseConcat() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	args := []nodes.Expr{p.parseMath2()}
	for p.Stream.Current.Type == lexer.TokenTilde {
		p.next()
		args = append(args, p.parseMath2())
	}
	if len(args) == 1 {
		return args[0]
	}
	n := &nodes.Concat{Nodes: args}
	n.Line = lineno
	return n
}

func (p *Parser) parseMath2() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	left := p.parsePow()
	for {
		var op string
		switch p.Stream.Current.Type {
		case lexer.TokenMul:
			op = "*"
		case lexer.TokenDiv:
			op = "/"
		case lexer.TokenFloordiv:
			op = "//"
		case lexer.TokenMod:
			op = "%"
		default:
			return left
		}
		p.next()
		right := p.parsePow()
		n := &nodes.BinExpr{Op: op, Left: left, Right: right}
		n.Line = lineno
		left = n
		lineno = p.Stream.Current.Lineno
	}
}

func (p *Parser) parsePow() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	left := p.parseUnary(true)
	for p.Stream.Current.Type == lexer.TokenPow {
		p.next()
		right := p.parseUnary(true)
		n := &nodes.BinExpr{Op: "**", Left: left, Right: right}
		n.Line = lineno
		left = n
		lineno = p.Stream.Current.Lineno
	}
	return left
}

func (p *Parser) parseUnary(withFilter bool) nodes.Expr {
	tokenType := p.Stream.Current.Type
	lineno := p.Stream.Current.Lineno
	var node nodes.Expr

	switch tokenType {
	case lexer.TokenSub:
		p.next()
		n := &nodes.UnaryExpr{Op: "-", Node: p.parseUnary(false)}
		n.Line = lineno
		node = n
	case lexer.TokenAdd:
		p.next()
		n := &nodes.UnaryExpr{Op: "+", Node: p.parseUnary(false)}
		n.Line = lineno
		node = n
	default:
		node = p.parsePrimary(false)
	}
	node = p.parsePostfix(node)
	if withFilter {
		node = p.parseFilterExpr(node)
	}
	return node
}

func (p *Parser) parsePrimary(withNamespace bool) nodes.Expr {
	token := p.Stream.Current
	switch token.Type {
	case lexer.TokenName:
		p.next()
		switch token.Value {
		case "true", "false", "True", "False":
			n := &nodes.Const{Value: token.Value == "true" || token.Value == "True"}
			n.Line = token.Lineno
			return n
		case "none", "None":
			n := &nodes.Const{Value: nil}
			n.Line = token.Lineno
			return n
		}
		if withNamespace && p.Stream.Current.Type == lexer.TokenDot {
			p.next()
			attr := p.expect("name")
			n := &nodes.NSRef{Name: token.Value, Attr: attr.Value}
			n.Line = token.Lineno
			return n
		}
		n := &nodes.Name{Name: token.Value, Ctx: "load"}
		n.Line = token.Lineno
		return n
	case lexer.TokenString:
		p.next()
		var buf strings.Builder
		buf.WriteString(token.Value)
		lineno := token.Lineno
		for p.Stream.Current.Type == lexer.TokenString {
			buf.WriteString(p.Stream.Current.Value)
			p.next()
		}
		n := &nodes.Const{Value: buf.String()}
		n.Line = lineno
		return n
	case lexer.TokenInteger:
		p.next()
		n := &nodes.Const{Value: token.IntVal}
		n.Line = token.Lineno
		return n
	case lexer.TokenFloat:
		p.next()
		n := &nodes.Const{Value: token.FloatVal}
		n.Line = token.Lineno
		return n
	case lexer.TokenLparen:
		p.next()
		node := p.parseTupleImpl(false, true, nil, true, false)
		p.expect("rparen")
		return node
	case lexer.TokenLbracket:
		return p.parseList()
	case lexer.TokenLbrace:
		return p.parseDict()
	}
	p.Fail("unexpected "+nodes.PyStrRepr(lexer.DescribeToken(token)), token.Lineno)
	return nil
}

// ParseTuple 对应 parse_tuple.
func (p *Parser) ParseTuple(simplified, withCondexpr bool, extraEndRules []string,
	withNamespace bool) nodes.Expr {
	return p.parseTupleImpl(simplified, withCondexpr, extraEndRules, false, withNamespace)
}

func (p *Parser) parseTupleImpl(simplified, withCondexpr bool, extraEndRules []string,
	explicitParens, withNamespace bool) nodes.Expr {
	lineno := p.Stream.Current.Lineno
	parse := func() nodes.Expr {
		if simplified {
			return p.parsePrimary(withNamespace)
		}
		return p.ParseExpression(withCondexpr)
	}

	var args []nodes.Expr
	isTuple := false
	for {
		if len(args) > 0 {
			p.expect("comma")
		}
		if p.isTupleEnd(extraEndRules) {
			break
		}
		args = append(args, parse())
		if p.Stream.Current.Type == lexer.TokenComma {
			isTuple = true
		} else {
			break
		}
		lineno = p.Stream.Current.Lineno
	}

	if !isTuple {
		if len(args) > 0 {
			return args[0]
		}
		// 没有显式括号时, 空元组不是合法表达式
		if !explicitParens {
			p.Fail("Expected an expression, got "+
				nodes.PyStrRepr(lexer.DescribeToken(p.Stream.Current)), 0)
		}
	}
	n := &nodes.Tuple{Items: args, Ctx: "load"}
	n.Line = lineno
	return n
}

func (p *Parser) parseList() nodes.Expr {
	token := p.expect("lbracket")
	var items []nodes.Expr
	for p.Stream.Current.Type != lexer.TokenRbracket {
		if len(items) > 0 {
			p.expect("comma")
		}
		if p.Stream.Current.Type == lexer.TokenRbracket {
			break
		}
		items = append(items, p.ParseExpression(true))
	}
	p.expect("rbracket")
	n := &nodes.List{Items: items}
	n.Line = token.Lineno
	return n
}

func (p *Parser) parseDict() nodes.Expr {
	token := p.expect("lbrace")
	var items []*nodes.Pair
	for p.Stream.Current.Type != lexer.TokenRbrace {
		if len(items) > 0 {
			p.expect("comma")
		}
		if p.Stream.Current.Type == lexer.TokenRbrace {
			break
		}
		key := p.ParseExpression(true)
		p.expect("colon")
		value := p.ParseExpression(true)
		pair := &nodes.Pair{Key: key, Value: value}
		pair.Line = key.Lineno()
		items = append(items, pair)
	}
	p.expect("rbrace")
	n := &nodes.Dict{Items: items}
	n.Line = token.Lineno
	return n
}

func (p *Parser) parsePostfix(node nodes.Expr) nodes.Expr {
	for {
		switch p.Stream.Current.Type {
		case lexer.TokenDot, lexer.TokenLbracket:
			node = p.parseSubscript(node)
		case lexer.TokenLparen:
			// getattr / getitem 之后和 filter / test 之后都允许调用
			node = p.parseCallNode(node)
		default:
			return node
		}
	}
}

func (p *Parser) parseFilterExpr(node nodes.Expr) nodes.Expr {
	for {
		switch {
		case p.Stream.Current.Type == lexer.TokenPipe:
			node = p.parseFilterChain(node, false)
		case p.Stream.Current.Type == lexer.TokenName && p.Stream.Current.Value == "is":
			node = p.parseTest(node)
		case p.Stream.Current.Type == lexer.TokenLparen:
			node = p.parseCallNode(node)
		default:
			return node
		}
	}
}

func (p *Parser) parseSubscript(node nodes.Expr) nodes.Expr {
	token := p.next()
	if token.Type == lexer.TokenDot {
		attrToken := p.Stream.Current
		p.next()
		if attrToken.Type == lexer.TokenName {
			n := &nodes.Getattr{Node: node, Attr: attrToken.Value, Ctx: "load"}
			n.Line = token.Lineno
			return n
		} else if attrToken.Type != lexer.TokenInteger {
			p.Fail("expected name or number", attrToken.Lineno)
		}
		arg := &nodes.Const{Value: attrToken.IntVal}
		arg.Line = attrToken.Lineno
		n := &nodes.Getitem{Node: node, Arg: arg, Ctx: "load"}
		n.Line = token.Lineno
		return n
	}
	if token.Type == lexer.TokenLbracket {
		var args []nodes.Expr
		for p.Stream.Current.Type != lexer.TokenRbracket {
			if len(args) > 0 {
				p.expect("comma")
			}
			args = append(args, p.parseSubscribed())
		}
		p.expect("rbracket")
		var arg nodes.Expr
		if len(args) == 1 {
			arg = args[0]
		} else {
			tup := &nodes.Tuple{Items: args, Ctx: "load"}
			tup.Line = token.Lineno
			arg = tup
		}
		n := &nodes.Getitem{Node: node, Arg: arg, Ctx: "load"}
		n.Line = token.Lineno
		return n
	}
	p.Fail("expected subscript expression", token.Lineno)
	return nil
}

func (p *Parser) parseSubscribed() nodes.Expr {
	lineno := p.Stream.Current.Lineno
	var args []nodes.Expr

	if p.Stream.Current.Type == lexer.TokenColon {
		p.next()
		args = []nodes.Expr{nil}
	} else {
		node := p.ParseExpression(true)
		if p.Stream.Current.Type != lexer.TokenColon {
			return node
		}
		p.next()
		args = []nodes.Expr{node}
	}

	if p.Stream.Current.Type == lexer.TokenColon {
		args = append(args, nil)
	} else if p.Stream.Current.Type != lexer.TokenRbracket &&
		p.Stream.Current.Type != lexer.TokenComma {
		args = append(args, p.ParseExpression(true))
	} else {
		args = append(args, nil)
	}

	if p.Stream.Current.Type == lexer.TokenColon {
		p.next()
		if p.Stream.Current.Type != lexer.TokenRbracket &&
			p.Stream.Current.Type != lexer.TokenComma {
			args = append(args, p.ParseExpression(true))
		} else {
			args = append(args, nil)
		}
	} else {
		args = append(args, nil)
	}

	n := &nodes.Slice{Start: args[0], Stop: args[1], Step: args[2]}
	n.Line = lineno
	return n
}

func (p *Parser) parseCallArgs() (args []nodes.Expr, kwargs []*nodes.Keyword,
	dynArgs, dynKwargs nodes.Expr) {
	token := p.expect("lparen")
	requireComma := false

	ensure := func(expr bool) {
		if !expr {
			p.Fail("invalid syntax for function call expression", token.Lineno)
		}
	}

	for p.Stream.Current.Type != lexer.TokenRparen {
		if requireComma {
			p.expect("comma")
			// 允许尾随逗号
			if p.Stream.Current.Type == lexer.TokenRparen {
				break
			}
		}
		if p.Stream.Current.Type == lexer.TokenMul {
			ensure(dynArgs == nil && dynKwargs == nil)
			p.next()
			dynArgs = p.ParseExpression(true)
		} else if p.Stream.Current.Type == lexer.TokenPow {
			ensure(dynKwargs == nil)
			p.next()
			dynKwargs = p.ParseExpression(true)
		} else {
			if p.Stream.Current.Type == lexer.TokenName &&
				p.Stream.Look().Type == lexer.TokenAssign {
				// 关键字参数
				ensure(dynKwargs == nil)
				key := p.Stream.Current.Value
				p.Stream.Skip(2)
				value := p.ParseExpression(true)
				kw := &nodes.Keyword{Key: key, Value: value}
				kw.Line = value.Lineno()
				kwargs = append(kwargs, kw)
			} else {
				// 位置参数
				ensure(dynArgs == nil && dynKwargs == nil && len(kwargs) == 0)
				args = append(args, p.ParseExpression(true))
			}
		}
		requireComma = true
	}
	p.expect("rparen")
	return args, kwargs, dynArgs, dynKwargs
}

func (p *Parser) parseCallNode(node nodes.Expr) nodes.Expr {
	token := p.Stream.Current
	args, kwargs, dynArgs, dynKwargs := p.parseCallArgs()
	n := &nodes.Call{Node: node, Args: args, Kwargs: kwargs,
		DynArgs: dynArgs, DynKwargs: dynKwargs}
	n.Line = token.Lineno
	return n
}

// parseFilterChain 对应 parse_filter.
func (p *Parser) parseFilterChain(node nodes.Expr, startInline bool) *nodes.Filter {
	var result *nodes.Filter
	for p.Stream.Current.Type == lexer.TokenPipe || startInline {
		if !startInline {
			p.next()
		}
		token := p.expect("name")
		name := token.Value
		for p.Stream.Current.Type == lexer.TokenDot {
			p.next()
			name += "." + p.expect("name").Value
		}
		var args []nodes.Expr
		var kwargs []*nodes.Keyword
		var dynArgs, dynKwargs nodes.Expr
		if p.Stream.Current.Type == lexer.TokenLparen {
			args, kwargs, dynArgs, dynKwargs = p.parseCallArgs()
		}
		f := &nodes.Filter{Node: node, Name: name, Args: args, Kwargs: kwargs,
			DynArgs: dynArgs, DynKwargs: dynKwargs}
		f.Line = token.Lineno
		node = f
		result = f
		startInline = false
	}
	return result
}

func (p *Parser) parseTest(node nodes.Expr) nodes.Expr {
	token := p.next()
	negated := false
	if p.Stream.Current.Test("name:not") {
		p.next()
		negated = true
	}
	name := p.expect("name").Value
	for p.Stream.Current.Type == lexer.TokenDot {
		p.next()
		name += "." + p.expect("name").Value
	}
	var args []nodes.Expr
	var kwargs []*nodes.Keyword
	var dynArgs, dynKwargs nodes.Expr
	if p.Stream.Current.Type == lexer.TokenLparen {
		args, kwargs, dynArgs, dynKwargs = p.parseCallArgs()
	} else if isTestArgStart(p.Stream.Current) &&
		!p.Stream.Current.TestAny("name:else", "name:or", "name:and") {
		if p.Stream.Current.Test("name:is") {
			p.Fail("You cannot chain multiple tests with is", 0)
		}
		argNode := p.parsePrimary(false)
		argNode = p.parsePostfix(argNode)
		args = []nodes.Expr{argNode}
	}
	testNode := &nodes.Test{Node: node, Name: name, Args: args, Kwargs: kwargs,
		DynArgs: dynArgs, DynKwargs: dynKwargs}
	testNode.Line = token.Lineno
	if negated {
		n := &nodes.UnaryExpr{Op: "not", Node: testNode}
		n.Line = token.Lineno
		return n
	}
	return testNode
}

func isTestArgStart(t lexer.Token) bool {
	switch t.Type {
	case lexer.TokenName, lexer.TokenString, lexer.TokenInteger, lexer.TokenFloat,
		lexer.TokenLparen, lexer.TokenLbracket, lexer.TokenLbrace:
		return true
	}
	return false
}

// subparse 对应 Parser.subparse.
func (p *Parser) subparse(endTokens []string) []nodes.Node {
	var body []nodes.Node
	var dataBuffer []nodes.Expr

	if endTokens != nil {
		p.endTokenStack = append(p.endTokenStack, endTokens)
		defer func() {
			p.endTokenStack = p.endTokenStack[:len(p.endTokenStack)-1]
		}()
	}

	flushData := func() {
		if len(dataBuffer) > 0 {
			out := &nodes.Output{Nodes: dataBuffer}
			out.Line = dataBuffer[0].Lineno()
			body = append(body, out)
			dataBuffer = nil
		}
	}

	for p.Stream.Bool() {
		token := p.Stream.Current
		switch token.Type {
		case lexer.TokenData:
			if token.Value != "" {
				td := &nodes.TemplateData{Data: token.Value}
				td.Line = token.Lineno
				dataBuffer = append(dataBuffer, td)
			}
			p.next()
		case lexer.TokenVariableBegin:
			p.next()
			dataBuffer = append(dataBuffer, p.parseTupleImpl(false, true, nil, false, false))
			p.expect("variable_end")
		case lexer.TokenBlockBegin:
			flushData()
			p.next()
			if endTokens != nil && p.Stream.Current.TestAny(endTokens...) {
				return body
			}
			body = append(body, p.parseStatement()...)
			p.expect("block_end")
		default:
			panic("gojinja2/parser: internal parsing error")
		}
	}
	flushData()
	return body
}

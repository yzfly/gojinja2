// Package lexer 实现 Jinja2 模板的词法分析器, 移植自 jinja2/lexer.py (3.1.6).
//
// 官方实现基于 Python regex (依赖 lookbehind/lookahead, Go RE2 不支持),
// 这里改为手写扫描器, 逐条复刻原正则的匹配语义.
package lexer

import (
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
)

// Type 是 token 类型. 与 Python 实现一致使用字符串,
// 以便支持 "name:if" 这类 token 表达式测试.
type Type string

const (
	TokenAdd        Type = "add"
	TokenAssign     Type = "assign"
	TokenColon      Type = "colon"
	TokenComma      Type = "comma"
	TokenDiv        Type = "div"
	TokenDot        Type = "dot"
	TokenEq         Type = "eq"
	TokenFloordiv   Type = "floordiv"
	TokenGt         Type = "gt"
	TokenGteq       Type = "gteq"
	TokenLbrace     Type = "lbrace"
	TokenLbracket   Type = "lbracket"
	TokenLparen     Type = "lparen"
	TokenLt         Type = "lt"
	TokenLteq       Type = "lteq"
	TokenMod        Type = "mod"
	TokenMul        Type = "mul"
	TokenNe         Type = "ne"
	TokenPipe       Type = "pipe"
	TokenPow        Type = "pow"
	TokenRbrace     Type = "rbrace"
	TokenRbracket   Type = "rbracket"
	TokenRparen     Type = "rparen"
	TokenSemicolon  Type = "semicolon"
	TokenSub        Type = "sub"
	TokenTilde      Type = "tilde"
	TokenWhitespace Type = "whitespace"
	TokenFloat      Type = "float"
	TokenInteger    Type = "integer"
	TokenName       Type = "name"
	TokenString     Type = "string"
	TokenOperator   Type = "operator"

	TokenBlockBegin         Type = "block_begin"
	TokenBlockEnd           Type = "block_end"
	TokenVariableBegin      Type = "variable_begin"
	TokenVariableEnd        Type = "variable_end"
	TokenRawBegin           Type = "raw_begin"
	TokenRawEnd             Type = "raw_end"
	TokenCommentBegin       Type = "comment_begin"
	TokenCommentEnd         Type = "comment_end"
	TokenComment            Type = "comment"
	TokenLinestatementBegin Type = "linestatement_begin"
	TokenLinestatementEnd   Type = "linestatement_end"
	TokenLinecommentBegin   Type = "linecomment_begin"
	TokenLinecommentEnd     Type = "linecomment_end"
	TokenLinecomment        Type = "linecomment"
	TokenData               Type = "data"
	TokenInitial            Type = "initial"
	TokenEOF                Type = "eof"
)

// operators 对应 lexer.py 的 operators 表.
var operatorTypes = map[string]Type{
	"+": TokenAdd, "-": TokenSub, "/": TokenDiv, "//": TokenFloordiv,
	"*": TokenMul, "%": TokenMod, "**": TokenPow, "~": TokenTilde,
	"[": TokenLbracket, "]": TokenRbracket, "(": TokenLparen, ")": TokenRparen,
	"{": TokenLbrace, "}": TokenRbrace, "==": TokenEq, "!=": TokenNe,
	">": TokenGt, ">=": TokenGteq, "<": TokenLt, "<=": TokenLteq,
	"=": TokenAssign, ".": TokenDot, ":": TokenColon, "|": TokenPipe,
	",": TokenComma, ";": TokenSemicolon,
}

var reverseOperators = func() map[Type]string {
	m := make(map[Type]string, len(operatorTypes))
	for k, v := range operatorTypes {
		m[v] = k
	}
	return m
}()

// 按长度降序排列的运算符列表, 模拟 operator_re 的最长优先匹配.
var sortedOperators = []string{
	"//", "**", "==", "!=", ">=", "<=",
	"+", "-", "/", "*", "%", "~", "[", "]", "(", ")", "{", "}",
	">", "<", "=", ".", ":", "|", ",", ";",
}

// ignoredTokens 在 wrap 阶段被丢弃, 对应 lexer.py 的 ignored_tokens.
var ignoredTokens = map[Type]bool{
	TokenCommentBegin: true, TokenComment: true, TokenCommentEnd: true,
	TokenWhitespace: true, TokenLinecommentBegin: true,
	TokenLinecommentEnd: true, TokenLinecomment: true,
}

// ignoreIfEmpty 对应 lexer.py 的 ignore_if_empty.
var ignoreIfEmpty = map[Type]bool{
	TokenWhitespace: true, TokenData: true,
	TokenComment: true, TokenLinecomment: true,
}

// Token 是一个词法单元.
// 与 Python 不同, 数值字面量的解析结果存放在独立字段中以避免 interface 装箱:
// Type==TokenInteger 时取 IntVal, Type==TokenFloat 时取 FloatVal,
// 其余类型取 Value (name/string/data 为处理后的文本).
type Token struct {
	Lineno   int
	Type     Type
	Value    string
	IntVal   int64
	FloatVal float64
}

func describeTokenType(t Type) string {
	if op, ok := reverseOperators[t]; ok {
		return op
	}
	switch t {
	case TokenCommentBegin:
		return "begin of comment"
	case TokenCommentEnd:
		return "end of comment"
	case TokenComment, TokenLinecomment:
		return "comment"
	case TokenBlockBegin:
		return "begin of statement block"
	case TokenBlockEnd:
		return "end of statement block"
	case TokenVariableBegin:
		return "begin of print statement"
	case TokenVariableEnd:
		return "end of print statement"
	case TokenLinestatementBegin:
		return "begin of line statement"
	case TokenLinestatementEnd:
		return "end of line statement"
	case TokenData:
		return "template data / text"
	case TokenEOF:
		return "end of template"
	}
	return string(t)
}

// DescribeToken 返回 token 的人类可读描述, 对应 describe_token.
func DescribeToken(t Token) string {
	if t.Type == TokenName {
		return t.Value
	}
	return describeTokenType(t.Type)
}

// DescribeTokenExpr 对应 describe_token_expr.
func DescribeTokenExpr(expr string) string {
	if i := strings.IndexByte(expr, ':'); i >= 0 {
		typ, value := expr[:i], expr[i+1:]
		if typ == string(TokenName) {
			return value
		}
		return describeTokenType(Type(typ))
	}
	return describeTokenType(Type(expr))
}

// Test 用 token 表达式测试该 token, 形式为 "token_type" 或 "token_type:token_value".
func (t Token) Test(expr string) bool {
	if string(t.Type) == expr {
		return true
	}
	if i := strings.IndexByte(expr, ':'); i >= 0 {
		return expr[:i] == string(t.Type) && expr[i+1:] == t.Value
	}
	return false
}

// TestAny 对多个 token 表达式做测试.
func (t Token) TestAny(exprs ...string) bool {
	for _, e := range exprs {
		if t.Test(e) {
			return true
		}
	}
	return false
}

func (t Token) String() string { return DescribeToken(t) }

// TokenStream 是 token 流. 解析器通过 Next 前进, Current 为当前 token.
// 移植自 lexer.py 的 TokenStream.
type TokenStream struct {
	tokens   []Token
	idx      int
	pushed   []Token
	Name     string
	Filename string
	Closed   bool
	Current  Token
	// deferredErr 是源码后段的词法错误: 复刻 Python 惰性分词的语义,
	// 仅当解析器前进到错误位置时才抛出 (panic).
	deferredErr error
}

// NewTokenStream 由已完成 wrap 的 token 序列构造流.
func NewTokenStream(tokens []Token, name, filename string) *TokenStream {
	s := &TokenStream{tokens: tokens, Name: name, Filename: filename,
		Current: Token{Lineno: 1, Type: TokenInitial}}
	s.Next()
	return s
}

// Bool 对应 Python 的 __bool__: 流未结束时为 true.
func (s *TokenStream) Bool() bool {
	return len(s.pushed) > 0 || s.Current.Type != TokenEOF
}

// EOS 是否到达流末尾.
func (s *TokenStream) EOS() bool { return !s.Bool() }

// Push 把一个 token 推回流中.
func (s *TokenStream) Push(t Token) { s.pushed = append(s.pushed, t) }

// Look 偷看下一个 token (不消费).
func (s *TokenStream) Look() Token {
	old := s.Next()
	result := s.Current
	s.Push(result)
	s.Current = old
	return result
}

// Skip 向前跳过 n 个 token.
func (s *TokenStream) Skip(n int) {
	for i := 0; i < n; i++ {
		s.Next()
	}
}

// NextIf 当前 token 匹配表达式时消费并返回它, 否则返回 nil.
func (s *TokenStream) NextIf(expr string) *Token {
	if s.Current.Test(expr) {
		t := s.Next()
		return &t
	}
	return nil
}

// SkipIf 同 NextIf 但仅返回是否匹配.
func (s *TokenStream) SkipIf(expr string) bool { return s.NextIf(expr) != nil }

// Next 前进一个 token 并返回旧的当前 token.
func (s *TokenStream) Next() Token {
	rv := s.Current
	if len(s.pushed) > 0 {
		s.Current = s.pushed[0]
		s.pushed = s.pushed[1:]
	} else if s.Current.Type != TokenEOF {
		if s.idx < len(s.tokens) {
			s.Current = s.tokens[s.idx]
			s.idx++
		} else if s.deferredErr != nil {
			err := s.deferredErr
			s.deferredErr = nil
			panic(err)
		} else {
			s.Close()
		}
	}
	return rv
}

// Close 关闭流.
func (s *TokenStream) Close() {
	s.Current = Token{Lineno: s.Current.Lineno, Type: TokenEOF}
	s.Closed = true
}

// Expect 断言当前 token 匹配表达式并消费它, 否则返回语法错误.
func (s *TokenStream) Expect(expr string) (Token, error) {
	if !s.Current.Test(expr) {
		desc := DescribeTokenExpr(expr)
		if s.Current.Type == TokenEOF {
			return Token{}, exceptions.NewSyntaxError(
				"unexpected end of template, expected '"+desc+"'.",
				s.Current.Lineno, s.Name, s.Filename)
		}
		return Token{}, exceptions.NewSyntaxError(
			"expected token '"+desc+"', got '"+DescribeToken(s.Current)+"'",
			s.Current.Lineno, s.Name, s.Filename)
	}
	return s.Next(), nil
}

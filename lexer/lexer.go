package lexer

import (
	"strconv"
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
)

// Config 是 lexer 所需的环境配置, 对应 Environment 上与词法相关的选项.
type Config struct {
	BlockStart          string // "{%"
	BlockEnd            string // "%}"
	VariableStart       string // "{{"
	VariableEnd         string // "}}"
	CommentStart        string // "{#"
	CommentEnd          string // "#}"
	LineStatementPrefix string // "" 表示禁用
	LineCommentPrefix   string // "" 表示禁用
	TrimBlocks          bool
	LstripBlocks        bool
	NewlineSequence     string // 默认 "\n"
	KeepTrailingNewline bool
}

// DefaultConfig 返回与 jinja2.defaults 一致的默认配置.
func DefaultConfig() Config {
	return Config{
		BlockStart: "{%", BlockEnd: "%}",
		VariableStart: "{{", VariableEnd: "}}",
		CommentStart: "{#", CommentEnd: "#}",
		NewlineSequence: "\n",
	}
}

// rootRule 是 root 状态下的一个 tag 起始规则.
// 对应 compile_rules 的产物, 按 (起始串长度, 规则名) 降序排列.
type rootRule struct {
	name  Type   // block_begin / variable_begin / comment_begin / linestatement_begin / linecomment_begin
	start string // 起始定界符或行前缀
}

// Lexer 持有编译后的扫描规则. 多个环境可共享同一个 Lexer.
type Lexer struct {
	cfg       Config
	rootRules []rootRule
}

// New 由配置编译一个 Lexer.
func New(cfg Config) *Lexer {
	rules := []rootRule{
		{TokenCommentBegin, cfg.CommentStart},
		{TokenBlockBegin, cfg.BlockStart},
		{TokenVariableBegin, cfg.VariableStart},
	}
	if cfg.LineStatementPrefix != "" {
		rules = append(rules, rootRule{TokenLinestatementBegin, cfg.LineStatementPrefix})
	}
	if cfg.LineCommentPrefix != "" {
		rules = append(rules, rootRule{TokenLinecommentBegin, cfg.LineCommentPrefix})
	}
	// 对应 compile_rules 的 sorted(rules, reverse=True):
	// 先比起始串长度, 再比规则名 (字符串降序), 以保证共享前缀的定界符
	// (如 "<?" 与 "<?=") 长者优先.
	for i := 0; i < len(rules); i++ {
		for j := i + 1; j < len(rules); j++ {
			a, b := rules[i], rules[j]
			if len(b.start) > len(a.start) ||
				(len(b.start) == len(a.start) && string(b.name) > string(a.name)) {
				rules[i], rules[j] = rules[j], rules[i]
			}
		}
	}
	return &Lexer{cfg: cfg, rootRules: rules}
}

// Tokenize 对模板源码做完整词法分析, 返回 TokenStream.
// state 可为 "" (root), "variable" 或 "block" (供扩展使用的初始状态).
//
// 词法错误的语义与 Python 的惰性分词一致: 错误位置之前的 token 正常
// 提供, 解析器前进到错误位置时才抛出. 首 token 即错误时直接返回 error.
func (l *Lexer) Tokenize(source, name, filename, state string) (*TokenStream, error) {
	raw, scanErr := l.tokeniter(source, name, filename, state)
	wrapped, wrapErr := l.wrap(raw, name, filename)
	deferred := wrapErr
	if deferred == nil {
		deferred = scanErr
	}
	if len(wrapped) == 0 && deferred != nil {
		return nil, deferred
	}
	stream := NewTokenStream(wrapped, name, filename)
	stream.deferredErr = deferred
	return stream, nil
}

// normalizeNewlines 把 \n 替换为配置的换行序列.
// 注意源码在 tokeniter 预处理时已统一为 \n.
func (l *Lexer) normalizeNewlines(s string) string {
	if l.cfg.NewlineSequence == "\n" {
		return s
	}
	return strings.ReplaceAll(s, "\n", l.cfg.NewlineSequence)
}

// wrap 把原始 token 序列转换为解析器需要的形式, 对应 Lexer.wrap:
// 丢弃注释和空白, 行语句映射为块语句, 解析字符串/数字字面量.
// 出错时返回错误前已转换的 token (延迟错误语义).
func (l *Lexer) wrap(raw []rawToken, name, filename string) ([]Token, error) {
	out := make([]Token, 0, len(raw))
	for _, rt := range raw {
		if ignoredTokens[rt.typ] {
			continue
		}
		tok := Token{Lineno: rt.lineno, Type: rt.typ, Value: rt.value}
		switch rt.typ {
		case TokenLinestatementBegin:
			tok.Type = TokenBlockBegin
		case TokenLinestatementEnd:
			tok.Type = TokenBlockEnd
		case TokenRawBegin, TokenRawEnd:
			// 解析器不关心这两个 token
			continue
		case TokenData:
			tok.Value = l.normalizeNewlines(rt.value)
		case TokenName:
			if !isIdentifier(rt.value) {
				return out, exceptions.NewSyntaxError(
					"Invalid character in identifier", rt.lineno, name, filename)
			}
		case TokenString:
			// 与 Python 一致: 先规范化换行, 再做 unicode-escape 解码
			s, err := pyUnescape(l.normalizeNewlines(rt.value[1 : len(rt.value)-1]))
			if err != nil {
				return out, exceptions.NewSyntaxError(err.Error(), rt.lineno, name, filename)
			}
			tok.Value = s
		case TokenInteger:
			n, err := strconv.ParseInt(strings.ReplaceAll(rt.value, "_", ""), 0, 64)
			if err != nil {
				// int64 溢出等. Python 为任意精度整数, 这是文档化的差异.
				return out, exceptions.NewSyntaxError(
					"integer literal out of int64 range: "+rt.value,
					rt.lineno, name, filename)
			}
			tok.IntVal = n
		case TokenFloat:
			f, err := strconv.ParseFloat(strings.ReplaceAll(rt.value, "_", ""), 64)
			if err != nil {
				return out, exceptions.NewSyntaxError(
					"invalid float literal: "+rt.value, rt.lineno, name, filename)
			}
			tok.FloatVal = f
		case TokenOperator:
			tok.Type = operatorTypes[rt.value]
		}
		out = append(out, tok)
	}
	return out, nil
}

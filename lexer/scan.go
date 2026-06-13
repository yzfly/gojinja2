package lexer

import (
	"fmt"
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
)

// rawToken 是 tokeniter 产出的原始词法单元 (尚未做字面量解析).
type rawToken struct {
	lineno int
	typ    Type
	value  string
}

// scanner 复刻 Lexer.tokeniter 的状态机.
// 官方实现是惰性正则驱动; 这里手写扫描并一次性产出全部 token.
type scanner struct {
	l        *Lexer
	src      string
	name     string
	filename string

	pos          int
	lineno       int
	stack        []string // 状态栈, 键与 Python rules 字典一致
	balancing    []byte   // 期望的右括号栈
	lineStarting bool
	out          []rawToken
}

func (l *Lexer) tokeniter(source, name, filename, state string) ([]rawToken, error) {
	// 预处理: 换行统一为 \n; 默认去掉源码末尾的单个换行
	lines := splitNewlines(source)
	if !l.cfg.KeepTrailingNewline && len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	s := &scanner{
		l: l, src: strings.Join(lines, "\n"),
		name: name, filename: filename,
		lineno: 1, stack: []string{"root"}, lineStarting: true,
	}
	if state == "variable" || state == "block" {
		s.stack = append(s.stack, state+"_begin")
	}
	// 错误前已扫出的 token 一并返回 (惰性分词的延迟错误语义)
	err := s.run()
	return s.out, err
}

// splitNewlines 按 \r\n | \r | \n 切分, 对应 newline_re.split(source)[::2].
func splitNewlines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			lines = append(lines, s[start:i])
			start = i + 1
		case '\r':
			lines = append(lines, s[start:i])
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	return append(lines, s[start:])
}

func (s *scanner) emit(lineno int, typ Type, value string) {
	s.out = append(s.out, rawToken{lineno, typ, value})
}

// advance 记录一次成功匹配: 计入换行数并维护 line_starting.
// 对应 Python 在每次规则匹配后的公共收尾.
func (s *scanner) bump(value string) {
	s.lineno += strings.Count(value, "\n")
	if value != "" {
		s.lineStarting = strings.HasSuffix(value, "\n")
	}
}

func (s *scanner) syntaxErr(msg string) error {
	return exceptions.NewSyntaxError(msg, s.lineno, s.name, s.filename)
}

func (s *scanner) run() error {
	for {
		state := s.stack[len(s.stack)-1]
		var (
			matched bool
			err     error
		)
		switch state {
		case "root":
			matched, err = s.stepRoot()
		case "block_begin", "variable_begin", "linestatement_begin":
			matched, err = s.stepTag(state)
		case "comment_begin":
			matched, err = s.stepComment()
		case "raw_begin":
			matched, err = s.stepRaw()
		case "linecomment_begin":
			matched, err = s.stepLinecomment()
		default:
			return fmt.Errorf("gojinja2/lexer: 未知状态 %q", state)
		}
		if err != nil {
			return err
		}
		if !matched {
			// 所有规则都不匹配: 到达模板末尾则正常结束, 否则是非法字符
			if s.pos >= len(s.src) {
				return nil
			}
			return s.syntaxErr(fmt.Sprintf("unexpected char %s at %d",
				pyCharRepr(s.src[s.pos:]), s.pos))
		}
	}
}

// ---- root 状态 ----

// rootMatch 表示 root 规则在某个位置命中的一个 tag 起始.
type rootMatch struct {
	typ   Type   // raw_begin / block_begin / ...
	text  string // 命中的完整文本 (起始串+符号, raw 含整个 {% raw %} 标签)
	sign  string // "-", "+" 或 ""
	state string // 进入的新状态
}

func (s *scanner) stepRoot() (bool, error) {
	if s.pos >= len(s.src) {
		return false, nil
	}
	// 模拟 (.*?)(?:raw|...) 的惰性匹配: 从 pos 起找最早能命中的位置;
	// 同一位置按 raw 优先、其余按编译序尝试.
	for k := s.pos; k < len(s.src); k++ {
		m, ok := s.tryTagAt(k)
		if !ok {
			continue
		}
		data, nlStripped := s.applyStrip(s.src[s.pos:k], m.sign, m.typ == TokenVariableBegin)
		if data != "" {
			s.emit(s.lineno, TokenData, data)
		}
		s.lineno += strings.Count(data, "\n") + nlStripped
		s.emit(s.lineno, m.typ, m.text)
		s.lineno += strings.Count(m.text, "\n")
		s.lineStarting = strings.HasSuffix(m.text, "\n")
		s.pos = k + len(m.text)
		s.stack = append(s.stack, m.state)
		return true, nil
	}
	// 没有任何 tag: 余下全部是 data (对应 root 的第二条规则 ".+")
	data := s.src[s.pos:]
	s.emit(s.lineno, TokenData, data)
	s.bump(data)
	s.pos = len(s.src)
	return true, nil
}

// tryTagAt 在位置 k 依优先级尝试各 tag 起始.
func (s *scanner) tryTagAt(k int) (rootMatch, bool) {
	// raw 永远第一优先 (对应 root_raw_re 在 alternation 中位列最前)
	if text, sign, ok := s.tryRawBegin(k); ok {
		return rootMatch{TokenRawBegin, text, sign, "raw_begin"}, true
	}
	for _, r := range s.l.rootRules {
		p := k
		switch r.name {
		case TokenLinestatementBegin:
			// ^[ \t\v]*PREFIX(\-|\+|)
			if !(k == 0 || s.src[k-1] == '\n') {
				continue
			}
			for p < len(s.src) && (s.src[p] == ' ' || s.src[p] == '\t' || s.src[p] == '\v') {
				p++
			}
		case TokenLinecommentBegin:
			// (?:^|(?<=\S))[^\S\r\n]*PREFIX(\-|\+|)
			if !(k == 0 || s.src[k-1] == '\n' || !isSpaceByte(s.src[k-1])) {
				continue
			}
			p = skipInlineSpace(s.src, k)
		}
		if !strings.HasPrefix(s.src[p:], r.start) {
			continue
		}
		p += len(r.start)
		sign := ""
		if signLen(s.src, p) > 0 {
			sign = s.src[p : p+1]
			p++
		}
		return rootMatch{r.name, s.src[k:p], sign, string(r.name)}, true
	}
	return rootMatch{}, false
}

// tryRawBegin 匹配 {%(\-|\+|)\s*raw\s*(?:\-%}\s*|%}), 对应 root_raw_re.
func (s *scanner) tryRawBegin(k int) (text, sign string, ok bool) {
	cfg := &s.l.cfg
	if !strings.HasPrefix(s.src[k:], cfg.BlockStart) {
		return "", "", false
	}
	p := k + len(cfg.BlockStart)
	if n := signLen(s.src, p); n > 0 {
		sign = s.src[p : p+1]
		p++
	}
	p = skipSpace(s.src, p)
	if !strings.HasPrefix(s.src[p:], "raw") {
		return "", "", false
	}
	p = skipSpace(s.src, p+3)
	if strings.HasPrefix(s.src[p:], "-"+cfg.BlockEnd) {
		p = skipSpace(s.src, p+1+len(cfg.BlockEnd))
		return s.src[k:p], sign, true
	}
	if strings.HasPrefix(s.src[p:], cfg.BlockEnd) {
		return s.src[k : p+len(cfg.BlockEnd)], sign, true
	}
	return "", "", false
}

// applyStrip 实现 OptionalLStrip 的空白控制语义:
// '-' 删除 tag 前的全部空白; 否则在 lstrip_blocks 开启且非变量 tag 时,
// 删除行首到 tag 之间的纯空白.
// 返回处理后的 data 与被删除的换行数 (用于行号修正).
func (s *scanner) applyStrip(data, sign string, isVariable bool) (string, int) {
	if sign == "-" {
		stripped := strings.TrimRightFunc(data, isSpaceRune)
		return stripped, strings.Count(data[len(stripped):], "\n")
	}
	if sign != "+" && s.l.cfg.LstripBlocks && !isVariable {
		lpos := strings.LastIndexByte(data, '\n') + 1
		if lpos > 0 || s.lineStarting {
			rest := data[lpos:]
			if rest != "" && isAllSpace(rest) {
				return data[:lpos], 0
			}
		}
	}
	return data, 0
}

// ---- 注释状态 ----

func (s *scanner) stepComment() (bool, error) {
	cfg := &s.l.cfg
	// (.*?)((?:\+#}|\-#}\s*|#}\n?)) — 找最早命中的结束 tag
	for k := s.pos; k < len(s.src); k++ {
		end, ok := s.matchTagEnd(k, cfg.CommentEnd, true, cfg.TrimBlocks)
		if !ok {
			continue
		}
		data := s.src[s.pos:k]
		if data != "" {
			s.emit(s.lineno, TokenComment, data)
		}
		s.lineno += strings.Count(data, "\n")
		s.emit(s.lineno, TokenCommentEnd, end)
		s.bump(end)
		s.pos = k + len(end)
		s.stack = s.stack[:len(s.stack)-1]
		return true, nil
	}
	if s.pos >= len(s.src) {
		return false, nil
	}
	return false, s.syntaxErr("Missing end of comment tag")
}

// matchTagEnd 在位置 k 匹配结束定界符, 按 Python alternation 顺序:
//
//	\+END | \-END\s* | END(\n? if trim)
//
// allowPlus=false 时跳过第一个分支 (variable_end 没有 + 变体).
func (s *scanner) matchTagEnd(k int, end string, allowPlus, trim bool) (string, bool) {
	src := s.src
	if allowPlus && k < len(src) && src[k] == '+' && strings.HasPrefix(src[k+1:], end) {
		return src[k : k+1+len(end)], true
	}
	if k < len(src) && src[k] == '-' && strings.HasPrefix(src[k+1:], end) {
		p := skipSpace(src, k+1+len(end))
		return src[k:p], true
	}
	if strings.HasPrefix(src[k:], end) {
		p := k + len(end)
		if trim && p < len(src) && src[p] == '\n' {
			p++
		}
		return src[k:p], true
	}
	return "", false
}

// ---- raw 状态 ----

func (s *scanner) stepRaw() (bool, error) {
	// (.*?)({%(\-|\+|)\s*endraw\s*(?:\+%}|\-%}\s*|%}\n?))
	for k := s.pos; k < len(s.src); k++ {
		text, sign, ok := s.tryEndraw(k)
		if !ok {
			continue
		}
		data, nlStripped := s.applyStrip(s.src[s.pos:k], sign, false)
		if data != "" {
			s.emit(s.lineno, TokenData, data)
		}
		s.lineno += strings.Count(data, "\n") + nlStripped
		s.emit(s.lineno, TokenRawEnd, text)
		s.lineno += strings.Count(text, "\n")
		s.lineStarting = strings.HasSuffix(text, "\n")
		s.pos = k + len(text)
		s.stack = s.stack[:len(s.stack)-1]
		return true, nil
	}
	if s.pos >= len(s.src) {
		return false, nil
	}
	return false, s.syntaxErr("Missing end of raw directive")
}

func (s *scanner) tryEndraw(k int) (text, sign string, ok bool) {
	cfg := &s.l.cfg
	if !strings.HasPrefix(s.src[k:], cfg.BlockStart) {
		return "", "", false
	}
	p := k + len(cfg.BlockStart)
	if n := signLen(s.src, p); n > 0 {
		sign = s.src[p : p+1]
		p++
	}
	p = skipSpace(s.src, p)
	if !strings.HasPrefix(s.src[p:], "endraw") {
		return "", "", false
	}
	p = skipSpace(s.src, p+len("endraw"))
	end, ok := s.matchTagEnd(p, cfg.BlockEnd, true, cfg.TrimBlocks)
	if !ok {
		return "", "", false
	}
	return s.src[k : p+len(end)], sign, true
}

// ---- 行注释状态 ----

func (s *scanner) stepLinecomment() (bool, error) {
	// (.*?)()(?=\n|$)
	idx := strings.IndexByte(s.src[s.pos:], '\n')
	var data string
	if idx < 0 {
		data = s.src[s.pos:]
	} else {
		data = s.src[s.pos : s.pos+idx]
	}
	if data != "" {
		s.emit(s.lineno, TokenLinecomment, data)
	}
	s.emit(s.lineno, TokenLinecommentEnd, "")
	// Python 中 m.group() 不含换行 (lookahead 不消费), line_starting 必为 false
	s.lineStarting = false
	s.pos += len(data)
	s.stack = s.stack[:len(s.stack)-1]
	return true, nil
}

// ---- 块 / 变量 / 行语句状态 (表达式 token) ----

func (s *scanner) stepTag(state string) (bool, error) {
	cfg := &s.l.cfg
	src := s.src

	// 规则 1: 结束 tag. 括号未配平时跳过 (落入 operator 规则).
	if len(s.balancing) == 0 {
		switch state {
		case "block_begin":
			if end, ok := s.matchTagEnd(s.pos, cfg.BlockEnd, true, cfg.TrimBlocks); ok {
				s.emit(s.lineno, TokenBlockEnd, end)
				s.bump(end)
				s.pos += len(end)
				s.stack = s.stack[:len(s.stack)-1]
				return true, nil
			}
		case "variable_begin":
			if end, ok := s.matchTagEnd(s.pos, cfg.VariableEnd, false, false); ok {
				s.emit(s.lineno, TokenVariableEnd, end)
				s.bump(end)
				s.pos += len(end)
				s.stack = s.stack[:len(s.stack)-1]
				return true, nil
			}
		case "linestatement_begin":
			if end, ok := s.matchLinestatementEnd(); ok {
				s.emit(s.lineno, TokenLinestatementEnd, end)
				s.bump(end)
				s.pos += len(end)
				s.stack = s.stack[:len(s.stack)-1]
				return true, nil
			}
		}
	}

	if s.pos >= len(src) {
		return false, nil
	}

	// 规则顺序与 tag_rules 一致: whitespace, float, integer, name, string, operator
	if p := skipSpace(src, s.pos); p > s.pos {
		ws := src[s.pos:p]
		s.emit(s.lineno, TokenWhitespace, ws)
		s.bump(ws)
		s.pos = p
		return true, nil
	}
	if text, ok := matchFloat(src, s.pos); ok {
		s.emit(s.lineno, TokenFloat, text)
		s.bump(text)
		s.pos += len(text)
		return true, nil
	}
	if text, ok := matchInteger(src, s.pos); ok {
		s.emit(s.lineno, TokenInteger, text)
		s.bump(text)
		s.pos += len(text)
		return true, nil
	}
	if text, ok := matchName(src, s.pos); ok {
		s.emit(s.lineno, TokenName, text)
		s.bump(text)
		s.pos += len(text)
		return true, nil
	}
	if text, ok := matchString(src, s.pos); ok {
		s.emit(s.lineno, TokenString, text)
		s.bump(text)
		s.pos += len(text)
		return true, nil
	}
	if op, ok := matchOperator(src, s.pos); ok {
		if err := s.updateBalancing(op); err != nil {
			return false, err
		}
		s.emit(s.lineno, TokenOperator, op)
		s.bump(op)
		s.pos += len(op)
		return true, nil
	}
	return false, nil
}

// matchLinestatementEnd 复刻 \s*(\n|$) (MULTILINE) 的贪婪回溯结果:
// 空白连续段到达模板末尾时整段命中; 否则命中到段内最后一个换行 (含).
func (s *scanner) matchLinestatementEnd() (string, bool) {
	p := skipSpace(s.src, s.pos)
	run := s.src[s.pos:p]
	if p >= len(s.src) {
		return run, true // $ 在串尾
	}
	if i := strings.LastIndexByte(run, '\n'); i >= 0 {
		return run[:i+1], true
	}
	return "", false
}

func (s *scanner) updateBalancing(op string) error {
	switch op {
	case "{":
		s.balancing = append(s.balancing, '}')
	case "(":
		s.balancing = append(s.balancing, ')')
	case "[":
		s.balancing = append(s.balancing, ']')
	case "}", ")", "]":
		if len(s.balancing) == 0 {
			return s.syntaxErr(fmt.Sprintf("unexpected '%s'", op))
		}
		expected := s.balancing[len(s.balancing)-1]
		s.balancing = s.balancing[:len(s.balancing)-1]
		if string(expected) != op {
			return s.syntaxErr(fmt.Sprintf("unexpected '%s', expected '%c'", op, expected))
		}
	}
	return nil
}

func matchOperator(src string, pos int) (string, bool) {
	for _, op := range sortedOperators {
		if strings.HasPrefix(src[pos:], op) {
			return op, true
		}
	}
	return "", false
}

func signLen(src string, p int) int {
	if p < len(src) && (src[p] == '-' || src[p] == '+') {
		return 1
	}
	return 0
}

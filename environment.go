// Package gojinja2 是 100% 兼容 Jinja2 协议的 Go 模板引擎.
//
// 对应 jinja2/environment.py 的核心 API.
package gojinja2

import (
	"sync"

	"github.com/yzfly/gojinja2/exceptions"
	"github.com/yzfly/gojinja2/lexer"
	"github.com/yzfly/gojinja2/nodes"
	"github.com/yzfly/gojinja2/parser"
	"github.com/yzfly/gojinja2/runtime"
)

// AutoescapeFunc 按模板名决定是否自动转义.
type AutoescapeFunc func(name string) bool

// Environment 对应 jinja2.Environment.
type Environment struct {
	// 词法配置 (对应 block_start_string 等)
	BlockStart          string
	BlockEnd            string
	VariableStart       string
	VariableEnd         string
	CommentStart        string
	CommentEnd          string
	LineStatementPrefix string
	LineCommentPrefix   string
	TrimBlocks          bool
	LstripBlocks        bool
	NewlineSequence     string
	KeepTrailingNewline bool

	// Autoescape: bool 或 AutoescapeFunc
	Autoescape any

	// Undefined 行为
	Undefined runtime.UndefinedKind

	// Finalize 在输出转换前应用于每个表达式值
	Finalize func(any) any

	Filters map[string]*runtime.Func
	Tests   map[string]*runtime.Func
	Globals map[string]any

	// Policies 对应 jinja2 的 policies 字典
	Policies map[string]any

	// Loader 加载命名模板 (M3/M5)
	Loader Loader

	// 模板缓存
	cacheMu sync.Mutex
	cache   map[string]*Template

	lexerOnce sync.Once
	lx        *lexer.Lexer

	// extensions 已启用的扩展 (do / loopcontrols / i18n)
	extensions map[string]bool
}

// NewEnvironment 返回带默认配置的环境.
func NewEnvironment() *Environment {
	env := &Environment{
		BlockStart: "{%", BlockEnd: "%}",
		VariableStart: "{{", VariableEnd: "}}",
		CommentStart: "{#", CommentEnd: "#}",
		NewlineSequence: "\n",
		Autoescape:      false,
		Filters:         map[string]*runtime.Func{},
		Tests:           map[string]*runtime.Func{},
		Globals:         map[string]any{},
		Policies: map[string]any{
			"urlize.rel":            "noopener",
			"urlize.target":         nil,
			"urlize.extra_schemes":  nil,
			"truncate.leeway":       int64(5),
			"json.dumps_kwargs":     map[string]any{"sort_keys": true},
			"ext.i18n.trimmed":      false,
			"compiler.ascii_str":    true,
		},
		cache: map[string]*Template{},
	}
	registerDefaultFilters(env.Filters)
	registerDefaultTests(env.Tests)
	registerDefaultGlobals(env.Globals)
	return env
}

func (e *Environment) lexerConfig() lexer.Config {
	return lexer.Config{
		BlockStart: e.BlockStart, BlockEnd: e.BlockEnd,
		VariableStart: e.VariableStart, VariableEnd: e.VariableEnd,
		CommentStart: e.CommentStart, CommentEnd: e.CommentEnd,
		LineStatementPrefix: e.LineStatementPrefix,
		LineCommentPrefix:   e.LineCommentPrefix,
		TrimBlocks:          e.TrimBlocks, LstripBlocks: e.LstripBlocks,
		NewlineSequence:     e.NewlineSequence,
		KeepTrailingNewline: e.KeepTrailingNewline,
	}
}

func (e *Environment) lexer() *lexer.Lexer {
	e.lexerOnce.Do(func() { e.lx = lexer.New(e.lexerConfig()) })
	return e.lx
}

// undefinedFactory 构造配置类型的 Undefined.
func (e *Environment) undefinedFactory() runtime.UndefinedFactory {
	return runtime.NewUndefinedFactory(e.Undefined)
}

// autoescapeFor 计算模板的初始 autoescape.
func (e *Environment) autoescapeFor(name string) bool {
	switch ae := e.Autoescape.(type) {
	case bool:
		return ae
	case AutoescapeFunc:
		return ae(name)
	case func(string) bool:
		return ae(name)
	}
	return false
}

// Parse 解析模板源码为 AST.
func (e *Environment) Parse(source, name, filename string) (*nodes.Template, error) {
	stream, err := e.lexer().Tokenize(source, name, filename, "")
	if err != nil {
		return nil, err
	}
	return parser.New(stream, name, filename, e.parserExtensions()).Parse()
}

// FromString 对应 Environment.from_string.
func (e *Environment) FromString(source string) (*Template, error) {
	return e.fromSource(source, "<template>", "")
}

func (e *Environment) fromSource(source, name, filename string) (*Template, error) {
	tpl, err := e.Parse(source, name, filename)
	if err != nil {
		return nil, err
	}
	return newTemplate(e, tpl, name, filename), nil
}

// GetTemplate 通过 Loader 加载命名模板.
func (e *Environment) GetTemplate(name string) (*Template, error) {
	e.cacheMu.Lock()
	if t, ok := e.cache[name]; ok {
		e.cacheMu.Unlock()
		return t, nil
	}
	e.cacheMu.Unlock()

	if e.Loader == nil {
		// Python 抛 TypeError, 不是 TemplateNotFound:
		// ignore missing 不应吞掉此错误
		return nil, &exceptions.TemplateRuntimeError{
			Message: "no loader for this environment specified"}
	}
	source, filename, err := e.Loader.GetSource(e, name)
	if err != nil {
		return nil, err
	}
	t, err := e.fromSource(source, name, filename)
	if err != nil {
		return nil, err
	}
	e.cacheMu.Lock()
	e.cache[name] = t
	e.cacheMu.Unlock()
	return t, nil
}

// SelectTemplate 依次尝试一组模板名.
func (e *Environment) SelectTemplate(names []any) (*Template, error) {
	if len(names) == 0 {
		return nil, &exceptions.TemplatesNotFound{
			Message: "Tried to select from an empty list of templates."}
	}
	var tried []string
	for _, n := range names {
		if t, ok := n.(*Template); ok {
			return t, nil
		}
		name := runtime.Str(n)
		t, err := e.GetTemplate(name)
		if err == nil {
			return t, nil
		}
		if !isNotFound(err) {
			return nil, err
		}
		tried = append(tried, name)
	}
	return nil, &exceptions.TemplatesNotFound{Names: tried}
}

func isNotFound(err error) bool {
	switch err.(type) {
	case *exceptions.TemplateNotFound, *exceptions.TemplatesNotFound:
		return true
	}
	return false
}

// Loader 对应 jinja2 loaders 的最小接口.
type Loader interface {
	GetSource(env *Environment, name string) (source, filename string, err error)
}

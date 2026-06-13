// Package exceptions 对应 jinja2.exceptions, 定义模板引擎的错误类型.
package exceptions

import (
	"fmt"
	"strings"
)

// TemplateError 是所有模板错误的公共接口.
type TemplateError interface {
	error
	templateError()
}

// TemplateSyntaxError 对应 jinja2.exceptions.TemplateSyntaxError,
// 表示模板语法错误, 携带行号与模板名信息.
type TemplateSyntaxError struct {
	Message  string
	Lineno   int
	Name     string // 模板逻辑名
	Filename string // 模板文件路径
	// Source 在模板加载后由上层补充, 用于错误展示
	Source string
	// IsAssertion 对应 Python 子类 TemplateAssertionError
	IsAssertion bool
}

func NewSyntaxError(message string, lineno int, name, filename string) *TemplateSyntaxError {
	return &TemplateSyntaxError{Message: message, Lineno: lineno, Name: name, Filename: filename}
}

func (e *TemplateSyntaxError) templateError() {}

func (e *TemplateSyntaxError) Error() string {
	location := fmt.Sprintf("line %d", e.Lineno)
	name := e.Filename
	if name == "" {
		name = e.Name
	}
	if name != "" {
		location = fmt.Sprintf("File %q, %s", name, location)
	}
	return fmt.Sprintf("%s\n  %s", e.Message, location)
}

// TemplateNotFound 对应 jinja2.exceptions.TemplateNotFound.
type TemplateNotFound struct {
	TemplateName string
	Message      string
}

func (e *TemplateNotFound) templateError() {}

func (e *TemplateNotFound) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.TemplateName
}

// TemplatesNotFound 对应 jinja2.exceptions.TemplatesNotFound,
// 由 ChoiceLoader 或 include 候选列表全部未命中时抛出.
type TemplatesNotFound struct {
	Names   []string
	Message string
}

func (e *TemplatesNotFound) templateError() {}

func (e *TemplatesNotFound) Error() string {
	if e.Message != "" {
		return e.Message
	}
	parts := make([]string, len(e.Names))
	copy(parts, e.Names)
	return "none of the templates given were found: " + strings.Join(parts, ", ")
}

// TemplateRuntimeError 对应 jinja2.exceptions.TemplateRuntimeError.
type TemplateRuntimeError struct {
	Message string
}

func (e *TemplateRuntimeError) templateError() {}

func (e *TemplateRuntimeError) Error() string { return e.Message }

// UndefinedError 对应 jinja2.exceptions.UndefinedError,
// 对 Undefined 值做不允许的操作时抛出.
type UndefinedError struct {
	Message string
}

func (e *UndefinedError) templateError() {}

func (e *UndefinedError) Error() string { return e.Message }

// SecurityError 对应 jinja2.exceptions.SecurityError.
type SecurityError struct {
	Message string
}

func (e *SecurityError) templateError() {}

func (e *SecurityError) Error() string { return e.Message }

// FilterArgumentError 对应 jinja2.exceptions.FilterArgumentError.
type FilterArgumentError struct {
	Message string
}

func (e *FilterArgumentError) templateError() {}

func (e *FilterArgumentError) Error() string { return e.Message }

package gojinja2

import (
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/yzfly/gojinja2/exceptions"
)

func renderWith(t *testing.T, env *Environment, name string, vars map[string]any) string {
	t.Helper()
	tpl, err := env.GetTemplate(name)
	if err != nil {
		t.Fatalf("GetTemplate(%q): %v", name, err)
	}
	out, err := tpl.Render(vars)
	if err != nil {
		t.Fatalf("Render(%q): %v", name, err)
	}
	return out
}

func TestFileSystemLoader(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "base.html"), []byte("[{% block b %}base{% endblock %}]"), 0o644)
	os.WriteFile(filepath.Join(dir, "sub", "child.html"),
		[]byte("{% extends 'base.html' %}{% block b %}child{% endblock %}"), 0o644)

	env := NewEnvironment()
	env.Loader = NewFileSystemLoader(dir)
	if got := renderWith(t, env, "sub/child.html", nil); got != "[child]" {
		t.Errorf("继承渲染错误: %q", got)
	}

	// 越界路径必须拒绝
	if _, err := env.GetTemplate("../etc/passwd"); err == nil {
		t.Error("应拒绝越界路径")
	}
	// 缺失模板
	if _, err := env.GetTemplate("nope.html"); err == nil {
		t.Error("应报 TemplateNotFound")
	} else if _, ok := err.(*exceptions.TemplateNotFound); !ok {
		t.Errorf("错误类型应为 TemplateNotFound, 得到 %T", err)
	}

	names := env.Loader.(*FileSystemLoader).ListTemplates()
	if len(names) != 2 || names[0] != "base.html" || names[1] != "sub/child.html" {
		t.Errorf("ListTemplates: %v", names)
	}
}

func TestFSLoader(t *testing.T) {
	fsys := fstest.MapFS{
		"tpl/hello.txt": {Data: []byte("Hello {{ name }}!")},
	}
	env := NewEnvironment()
	env.Loader = NewFSLoader(fsys, "tpl")
	if got := renderWith(t, env, "hello.txt", map[string]any{"name": "Go"}); got != "Hello Go!" {
		t.Errorf("FSLoader 渲染错误: %q", got)
	}
}

func TestChoiceAndPrefixLoader(t *testing.T) {
	choice := NewChoiceLoader(
		NewDictLoader(map[string]string{"a": "A1"}),
		NewDictLoader(map[string]string{"a": "A2", "b": "B2"}),
	)
	env := NewEnvironment()
	env.Loader = choice
	if got := renderWith(t, env, "a", nil); got != "A1" {
		t.Errorf("ChoiceLoader 优先级错误: %q", got)
	}
	if got := renderWith(t, env, "b", nil); got != "B2" {
		t.Errorf("ChoiceLoader 回退错误: %q", got)
	}

	prefix := NewPrefixLoader(map[string]Loader{
		"one": NewDictLoader(map[string]string{"x": "1:{{ v }}"}),
		"two": NewDictLoader(map[string]string{"x": "2:{{ v }}"}),
	})
	env2 := NewEnvironment()
	env2.Loader = prefix
	if got := renderWith(t, env2, "two/x", map[string]any{"v": "ok"}); got != "2:ok" {
		t.Errorf("PrefixLoader 路由错误: %q", got)
	}
	if _, err := env2.GetTemplate("three/x"); err == nil {
		t.Error("未知前缀应报错")
	}
}

func TestFunctionLoader(t *testing.T) {
	env := NewEnvironment()
	env.Loader = NewFunctionLoader(func(name string) (string, string, bool) {
		if name == "dyn" {
			return "dynamic {{ x }}", "", true
		}
		return "", "", false
	})
	if got := renderWith(t, env, "dyn", map[string]any{"x": 1}); got != "dynamic 1" {
		t.Errorf("FunctionLoader: %q", got)
	}
	if _, err := env.GetTemplate("missing"); err == nil {
		t.Error("应报 TemplateNotFound")
	}
}

func TestTemplateCache(t *testing.T) {
	loader := NewDictLoader(map[string]string{"t": "v1"})
	env := NewEnvironment()
	env.Loader = loader
	t1, _ := env.GetTemplate("t")
	loader.Templates["t"] = "v2"
	t2, _ := env.GetTemplate("t")
	if t1 != t2 {
		t.Error("模板缓存未生效")
	}
}

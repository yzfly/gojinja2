package gojinja2

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/yzfly/gojinja2/exceptions"
)

// 本文件对应 jinja2/loaders.py.
// DictLoader 在 builtins.go 中定义.

// splitTemplatePath 校验并切分模板路径, 对应 split_template_path.
// 拒绝 ".." 等越界路径.
func splitTemplatePath(template string) ([]string, error) {
	var pieces []string
	for _, piece := range strings.Split(template, "/") {
		if strings.ContainsAny(piece, `/\`) || piece == ".." ||
			(piece != "" && piece[0] == '.' && piece != ".") {
			return nil, &exceptions.TemplateNotFound{TemplateName: template}
		}
		if piece != "" && piece != "." {
			pieces = append(pieces, piece)
		}
	}
	return pieces, nil
}

// FileSystemLoader 对应 jinja2.FileSystemLoader: 从文件系统目录加载.
type FileSystemLoader struct {
	SearchPaths []string
	// FollowLinks 仅为兼容; Go 的 os.ReadFile 总是跟随符号链接
	FollowLinks bool
}

func NewFileSystemLoader(searchPath ...string) *FileSystemLoader {
	return &FileSystemLoader{SearchPaths: searchPath}
}

func (l *FileSystemLoader) GetSource(env *Environment, name string) (string, string, error) {
	pieces, err := splitTemplatePath(name)
	if err != nil {
		return "", "", err
	}
	for _, root := range l.SearchPaths {
		filename := filepath.Join(root, filepath.Join(pieces...))
		data, err := os.ReadFile(filename)
		if err != nil {
			continue
		}
		return string(data), filename, nil
	}
	return "", "", &exceptions.TemplateNotFound{TemplateName: name}
}

// ListTemplates 枚举全部模板名 (排序).
func (l *FileSystemLoader) ListTemplates() []string {
	seen := map[string]bool{}
	var out []string
	for _, root := range l.SearchPaths {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return nil
			}
			rel, err := filepath.Rel(root, path)
			if err != nil {
				return nil
			}
			name := filepath.ToSlash(rel)
			if !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
			return nil
		})
	}
	sortStrings(out)
	return out
}

// FSLoader 从任意 io/fs.FS 加载 (embed.FS 等), 对应 PackageLoader 的 Go 等价物.
type FSLoader struct {
	FS   fs.FS
	Root string // 可选子目录前缀
}

func NewFSLoader(fsys fs.FS, root string) *FSLoader {
	return &FSLoader{FS: fsys, Root: root}
}

func (l *FSLoader) GetSource(env *Environment, name string) (string, string, error) {
	pieces, err := splitTemplatePath(name)
	if err != nil {
		return "", "", err
	}
	p := strings.Join(pieces, "/")
	if l.Root != "" {
		p = l.Root + "/" + p
	}
	data, err := fs.ReadFile(l.FS, p)
	if err != nil {
		return "", "", &exceptions.TemplateNotFound{TemplateName: name}
	}
	return string(data), p, nil
}

// FunctionLoader 对应 jinja2.FunctionLoader.
// fn 返回 (source, filename, found).
type FunctionLoader struct {
	Fn func(name string) (string, string, bool)
}

func NewFunctionLoader(fn func(name string) (string, string, bool)) *FunctionLoader {
	return &FunctionLoader{Fn: fn}
}

func (l *FunctionLoader) GetSource(env *Environment, name string) (string, string, error) {
	source, filename, ok := l.Fn(name)
	if !ok {
		return "", "", &exceptions.TemplateNotFound{TemplateName: name}
	}
	return source, filename, nil
}

// ChoiceLoader 对应 jinja2.ChoiceLoader: 依次尝试多个 loader.
type ChoiceLoader struct {
	Loaders []Loader
}

func NewChoiceLoader(loaders ...Loader) *ChoiceLoader {
	return &ChoiceLoader{Loaders: loaders}
}

func (l *ChoiceLoader) GetSource(env *Environment, name string) (string, string, error) {
	for _, sub := range l.Loaders {
		source, filename, err := sub.GetSource(env, name)
		if err == nil {
			return source, filename, nil
		}
		if !isNotFound(err) {
			return "", "", err
		}
	}
	return "", "", &exceptions.TemplateNotFound{TemplateName: name}
}

// PrefixLoader 对应 jinja2.PrefixLoader: 按前缀路由到子 loader.
type PrefixLoader struct {
	Mapping   map[string]Loader
	Delimiter string // 默认 "/"
}

func NewPrefixLoader(mapping map[string]Loader) *PrefixLoader {
	return &PrefixLoader{Mapping: mapping, Delimiter: "/"}
}

func (l *PrefixLoader) GetSource(env *Environment, name string) (string, string, error) {
	delim := l.Delimiter
	if delim == "" {
		delim = "/"
	}
	idx := strings.Index(name, delim)
	if idx < 0 {
		return "", "", &exceptions.TemplateNotFound{TemplateName: name}
	}
	prefix, rest := name[:idx], name[idx+len(delim):]
	sub, ok := l.Mapping[prefix]
	if !ok {
		return "", "", &exceptions.TemplateNotFound{TemplateName: name}
	}
	source, filename, err := sub.GetSource(env, rest)
	if err != nil {
		// 把 TemplateNotFound 的名字替换为完整名 (与 Python 一致)
		if isNotFound(err) {
			return "", "", &exceptions.TemplateNotFound{TemplateName: name}
		}
		return "", "", err
	}
	return source, filename, nil
}

func sortStrings(s []string) {
	for i := 0; i < len(s); i++ {
		for j := i + 1; j < len(s); j++ {
			if s[j] < s[i] {
				s[i], s[j] = s[j], s[i]
			}
		}
	}
}

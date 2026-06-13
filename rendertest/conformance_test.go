// Package rendertest 运行 render 级 conformance 测试:
// 与官方 Jinja2 在相同 (模板, 上下文, 配置) 下逐字符对齐输出.
package rendertest

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	gojinja2 "github.com/yzfly/gojinja2"
	"github.com/yzfly/gojinja2/exceptions"
	"github.com/yzfly/gojinja2/runtime"
)

type fixture struct {
	Name      string            `json:"name"`
	Source    string            `json:"source"`
	CtxRaw    json.RawMessage   `json:"ctx"`
	Templates map[string]string `json:"templates"`
	Env       map[string]any    `json:"env"`
	Output    *string           `json:"output"`
	ErrorType string            `json:"error_type"`
	Error     string            `json:"error"`
	// KnownDivergence 标记文档化差异 (如 int64 vs 任意精度), 跳过但计数
	KnownDivergence string `json:"known_divergence"`
}

// decodeOrdered 用 token 流解码 JSON, 对象转为保序 *runtime.Dict,
// 数字按整数性转为 int64 / float64.
func decodeOrdered(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeValue(dec, tok)
}

func decodeValue(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			d := runtime.NewDict()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				val, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				d.Set(keyTok.(string), val)
			}
			if _, err := dec.Token(); err != nil { // '}'
				return nil, err
			}
			return d, nil
		case '[':
			items := []any{}
			for dec.More() {
				v, err := decodeOrdered(dec)
				if err != nil {
					return nil, err
				}
				items = append(items, v)
			}
			if _, err := dec.Token(); err != nil { // ']'
				return nil, err
			}
			return items, nil
		}
	case json.Number:
		s := t.String()
		if !strings.ContainsAny(s, ".eE") {
			n, err := t.Int64()
			if err == nil {
				return n, nil
			}
		}
		f, err := t.Float64()
		return f, err
	case string:
		return t, nil
	case bool:
		return t, nil
	case nil:
		return nil, nil
	}
	return tok, nil
}

func ctxFrom(raw json.RawMessage) map[string]any {
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	v, err := decodeOrdered(dec)
	if err != nil {
		panic(err)
	}
	d := v.(*runtime.Dict)
	out := map[string]any{}
	for _, it := range d.Items() {
		out[it.Key.(string)] = it.Value
	}
	return out
}

func envFrom(spec map[string]any, templates map[string]string) *gojinja2.Environment {
	env := gojinja2.NewEnvironment()
	if v, ok := spec["autoescape"]; ok {
		env.Autoescape = v.(bool)
	}
	if v, ok := spec["trim_blocks"]; ok {
		env.TrimBlocks = v.(bool)
	}
	if v, ok := spec["lstrip_blocks"]; ok {
		env.LstripBlocks = v.(bool)
	}
	if v, ok := spec["keep_trailing_newline"]; ok {
		env.KeepTrailingNewline = v.(bool)
	}
	if v, ok := spec["newline_sequence"]; ok {
		env.NewlineSequence = v.(string)
	}
	if v, ok := spec["undefined"]; ok {
		switch v.(string) {
		case "chainable":
			env.Undefined = runtime.UndefinedChainable
		case "debug":
			env.Undefined = runtime.UndefinedDebug
		case "strict":
			env.Undefined = runtime.UndefinedStrict
		}
	}
	if v, ok := spec["extensions"]; ok {
		for _, e := range v.([]any) {
			env.AddExtension(e.(string))
		}
	}
	if v, ok := spec["install_null_translations"]; ok && v.(bool) {
		env.InstallNullTranslations()
	}
	if len(templates) > 0 {
		env.Loader = gojinja2.NewDictLoader(templates)
	}
	return env
}

// errMessage 提取与 Python str(e) 对应的错误信息.
func errMessage(err error) string {
	switch e := err.(type) {
	case *exceptions.TemplateSyntaxError:
		return e.Message
	case *exceptions.TemplateNotFound:
		return e.Error()
	case *exceptions.TemplatesNotFound:
		return e.Error()
	}
	return err.Error()
}

func TestRenderConformance(t *testing.T) {
	data, err := os.ReadFile("testdata/render_fixtures.json")
	if err != nil {
		t.Fatalf("读取语料失败 (先运行 tools/gen_render_fixtures.py): %v", err)
	}
	var fixtures []fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	pass := 0
	diverged := 0
	for _, fx := range fixtures {
		if fx.KnownDivergence != "" {
			diverged++
			t.Run(fx.Name, func(t *testing.T) {
				t.Skip("已知文档化差异: " + fx.KnownDivergence)
			})
			continue
		}
		ok := t.Run(fx.Name, func(t *testing.T) {
			env := envFrom(fx.Env, fx.Templates)
			tpl, err := env.FromString(fx.Source)
			var got string
			if err == nil {
				got, err = tpl.Render(ctxFrom(fx.CtxRaw))
			}

			if fx.Output != nil {
				if err != nil {
					t.Fatalf("意外错误: %v", err)
				}
				if got != *fx.Output {
					t.Errorf("输出不一致:\n  python: %q\n  go:     %q", *fx.Output, got)
				}
				return
			}
			// 错误用例: 错误信息逐字对齐
			if err == nil {
				t.Fatalf("期望错误 %s(%q), 实际输出 %q", fx.ErrorType, fx.Error, got)
			}
			if msg := errMessage(err); msg != fx.Error {
				t.Errorf("错误信息不一致:\n  python: %q\n  go:     %q", fx.Error, msg)
			}
		})
		if ok {
			pass++
		}
	}
	t.Logf("render conformance: %d/%d (另有 %d 个已知文档化差异)",
		pass, len(fixtures)-diverged, diverged)
}

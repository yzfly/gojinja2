package parser

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/yzfly/gojinja2/exceptions"
	"github.com/yzfly/gojinja2/lexer"
	"github.com/yzfly/gojinja2/nodes"
)

type fixture struct {
	Name        string         `json:"name"`
	Source      string         `json:"source"`
	Config      map[string]any `json:"config"`
	Repr        *string        `json:"repr"`
	Error       *string        `json:"error"`
	ErrorLineno int            `json:"error_lineno"`
}

func configFrom(m map[string]any) lexer.Config {
	cfg := lexer.DefaultConfig()
	if v, ok := m["line_statement_prefix"]; ok {
		cfg.LineStatementPrefix = v.(string)
	}
	if v, ok := m["trim_blocks"]; ok {
		cfg.TrimBlocks = v.(bool)
	}
	if v, ok := m["lstrip_blocks"]; ok {
		cfg.LstripBlocks = v.(bool)
	}
	return cfg
}

func TestParserConformance(t *testing.T) {
	data, err := os.ReadFile("testdata/parser_fixtures.json")
	if err != nil {
		t.Fatalf("读取语料失败 (先运行 tools/gen_parser_fixtures.py): %v", err)
	}
	var fixtures []fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			lx := lexer.New(configFrom(fx.Config))
			var tpl *nodes.Template
			stream, err := lx.Tokenize(fx.Source, "<template>", "", "")
			if err == nil {
				tpl, err = New(stream, "<template>", "", nil).Parse()
			}

			if fx.Error != nil {
				if err == nil {
					t.Fatalf("期望错误 %q, 实际解析成功:\n  %s", *fx.Error, nodes.PyRepr(tpl))
				}
				se, ok := err.(*exceptions.TemplateSyntaxError)
				if !ok {
					t.Fatalf("期望 TemplateSyntaxError, 得到 %T: %v", err, err)
				}
				if se.Message != *fx.Error {
					t.Errorf("错误信息不一致:\n  python: %q\n  go:     %q", *fx.Error, se.Message)
				}
				if se.Lineno != fx.ErrorLineno {
					t.Errorf("错误行号不一致: python=%d go=%d", fx.ErrorLineno, se.Lineno)
				}
				return
			}
			if err != nil {
				t.Fatalf("意外错误: %v", err)
			}
			got := nodes.PyRepr(tpl)
			if got != *fx.Repr {
				t.Errorf("AST 不一致:\n  python: %s\n  go:     %s", *fx.Repr, got)
			}
		})
	}
}

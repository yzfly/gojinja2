package lexer

import (
	"encoding/json"
	"math"
	"os"
	"strconv"
	"testing"

	"github.com/yzfly/gojinja2/exceptions"
)

// fixture 结构与 tools/gen_lexer_fixtures.py 的输出对应.
type fixtureToken struct {
	Lineno int     `json:"lineno"`
	Type   string  `json:"type"`
	Int    *int64  `json:"int"`
	Float  *string `json:"float"` // Python repr
	Value  *string `json:"value"`
}

type fixture struct {
	Name        string         `json:"name"`
	Source      string         `json:"source"`
	Config      map[string]any `json:"config"`
	Tokens      []fixtureToken `json:"tokens"`
	Error       *string        `json:"error"`
	ErrorLineno int            `json:"error_lineno"`
}

func configFrom(m map[string]any) Config {
	cfg := DefaultConfig()
	str := func(key string, dst *string) {
		if v, ok := m[key]; ok {
			*dst = v.(string)
		}
	}
	boolean := func(key string, dst *bool) {
		if v, ok := m[key]; ok {
			*dst = v.(bool)
		}
	}
	str("block_start_string", &cfg.BlockStart)
	str("block_end_string", &cfg.BlockEnd)
	str("variable_start_string", &cfg.VariableStart)
	str("variable_end_string", &cfg.VariableEnd)
	str("comment_start_string", &cfg.CommentStart)
	str("comment_end_string", &cfg.CommentEnd)
	str("line_statement_prefix", &cfg.LineStatementPrefix)
	str("line_comment_prefix", &cfg.LineCommentPrefix)
	str("newline_sequence", &cfg.NewlineSequence)
	boolean("trim_blocks", &cfg.TrimBlocks)
	boolean("lstrip_blocks", &cfg.LstripBlocks)
	boolean("keep_trailing_newline", &cfg.KeepTrailingNewline)
	return cfg
}

// pyFloatRepr 把 float64 格式化为与 Python repr 一致的形式.
func pyFloatRepr(f float64) string {
	if f == math.Trunc(f) && math.Abs(f) < 1e16 {
		return strconv.FormatFloat(f, 'f', 1, 64)
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func TestLexerConformance(t *testing.T) {
	data, err := os.ReadFile("testdata/lexer_fixtures.json")
	if err != nil {
		t.Fatalf("读取语料失败 (先运行 tools/gen_lexer_fixtures.py): %v", err)
	}
	var fixtures []fixture
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fx := range fixtures {
		t.Run(fx.Name, func(t *testing.T) {
			lx := New(configFrom(fx.Config))
			stream, err := lx.Tokenize(fx.Source, "<template>", "", "")

			// 延迟错误语义: 读完整个流才能确认是否有词法错误
			var got []Token
			if err == nil {
				err = func() (drainErr error) {
					defer func() {
						if r := recover(); r != nil {
							drainErr = r.(error)
						}
					}()
					for stream.Current.Type != TokenEOF {
						got = append(got, stream.Next())
					}
					return nil
				}()
			}

			if fx.Error != nil {
				if err == nil {
					t.Fatalf("期望错误 %q, 实际成功", *fx.Error)
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

			if len(got) != len(fx.Tokens) {
				t.Errorf("token 数不一致: python=%d go=%d\n  python: %v\n  go:     %v",
					len(fx.Tokens), len(got), fx.Tokens, got)
				return
			}
			for i, want := range fx.Tokens {
				g := got[i]
				if g.Lineno != want.Lineno {
					t.Errorf("token[%d] 行号: python=%d go=%d", i, want.Lineno, g.Lineno)
				}
				if string(g.Type) != want.Type {
					t.Errorf("token[%d] 类型: python=%q go=%q", i, want.Type, g.Type)
				}
				switch {
				case want.Int != nil:
					if g.IntVal != *want.Int {
						t.Errorf("token[%d] 整数值: python=%d go=%d", i, *want.Int, g.IntVal)
					}
				case want.Float != nil:
					if pyFloatRepr(g.FloatVal) != *want.Float {
						t.Errorf("token[%d] 浮点值: python=%s go=%s", i, *want.Float, pyFloatRepr(g.FloatVal))
					}
				case want.Value != nil:
					if g.Value != *want.Value {
						t.Errorf("token[%d] 值: python=%q go=%q", i, *want.Value, g.Value)
					}
				}
			}
		})
	}
}

// TestTokenStream 移植 test_lexnparse.py 的 TestTokenStream.
func TestTokenStream(t *testing.T) {
	testTokens := []Token{
		{Lineno: 1, Type: TokenBlockBegin},
		{Lineno: 2, Type: TokenBlockEnd},
	}
	ts := NewTokenStream(testTokens, "foo", "bar")
	if ts.Current.Type != TokenBlockBegin || !ts.Bool() || ts.EOS() {
		t.Fatal("初始状态错误")
	}
	ts.Next()
	if ts.Current.Type != TokenBlockEnd || !ts.Bool() || ts.EOS() {
		t.Fatal("第二个 token 状态错误")
	}
	ts.Next()
	if ts.Current.Type != TokenEOF || ts.Bool() || !ts.EOS() {
		t.Fatal("EOF 状态错误")
	}

	// look / push
	ts2 := NewTokenStream(testTokens, "", "")
	if look := ts2.Look(); look.Type != TokenBlockEnd {
		t.Fatalf("Look 应返回 block_end, 得到 %s", look.Type)
	}
	if ts2.Current.Type != TokenBlockBegin {
		t.Fatal("Look 不应消费当前 token")
	}
}

func TestPyUnescape(t *testing.T) {
	cases := map[string]string{
		`a\nb`:              "a\nb",
		`\x41\101`:          "AA",
		`♨`:            "♨",
		`\U0001F40D`:        "🐍",
		`\q`:                `\q`,
		`tab\tend`:          "tab\tend",
		`\N{HOT SPRINGS}`:   "♨",
		`\N{LATIN SMALL LETTER A}`: "a",
		`\0`:                "\x00",
		`\777`:              string(rune(511)),
	}
	for in, want := range cases {
		got, err := pyUnescape(in)
		if err != nil {
			t.Errorf("pyUnescape(%q) 错误: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("pyUnescape(%q) = %q, 期望 %q", in, got, want)
		}
	}
	if _, err := pyUnescape(`\x4`); err == nil {
		t.Error(`\x4 应报 truncated 错误`)
	}
}

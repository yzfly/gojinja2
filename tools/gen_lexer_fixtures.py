#!/usr/bin/env python3
"""用参考实现 (reference/jinja) 生成 lexer 级 conformance 语料.

每个用例: 模板源码 + 环境配置 -> wrap 后的 token 序列 (或语法错误信息).
输出 JSON 供 Go 测试逐 token 对齐.

用法: PYTHONPATH=reference/jinja/src python3 tools/gen_lexer_fixtures.py > lexer/testdata/lexer_fixtures.json
"""
import json
import sys

from jinja2 import Environment
from jinja2.exceptions import TemplateSyntaxError

# (名称, 配置dict, 模板源码)
CASES = []


def case(name, source, **env_kwargs):
    CASES.append((name, env_kwargs, source))


# ---- 基础 ----
case("simple_var", "{{ foo }}")
case("simple_block", "{% if x %}y{% endif %}")
case("simple_comment", "a{# note #}b")
case("text_only", "hello world\nsecond line")
case("empty", "")
case("var_with_expr", "{{ a + b * c ** d // e % f ~ g }}")
case("chained_compare", "{{ 1 < 2 <= 3 == 3 != 4 > 0 >= 0 }}")
case("dict_literal", "{{ {'a': 1, 'b': [1, 2], 'c': (1,)} }}")
case("pipe_filter", "{{ foo|default('x')|upper }}")
case("multiline_block", "{% for item\n   in seq %}x{% endfor %}")

# ---- 数字字面量 (来自 test_numeric_literal 及边角) ----
for lit in ["1", "123", "12_34_56", "1.2", "34.56", "3_4.5_6", "1e0", "10e1",
            "2.5e100", "2.5e+100", "25.6e-10", "1_2.3_4e5_6", "0", "00",
            "0b1001_111", "0o123", "0o1_23", "0x123abc", "0x12_3abc",
            "0xdEAdBeeF", "0B101", "0O17", "0X1f", "1.5E3", "1_000",
            "2.5e1_0"]:
    case(f"num_{lit}", "{{ %s }}" % lit)
# 不是单个数字 token 的写法
for lit in ["007", "1__2", "1_", "0x", "0b2", "1.e5", "12.", "foo.0", "x.5"]:
    case(f"numedge_{lit}", "{{ %s }}" % lit)

# ---- 字符串字面量与转义 ----
case("str_escapes", r"""{{ '\0' ~ '\t' ~ '\n' ~ '\\' ~ "\'" ~ '\"' }}""")
case("str_hex_oct", r"{{ '\x41\101♨\U0001F40D' }}")
case("str_named_escape", r'{{ "\N{HOT SPRINGS}" }}')
case("str_unknown_escape", r"{{ '\q\w' }}")
case("str_nonascii", "{{ 'bär' }}")
case("str_multiline", "{{ 'a\nb' }}")
case("str_with_tags", '{% set x = " {% str %} " %}{{ x }}')
case("str_truncated_x", r"{{ '\x4' }}")

# ---- 标识符 ----
case("name_unicode", "{{ föö }}")
case("name_ja", "{{ き }}")
case("name_underscore", "{{ _ }}{{ _foo }}")
case("name_digits", "{{ a1_b2 }}")

# ---- 运算符 ----
case("all_operators", "{{ + - / // * % ** ~ == != > >= < <= = . : | , ; }}")
case("brackets", "{{ ([{}]) }}")

# ---- 括号配平 ----
case("balancing_custom", "{% for item in seq\n            %}${{'foo': item}|upper}{% endfor %}",
     block_start_string="{%", block_end_string="%}",
     variable_start_string="${", variable_end_string="}")
case("balance_err_unexpected", "{{ (a)) }}")
case("balance_err_mismatch", "{{ (a] }}")
case("unexpected_char", "{{ a ? b }}")
case("invalid_str", "{{ 'abc }}")

# ---- raw ----
case("raw1", "{% raw %}foo{% endraw %}|{%raw%}{{ bar }}|{% baz %}{%       endraw    %}")
case("raw2", "1  {%- raw -%}   2   {%- endraw -%}   3")
case("raw3", "bar\n{% raw %}\n  {{baz}}2 spaces\n{% endraw %}\nfoo",
     lstrip_blocks=True, trim_blocks=True)
case("raw4", "bar\n{%- raw -%}\n\n  \n  2 spaces\n space{%- endraw -%}\nfoo",
     lstrip_blocks=True, trim_blocks=False)
case("raw_trim_lstrip", "{% raw %}\n  {{baz}}2 spaces\n{% endraw %}\nfoo",
     lstrip_blocks=True, trim_blocks=True)
case("raw_no_trim_lstrip", "{% raw %}\n  {{baz}}2 spaces\n{% endraw %}\nfoo",
     lstrip_blocks=True, trim_blocks=False)
case("raw_missing_end", "{% raw %}foo")
case("raw_plus_end", "x  {% raw +%}\ndata{% endraw +%}  y")

# ---- 注释 ----
case("comment_missing_end", "{# foo")
case("comment_signs", "a   {#- x -#}   b")
case("comment_plus", "a   {#+ x +#}   b")
case("comment_custom", "<ul>\n<!--- for item in seq -->\n  <li>{item}</li>\n<!--- endfor -->\n</ul>",
     block_start_string="<!--", block_end_string="-->",
     variable_start_string="{", variable_end_string="}")
case("comment_trim", "X{#comment#}\nY", trim_blocks=True)
case("comment_no_trim", "X{#comment#}\nY")

# ---- lstrip / trim 全家桶 ----
case("lstrip", "    {% if True %}\n    {% endif %}", lstrip_blocks=True)
case("lstrip_trim", "    {% if True %}\n    {% endif %}",
     lstrip_blocks=True, trim_blocks=True)
case("no_lstrip", "    {%+ if True %}\n    {%+ endif %}", lstrip_blocks=True)
case("no_lstrip_minus", "    {%- if True %}\n    {%- endif %}", lstrip_blocks=True)
case("lstrip_false_plus", "    {%+ if True %}\n    {%+ endif %}", lstrip_blocks=False)
case("lstrip_false_minus", "    {%- if True %}\n    {%- endif %}", lstrip_blocks=False)
case("lstrip_endline", "    hello{% if True %}\n    goodbye{% endif %}", lstrip_blocks=True)
case("lstrip_inline", "    {% if True %}hello    {% endif %}", lstrip_blocks=True)
case("lstrip_nested", "    {% if True %}a {% if True %}b {% endif %}c {% endif %}",
     lstrip_blocks=True)
case("lstrip_left_chars", "    hello\n    abc{% if True %}\n        world{% endif %}",
     lstrip_blocks=True)
case("lstrip_preserve_leading_newlines", "\n\n\n{% set hello = 1 %}", lstrip_blocks=True)
case("lstrip_comment", "    {# if True #}\nhello\n    {#endif#}", lstrip_blocks=True)
case("lstrip_angle_simple",
     "    <% if True %>hello    <% endif %>",
     block_start_string="<%", block_end_string="%>",
     variable_start_string="${", variable_end_string="}",
     comment_start_string="<%#", comment_end_string="%>",
     lstrip_blocks=True)
case("php_syntax_lstrip",
     "<!-- I'm a comment, I'm not interesting -->\n<? for item in seq -?>\n        <?= item ?>\n<?- endfor ?>",
     block_start_string="<?", block_end_string="?>",
     variable_start_string="<?=", variable_end_string="?>",
     comment_start_string="<!--", comment_end_string="-->",
     lstrip_blocks=True, trim_blocks=True)
case("erb_syntax",
     "<%# I'm a comment, I'm not interesting %>\n    <% for item in seq %>\n    <%= item %>\n    <% endfor %>",
     block_start_string="<%", block_end_string="%>",
     variable_start_string="<%=", variable_end_string="%>",
     comment_start_string="<%#", comment_end_string="%>",
     lstrip_blocks=True, trim_blocks=True)
case("trim", "    {% if True %}\n    {% endif %}", trim_blocks=True)
case("trim_nested", "    {% if True %}\na {% if True %}\nb {% endif %}\nc {% endif %}",
     trim_blocks=True, lstrip_blocks=True)
case("no_trim_nested", "    {% if True +%}\na {% if True +%}\nb {% endif +%}\nc {% endif %}",
     trim_blocks=True, lstrip_blocks=True)
case("trim_plus_var", "    {{ x }}\n    {{ y }}", lstrip_blocks=True, trim_blocks=True)
case("multiple_comment_trim_lstrip",
     "   {# comment #}\n\n{# comment2 #}\n   \n{# comment3 #}\n\n ",
     lstrip_blocks=True, trim_blocks=True)

# ---- 行语句 / 行注释 ----
case("line_syntax",
     "<%# regular comment %>\n% for item in seq:\n    ${item}\n% endfor",
     block_start_string="<%", block_end_string="%>",
     variable_start_string="${", variable_end_string="}",
     comment_start_string="<%#", comment_end_string="%>",
     line_statement_prefix="%")
case("line_syntax_priority1",
     "/* ignore me.\n   I'm a multiline comment */\n## for item in seq:\n* ${item}          # this is just extra stuff\n## endfor",
     block_start_string="{%", block_end_string="%}",
     variable_start_string="${", variable_end_string="}",
     comment_start_string="/*", comment_end_string="*/",
     line_statement_prefix="##", line_comment_prefix="#")
case("line_syntax_priority2",
     "/* ignore me.\n   I'm a multiline comment */\n# for item in seq:\n* ${item}          ## this is just extra stuff\n    ## extra stuff i just want to ignore\n# endfor",
     block_start_string="{%", block_end_string="%}",
     variable_start_string="${", variable_end_string="}",
     comment_start_string="/*", comment_end_string="*/",
     line_statement_prefix="#", line_comment_prefix="##")
case("line_stmt_signs", "# if x\nfoo\n# endif", line_statement_prefix="#")
case("line_stmt_eof_nonl", "# if x\nfoo\n# endif", line_statement_prefix="#")
case("line_comment_only", "foo ## comment\nbar", line_comment_prefix="##")

# ---- 换行处理 ----
case("trailing_newline_keep", "a\n", keep_trailing_newline=True)
case("trailing_newline_drop", "a\n")
case("trailing_crlf", "a\r\n")
case("mixed_newlines", "1\n2\r\n3\n4\n")
for seq in ["\r", "\r\n", "\n"]:
    case(f"newline_seq_{json.dumps(seq)}", "1\n2\r\n3\n4\n", newline_sequence=seq)
case("string_newline_normalize", "{{ 'a\r\nb' }}", newline_sequence="\r\n")

# ---- 行号 ----
case("lineno_with_strip",
     "foo\nbar\n{%- if baz %}\nbuz\n{% endif %}\nzop")
case("lineno_multiline_ws", "a\n\n\n{{ x\n+\ny }}\nb")


def main():
    out = []
    for name, kwargs, source in CASES:
        env = Environment(**kwargs)
        entry = {"name": name, "source": source, "config": kwargs}
        try:
            toks = []
            for t in env.lexer.tokenize(source):
                v = {"lineno": t.lineno, "type": t.type}
                if isinstance(t.value, bool):
                    raise AssertionError("unexpected bool")
                if isinstance(t.value, int):
                    v["int"] = t.value
                elif isinstance(t.value, float):
                    v["float"] = repr(t.value)
                else:
                    v["value"] = t.value
                toks.append(v)
            entry["tokens"] = toks
        except TemplateSyntaxError as e:
            entry["error"] = e.message
            entry["error_lineno"] = e.lineno
        out.append(entry)
    json.dump(out, sys.stdout, indent=1, ensure_ascii=False)


if __name__ == "__main__":
    main()

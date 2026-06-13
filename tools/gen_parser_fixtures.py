#!/usr/bin/env python3
"""用参考实现生成 parser 级 conformance 语料: 模板 -> repr(AST) 或错误信息.

用法: PYTHONPATH=reference/jinja/src python3 tools/gen_parser_fixtures.py > parser/testdata/parser_fixtures.json
"""
import json
import sys

from jinja2 import Environment
from jinja2.exceptions import TemplateSyntaxError

CASES = []


def case(name, source, **env_kwargs):
    CASES.append((name, env_kwargs, source))


# ---- 基础输出 ----
case("data_only", "hello")
case("var", "{{ foo }}")
case("var_data_mix", "a{{ b }}c{{ d }}e")
case("tuple_in_var", "{{ a, b }}")
case("empty_var_err", "{{ }}")

# ---- TestSyntax 全family ----
case("call", "{{ foo('a', c='d', e='f', *g, **h) }}")
case("call_dynargs_only", "{{ foo(*args) }}")
case("call_trailing_comma", "{{ foo(1, 2,) }}")
case("call_arg_after_kwarg_err", "{{ foo(a=1, 2) }}")
case("call_two_starargs_err", "{{ foo(*a, *b) }}")
case("slicing", "{{ [1, 2, 3][:] }}|{{ [1, 2, 3][::-1] }}")
case("slicing_more", "{{ x[1:2:3] }}{{ x[a:] }}{{ x[:b] }}{{ x[::2] }}{{ x[1:2, 3] }}")
case("attr", "{{ foo.bar }}|{{ foo['bar'] }}")
case("subscript", "{{ foo[0] }}|{{ foo[-1] }}")
case("attr_int", "{{ foo.0 }}")
case("attr_float_getattr", "{{ foo.0.0 }}")
case("tuple_expr", "{{ () }}|{{ (1, 2) }}|{{ (1,) }}|{{ 1, }}")
case("math", "{{ (1 + 1 * 2) - 3 / 2 }}|{{ 2**3 }}")
case("div", "{{ 3 // 2 }}|{{ 3 / 2 }}|{{ 3 % 2 }}")
case("unary", "{{ +3 }}|{{ -3 }}|{{ not 3 }}|{{ not not 3 }}|{{ --1 }}")
case("concat", "{{ [1, 2] ~ 'foo' }}")
case("compare_chained", "{{ 1 < 2 < 3 }}|{{ a == b != c in d not in e }}")
case("inop", "{{ 1 in [1, 2, 3] }}|{{ 1 not in [1, 2, 3] }}")
case("collection_literals", "{{ [] }}|{{ {} }}|{{ {'a': 'b'} }}|{{ [1, ['a', 'b'], {1: 2}] }}")
case("constant_casing", "{{ True }}|{{ true }}|{{ False }}|{{ false }}|{{ None }}|{{ none }}")
case("bool_ops", "{{ true and false }}|{{ false or true }}|{{ not false }}")
case("grouping", "{{ (true and false) or (false and true) and not false }}")
case("django_attr", "{{ [1, 2, 3].0 }}|{{ [[1]].0.0 }}")
case("conditional_expression", "{{ 0 if true else 1 }}")
case("short_conditional_expression", "{{ 1 if false }}")
case("nested_condexpr", "{{ a if b else c if d else e }}")
case("filter_priority", "{{ 'foo'|upper + 'bar'|upper }}")
case("filter_chain", "{{ x|a|b|c }}")
case("filter_args", "{{ x|join(', ', attribute='name') }}")
case("filter_dotted", "{{ x|to.string }}")
case("function_calls_complex", "{{ foo(x, y, z=1, *args, **kwargs) }}")
case("test_basic", "{{ x is defined }}")
case("test_not", "{{ x is not defined }}")
case("test_arg", "{{ x is divisibleby 3 }}")
case("test_arg_parens", "{{ x is divisibleby(3) }}")
case("test_arg_postfix", "{{ x is sameas foo.bar }}")
case("test_chain_err", "{{ x is sameas 1 is sameas 2 }}")
case("test_not_followed_else", "{{ 1 if x is defined else 2 }}")
case("test_or_and", "{{ x is defined and y is defined or z }}")
case("test_dotted", "{{ x is what.ever }}")
case("string_concatenation_implicit", '{{ "foo" "bar" "baz" }}')
case("operator_precedence", "{{ 2 * 3 + 4 % 2 + 1 - 2 }}")
case("operator_precedence2", "{{ not 1 + 2 * 3 < 4 }}")
case("implicit_subscribed_tuple", "{{ foo[1, 2] }}")
case("raw_in_expr", "{{ raw }}")  # raw 作为变量名? 其实 raw 是 lexer 关键... 看官方行为
case("neg_filter_priority", "{{ -1|foo }}")
case("pow_chain", "{{ 2**2**3 }}")
case("getattr_call_chain", "{{ a.b().c('x').d[0](1) }}")

# ---- 语句 ----
case("if", "{% if x %}A{% endif %}")
case("if_elif_else", "{% if a %}A{% elif b %}B{% elif c %}C{% else %}D{% endif %}")
case("if_colon", "{% if x: %}A{% endif %}")
case("for", "{% for item in seq %}{{ item }}{% endfor %}")
case("for_else", "{% for i in s %}a{% else %}b{% endfor %}")
case("for_unpack", "{% for a, b in items %}x{% endfor %}")
case("for_unpack_parens", "{% for (a, b) in items %}x{% endfor %}")
case("for_filtered", "{% for i in s if i > 1 %}x{% endfor %}")
case("for_recursive", "{% for i in s recursive %}x{% endfor %}")
case("for_filtered_recursive", "{% for i in s if i recursive %}x{% endfor %}")
case("for_assign_to_const_err", "{% for true in s %}x{% endfor %}")
case("set", "{% set x = 1 %}")
case("set_tuple", "{% set a, b = 1, 2 %}")
case("set_namespace", "{% set ns.x = 1 %}")
case("set_block", "{% set x %}content{% endset %}")
case("set_block_filter", "{% set x | upper %}content{% endset %}")
case("set_invalid_err", "{% set x.y = 1 %}")
case("set_const_err", "{% set true = 1 %}")
case("block", "{% block foo %}x{% endblock %}")
case("block_named_end", "{% block foo %}x{% endblock foo %}")
case("block_scoped", "{% block foo scoped %}x{% endblock %}")
case("block_required", "{% block foo required %}  {% endblock %}")
case("block_required_content_err", "{% block foo required %}x{% endblock %}")
case("block_hyphen_err", "{% block foo-bar %}x{% endblock %}")
case("extends", "{% extends 'base.html' %}")
case("extends_expr", "{% extends layout_template if layout else 'default.html' %}")
case("include", "{% include 'a.html' %}")
case("include_ignore_missing", "{% include 'a.html' ignore missing %}")
case("include_context", "{% include 'a.html' with context %}{% include 'b.html' without context %}")
case("include_list", "{% include ['a', 'b'] %}")
case("import", "{% import 'forms.html' as forms %}")
case("import_context", "{% import 'forms.html' as forms with context %}")
case("from_import", "{% from 'forms.html' import input %}")
case("from_import_as", "{% from 'forms.html' import input as input_field, textarea %}")
case("from_import_context", "{% from 'x' import a, b with context %}")
case("from_import_underscore_err", "{% from 'x' import _private %}")
case("macro", "{% macro m(a, b, c=1, d=2) %}body{% endmacro %}")
case("macro_nondefault_after_default_err", "{% macro m(a=1, b) %}x{% endmacro %}")
case("call_block", "{% call m() %}body{% endcall %}")
case("call_block_args", "{% call(user) dump_users(list_of_users) %}x{% endcall %}")
case("call_block_not_call_err", "{% call foo %}x{% endcall %}")
case("filter_block", "{% filter upper|replace('FOO', 'foo') %}foo{% endfilter %}")
case("with", "{% with a = 1, b = 2 %}{{ a }}{% endwith %}")
case("with_empty", "{% with %}x{% endwith %}")
case("autoescape", "{% autoescape true %}x{% endautoescape %}")
case("print_stmt", "{% print 'x', 1 + 2 %}")

# ---- 错误信息 (test_error_messages 等) ----
case("err_unclosed_if", "{% if foo %}")
case("err_unknown_tag", "{% fou %}")
case("err_unknown_endtag", "{% for item in seq %}...{% endfor item %}")
case("err_nesting", "{% if foo %}{% for item in seq %}...{% endfor %}{% endwhile %}")
case("err_endif_in_for", "{% for item in seq %}{% endif %}")
case("err_unexpected_end_block", "{% endblock %}")
case("err_tag_expected", "{% 1 %}")
case("err_expr", "{{ if }}")
case("err_unexpected_rbrace", "{{ x } }}")

# ---- 行语句 ----
case("line_stmt", "# for item in seq:\n  {{ item }}\n# endfor",
     line_statement_prefix="#")

# ---- 整库冒烟: 多语句组合 ----
case("kitchen_sink", """{% extends "base.html" %}
{% block title %}Members{% endblock %}
{% block content %}
  <ul>
  {% for user in users if not user.hidden recursive %}
    <li><a href="{{ user.url }}">{{ user.username|e }}</a></li>
    {{ loop(user.children) }}
  {% else %}
    <li>no users</li>
  {% endfor %}
  </ul>
{% endblock %}""")


def main():
    out = []
    for name, kwargs, source in CASES:
        env = Environment(**kwargs)
        entry = {"name": name, "source": source, "config": kwargs}
        try:
            entry["repr"] = repr(env.parse(source))
        except TemplateSyntaxError as e:
            entry["error"] = e.message
            entry["error_lineno"] = e.lineno
        out.append(entry)
    json.dump(out, sys.stdout, indent=1, ensure_ascii=False)


if __name__ == "__main__":
    main()

#!/usr/bin/env python3
"""Render 级 conformance 语料生成器.

每个用例: (环境配置, 模板集, 上下文 JSON) -> 官方 Jinja2 的渲染结果或异常.
上下文必须 JSON 可序列化 (dict 保序经 JSON 往返保持).

用法: PYTHONPATH=reference/jinja/src python3 tools/gen_render_fixtures.py > rendertest/testdata/render_fixtures.json
"""
import json
import sys

from jinja2 import DictLoader, Environment, TemplateSyntaxError, UndefinedError
from jinja2 import ChainableUndefined, DebugUndefined, StrictUndefined, Undefined
from jinja2.exceptions import TemplateRuntimeError, TemplateNotFound, TemplatesNotFound

CASES = []


def case(name, source, ctx=None, templates=None, **env_kwargs):
    CASES.append(dict(name=name, source=source, ctx=ctx or {},
                      templates=templates or {}, env=env_kwargs))


# ============ 输出与表达式 ============
case("output_types", "{{ none }}|{{ true }}|{{ false }}|{{ 1 }}|{{ 1.5 }}|{{ 'x' }}")
case("output_containers", "{{ [1, 'a', none] }}|{{ (1,) }}|{{ {'k': [1, 2]} }}")
case("output_nested_quotes", """{{ ["it's", 'he said "hi"'] }}""")
case("math_full", "{{ (1 + 1 * 2) - 3 / 2 }}|{{ 2**3 }}|{{ 3 // 2 }}|{{ 3 % 2 }}|{{ -7 // 2 }}|{{ -7 % 3 }}|{{ 7.5 // 2 }}")
case("math_float_div", "{{ 1 / 2 }}|{{ 4 / 2 }}|{{ 1.0 + 2 }}")
case("unary_ops", "{{ +3 }}|{{ -3 }}|{{ not 0 }}|{{ not 'x' }}|{{ --1 }}")
case("string_repeat", "{{ 'ab' * 3 }}|{{ [1, 2] * 2 }}")
case("concat_tilde", "{{ 1 ~ 'x' ~ none ~ true ~ 1.5 }}")
case("compare_chain", "{{ 1 < 2 < 3 }}|{{ 1 < 2 > 3 }}|{{ 'a' < 'b' }}|{{ [1] < [1, 2] }}|{{ (1, 2) < (1, 3) }}")
case("equality_cross_type", "{{ 1 == 1.0 }}|{{ 1 == True }}|{{ 0 == False }}|{{ '1' == 1 }}|{{ [] == [] }}|{{ {} == {} }}")
case("in_op", "{{ 1 in [1, 2] }}|{{ 'a' in 'abc' }}|{{ 'k' in {'k': 1} }}|{{ 3 not in [1, 2] }}")
case("bool_shortcircuit_value", "{{ 0 or 'fallback' }}|{{ 'first' and 'second' }}|{{ '' or [] }}")
case("condexpr", "{{ 0 if true else 1 }}|{{ 'yes' if 1 == 1 }}|{{ 'no' if false }}")
case("truthiness", "{% for v in [0, 1, '', 'x', [], [1], {}, none, 0.0] %}{{ 'T' if v else 'F' }}{% endfor %}")
case("slicing_str", "{{ 'hello'[1:3] }}|{{ 'hello'[::-1] }}|{{ 'hello'[-3:] }}|{{ '日本語テキスト'[2:4] }}")
case("slicing_list", "{{ [0,1,2,3,4][1:4:2] }}|{{ [0,1,2,3][::-1] }}|{{ [0,1,2][5:] }}")
case("getattr_getitem", "{{ d.k }}|{{ d['k'] }}|{{ l.0 }}|{{ l[0] }}|{{ l[-1] }}",
     ctx={"d": {"k": "v"}, "l": [10, 20, 30]})
case("getattr_missing_renders", "{{ d.missing|default('dflt') }}", ctx={"d": {"k": 1}})
case("string_methods", "{{ 'foo bar'.upper() }}|{{ 'A,B,C'.split(',') }}|{{ '  x '.strip() }}|{{ 'a-b'.replace('-', '+') }}|{{ 'x'.center(5) if false else 'skip' }}")
case("dict_methods", "{{ d.get('a') }}|{{ d.get('z', 'def') }}|{{ d.keys()|list }}|{{ d.items()|list }}", ctx={"d": {"a": 1, "b": 2}})
case("number_formats", "{{ 1e0 }}|{{ 2.5e100 }}|{{ 0x1F }}|{{ 0b101 }}|{{ 1_000 + 1 }}|{{ 12_34_56 }}")
case("tuple_literal_render", "{{ () }}|{{ (1,) }}|{{ (1, 2) }}")
case("implicit_tuple_output", "{{ 1, 2 }}")
case("str_mod_op", "{{ '%s-%d' % ('a', 7) }}|{{ '%05.2f' % 3.14159 }}|{{ '%x|%o|%e' % (255, 8, 12345.678) }}")
case("str_format_method", "{{ '{} {}'.format(1, 'two') }}|{{ '{1}{0}'.format('a', 'b') }}|{{ '{n}!'.format(n=9) }}")

# ============ if / for / set ============
case("if_elif_else", "{% if a %}A{% elif b %}B{% else %}C{% endif %}", ctx={"a": 0, "b": True})
case("for_basic", "{% for item in seq %}{{ item }}{% endfor %}", ctx={"seq": list(range(10))})
case("for_else_empty", "{% for item in [] %}x{% else %}EMPTY{% endfor %}")
case("for_else_nonempty", "{% for item in [1] %}x{% else %}EMPTY{% endfor %}")
case("for_loop_attrs", "{% for i in 'ab' %}{{ loop.index }}{{ loop.index0 }}{{ loop.revindex }}{{ loop.revindex0 }}{{ loop.first }}{{ loop.last }}|{% endfor %}")
case("for_loop_length", "{% for i in [1,2,3] %}{{ loop.length }}{% endfor %}")
case("for_cycle", "{% for i in range(7) %}{{ loop.cycle('a', 'b', 'c') }}{% endfor %}")
case("for_changed", "{% for v in [1, 1, 2, 2, 2, 3] %}{{ loop.changed(v) }},{% endfor %}")
case("for_prev_next", "{% for v in [1, 2, 3] %}[{{ loop.previtem|default('-') }} {{ v }} {{ loop.nextitem|default('-') }}]{% endfor %}")
case("for_unpack", "{% for a, b in [(1, 'x'), (2, 'y')] %}{{ a }}{{ b }};{% endfor %}")
case("for_unpack_dict_items", "{% for k, v in d.items() %}{{ k }}={{ v }},{% endfor %}", ctx={"d": {"a": 1, "b": 2, "c": 3}})
case("for_filtered", "{% for i in range(10) if i % 2 == 0 %}{{ i }}{% endfor %}")
case("for_filtered_loop_attrs", "{% for i in range(10) if i % 2 == 0 %}{{ loop.index }}:{{ i }} {% endfor %}")
case("for_nested_loop", "{% for i in [1, 2] %}{% for j in 'ab' %}{{ i }}{{ j }}{{ loop.index }}|{% endfor %}{% endfor %}")
case("for_recursive", "{% for item in seq recursive %}[{{ item.a }}{% if item.b %}<{{ loop(item.b) }}>{% endif %}]{% endfor %}",
     ctx={"seq": [{"a": 1, "b": [{"a": 1}, {"a": 2}]}, {"a": 2, "b": [{"a": 1}, {"a": 2}]}, {"a": 3, "b": [{"a": "a"}]}]})
case("for_recursive_depth", "{% for item in seq recursive %}{{ loop.depth }}:{{ item.a }}{% if item.b %}<{{ loop(item.b) }}>{% endif %};{% endfor %}",
     ctx={"seq": [{"a": 1, "b": [{"a": 2, "b": [{"a": 3}]}]}]})
case("for_scope_no_leak", "{% for i in [1] %}{% set x = 'in' %}{% endfor %}{{ x|default('out') }}")
case("for_loop_var_shadow", "{% set i = 'outer' %}{% for i in [1] %}{{ i }}{% endfor %}{{ i }}")
case("for_string_iter", "{% for c in 'héllo' %}{{ c }}.{% endfor %}")
case("for_dict_iter_keys", "{% for k in {'b': 1, 'a': 2} %}{{ k }}{% endfor %}")
case("if_scope_leak", "{% if true %}{% set x = 42 %}{% endif %}{{ x }}")
case("set_simple", "{% set x = 1 + 2 %}{{ x }}")
case("set_tuple_unpack", "{% set a, b = 1, 2 %}{{ a }}{{ b }}")
case("set_block", "{% set x %}hello {{ 'world' }}{% endset %}{{ x }}|{{ x.__class__ if false else '' }}")
case("set_block_filter", "{% set x | upper %}hello{% endset %}{{ x }}")
case("set_namespace", "{% set ns = namespace(found=false) %}{% for i in [1, 2] %}{% set ns.found = ns.found + i %}{% endfor %}{{ ns.found }}")
case("set_namespace_init_kwargs", "{% set ns = namespace(a=1, b='x') %}{{ ns.a }}{{ ns.b }}")
case("set_namespace_loop_counter", "{% set ns = namespace(c=0) %}{% for i in range(5) %}{% if i % 2 %}{% set ns.c = ns.c + 1 %}{% endif %}{% endfor %}{{ ns.c }}")
case("with_stmt", "{% with a = 1, b = a + 1 %}{{ a }},{{ b }}{% endwith %}{{ a is defined }}")
case("with_empty_vars", "{% set a = 1 %}{% with %}{% set a = 2 %}{{ a }}{% endwith %}{{ a }}")

# ============ 过滤器管道与 test ============
case("filter_chain_pipe", "{{ 'foo'|upper|lower }}|{{ [3, 1, 2]|sort|join(',') }}")
case("filter_priority_unary", "{{ -1|abs }}|{{ (-1)|abs }}")
case("tests_basic", "{{ x is defined }}|{{ y is defined }}|{{ none is none }}|{{ 1 is odd }}|{{ 2 is even }}|{{ 4 is divisibleby 2 }}", ctx={"x": 1})
case("tests_types", "{{ 1 is integer }}|{{ 1.0 is float }}|{{ 1 is number }}|{{ true is boolean }}|{{ 'x' is string }}|{{ [] is sequence }}|{{ {} is mapping }}|{{ x is callable }}", ctx={"x": 1})
case("test_sameas", "{{ a is sameas b }}|{{ true is sameas true }}|{{ 1 is sameas 1.0 }}", ctx={"a": "s", "b": "s"})
case("test_in", "{{ 1 is in([1, 2]) }}|{{ 'x' is in('xyz') }}")
case("test_comparison_aliases", "{{ 1 is eq 1 }}|{{ 1 is ne 2 }}|{{ 1 is lt 2 }}|{{ 2 is gt 1 }}|{{ 1 is le 1 }}|{{ 1 is ge 1 }}")
case("test_not_chain", "{{ 1 is not none }}|{{ none is not none }}")

# ============ 自动转义与 safe ============
case("autoescape_on", "{{ v }}|{{ v|safe }}|{{ v|e }}", ctx={"v": "<b>&'\"</b>"}, autoescape=True)
case("autoescape_off", "{{ v }}|{{ v|e }}|{{ v|e|e if false else '' }}", ctx={"v": "<b>"})
case("autoescape_block", "{% autoescape true %}{{ v }}{% endautoescape %}|{% autoescape false %}{{ v }}{% endautoescape %}", ctx={"v": "<x>"})
case("autoescape_concat", "{{ '<b>' ~ v }}", ctx={"v": "<i>"}, autoescape=True)
case("autoescape_add_markup", "{{ ('<b>'|safe) + v }}", ctx={"v": "<i>"}, autoescape=True)
case("escape_double", "{{ v|e|e }}", ctx={"v": "<"}, autoescape=True)
case("escape_idempotent_markup", "{{ ('<b>'|safe)|e }}", autoescape=True)

# ============ macro ============
case("macro_simple", "{% macro say(name) %}Hello {{ name }}!{% endmacro %}{{ say('World') }}")
case("macro_defaults", "{% macro m(a, b=2, c=3) %}{{ a }}{{ b }}{{ c }}{% endmacro %}{{ m(1) }}|{{ m(1, 9) }}|{{ m(1, c=8) }}")
case("macro_default_refs_arg", "{% macro m(a, b=a) %}{{ a }}{{ b }}{% endmacro %}{{ m(1) }}|{{ m(1, 2) }}")
case("macro_varargs", "{% macro m(a) %}{{ a }}{{ varargs }}{% endmacro %}{{ m(1, 2, 3) }}")
case("macro_kwargs", "{% macro m(a) %}{{ a }}{{ kwargs }}{% endmacro %}{{ m(1, x=2) }}")
case("macro_too_many_args_err", "{% macro m(a) %}{{ a }}{% endmacro %}{{ m(1, 2) }}")
case("macro_unknown_kwarg_err", "{% macro m(a) %}{{ a }}{% endmacro %}{{ m(1, b=2) }}")
case("macro_missing_arg_undefined", "{% macro m(a, b) %}{{ a }}{{ b }}{% endmacro %}{{ m(1) }}")
case("macro_caller", "{% macro wrap() %}<{{ caller() }}>{% endmacro %}{% call wrap() %}body{% endcall %}")
case("macro_caller_args", "{% macro dump(users) %}{% for u in users %}{{ caller(u) }}{% endfor %}{% endmacro %}{% call(u) dump(['a', 'b']) %}[{{ u }}]{% endcall %}")
case("macro_caller_undefined", "{% macro m() %}{{ caller is undefined }}{% endmacro %}{{ m() }}")
case("macro_closure", "{% set greeting = 'Hi' %}{% macro m(n) %}{{ greeting }} {{ n }}{% endmacro %}{{ m('x') }}")
case("macro_recursion", "{% macro fact(n) %}{% if n <= 1 %}1{% else %}{{ n * fact(n - 1)|int }}{% endif %}{% endmacro %}{{ fact(5) }}")
case("macro_call_star_args", "{% macro m(a, b) %}{{ a }}{{ b }}{% endmacro %}{{ m(*[1, 2]) }}|{{ m(**{'a': 3, 'b': 4}) }}")
case("macro_string_attr", "{% macro m() %}x{% endmacro %}{{ m.name }}")

# ============ 继承 ============
LAYOUT = "|{% block block1 %}block 1 from layout{% endblock %}|{% block block2 %}block 2 from layout{% endblock %}|{% block block3 %}{% block block4 %}nested block 4 from layout{% endblock %}{% endblock %}|"
LEVEL1 = "{% extends 'layout' %}{% block block1 %}block 1 from level1{% endblock %}"
LEVEL2 = "{% extends 'level1' %}{% block block2 %}{% block block5 %}nested block 5 from level2{% endblock %}{% endblock %}"
LEVEL3 = "{% extends 'level2' %}{% block block5 %}block 5 from level3{% endblock %}{% block block4 %}block 4 from level3{% endblock %}"
LEVEL4 = "{% extends 'level3' %}{% block block3 %}block 3 from level4{% endblock %}"
INHERIT_TPLS = {"layout": LAYOUT, "level1": LEVEL1, "level2": LEVEL2,
                "level3": LEVEL3, "level4": LEVEL4}
case("inherit_layout", "{% include 'layout' %}", templates=INHERIT_TPLS)
case("inherit_level1", "{% include 'level1' %}", templates=INHERIT_TPLS)
case("inherit_level2", "{% include 'level2' %}", templates=INHERIT_TPLS)
case("inherit_level3", "{% include 'level3' %}", templates=INHERIT_TPLS)
case("inherit_level4", "{% include 'level4' %}", templates=INHERIT_TPLS)
case("inherit_super", "{% extends 'parent' %}{% block b %}{{ super() }}+child{% endblock %}",
     templates={"parent": "[{% block b %}parent{% endblock %}]"})
case("inherit_double_super", "{% extends 'mid' %}{% block b %}{{ super.super() }}+leaf{% endblock %}",
     templates={"mid": "{% extends 'base' %}{% block b %}{{ super() }}+mid{% endblock %}",
                "base": "[{% block b %}base{% endblock %}]"})
case("inherit_scoped_block", "{% extends 'base' %}{% block item %}[{{ item }}]{% endblock %}",
     templates={"base": "{% for item in [1, 2] %}{% block item scoped %}{{ item }}{% endblock %}{% endfor %}"})
case("inherit_unscoped_block_no_loopvar", "{% extends 'base' %}{% block item %}[{{ item|default('?') }}]{% endblock %}",
     templates={"base": "{% for item in [1, 2] %}{% block item %}x{% endblock %}{% endfor %}"})
case("inherit_data_outside_block_dropped", "{% extends 'base' %}IGNORED{% block b %}B{% endblock %}ALSO IGNORED",
     templates={"base": "({% block b %}{% endblock %})"})
case("inherit_set_before_extends", "{% set x = 42 %}{% extends 'base' %}{% block b %}{{ x }}{% endblock %}",
     templates={"base": "[{% block b %}{% endblock %}]"})
case("inherit_self_ref", "{% block title %}T{% endblock %}|{{ self.title() }}")
case("inherit_block_in_for", "{% for i in [1, 2] %}{% block x scoped %}{{ i }}{% endblock %}{% endfor %}")
case("inherit_required_provided", "{% extends 'base' %}{% block b %}ok{% endblock %}",
     templates={"base": "[{% block b required %}{% endblock %}]"})
case("inherit_required_missing_err", "{% extends 'base' %}",
     templates={"base": "[{% block b required %}{% endblock %}]"})
case("inherit_dynamic_extends", "{% extends which %}{% block b %}child{% endblock %}",
     ctx={"which": "p2"},
     templates={"p1": "1:{% block b %}{% endblock %}", "p2": "2:{% block b %}{% endblock %}"})
case("inherit_super_in_nested", "{% extends 'base' %}{% block outer %}{{ super() }}/{% block inner %}I{% endblock %}{% endblock %}",
     templates={"base": "{% block outer %}O{% block inner %}i{% endblock %}{% endblock %}"})
case("extends_multiple_err", "{% extends 'a' %}{% extends 'b' %}",
     templates={"a": "A", "b": "B"})

# ============ include / import ============
case("include_basic", "A{% include 'inc' %}C", templates={"inc": "B{{ x|default('') }}"})
case("include_with_context", "{% set x = 42 %}{% include 'inc' %}", templates={"inc": "[{{ x|default('no') }}]"})
case("include_without_context", "{% set x = 42 %}{% include 'inc' without context %}", templates={"inc": "[{{ x|default('no') }}]"})
case("include_ignore_missing", "A{% include 'nope' ignore missing %}B")
case("include_missing_err", "A{% include 'nope' %}B")
case("include_list_fallback", "{% include ['nope', 'inc'] %}", templates={"inc": "found"})
case("include_loop_var_visible", "{% for i in [1, 2] %}{% include 'inc' %}{% endfor %}", templates={"inc": "[{{ i }}]"})
case("import_module", "{% import 'forms' as forms %}{{ forms.field('user') }}",
     templates={"forms": "{% macro field(name) %}<input name=\"{{ name }}\">{% endmacro %}"})
case("import_exported_var", "{% import 'mod' as m %}{{ m.answer }}",
     templates={"mod": "{% set answer = 42 %}"})
case("from_import", "{% from 'forms' import field, label as lbl %}{{ field('u') }}{{ lbl() }}",
     templates={"forms": "{% macro field(n) %}F:{{ n }}{% endmacro %}{% macro label() %}L{% endmacro %}"})
case("import_without_context_default", "{% set x = 1 %}{% import 'mod' as m %}{{ m.echo() }}",
     templates={"mod": "{% macro echo() %}{{ x|default('nox') }}{% endmacro %}"})
case("import_with_context", "{% set x = 1 %}{% import 'mod' as m with context %}{{ m.echo() }}",
     templates={"mod": "{% macro echo() %}{{ x|default('nox') }}{% endmacro %}"})
case("from_import_missing_name", "{% from 'mod' import nothing %}{{ nothing|default('undef') }}",
     templates={"mod": "x"})

# ============ Undefined 行为 ============
case("undef_default_print", "[{{ missing }}]")
case("undef_default_iter", "{% for x in missing %}x{% endfor %}done")
case("undef_default_attr_err", "{{ missing.attr }}")
case("undef_default_compare", "{{ missing == missing2 }}|{{ missing == 1 }}")
case("undef_add_err", "{{ missing + 1 }}")
case("undef_chainable", "[{{ missing.a.b['c'].d }}]", undefined="chainable")
case("undef_debug_print", "[{{ missing }}]", undefined="debug")
case("undef_strict_print_err", "[{{ missing }}]", undefined="strict")
case("undef_strict_defined_test", "{{ missing is defined }}|{{ given is defined }}", ctx={"given": 1}, undefined="strict")
case("undef_hint_getattr", "{{ d.missing.fail }}", ctx={"d": {}})

# ============ filter block / 其他语句 ============
case("filter_block", "{% filter upper %}foo {{ 'bar' }}{% endfilter %}")
case("filter_block_chain", "{% filter lower|capitalize %}HELLO WORLD{% endfilter %}")
case("whitespace_full", "  {% if true %}\n    x\n  {% endif %}  ", trim_blocks=True, lstrip_blocks=True)
case("raw_renders_literal", "{% raw %}{{ not evaluated }}{% endraw %}")

# ============ 错误信息 ============
case("err_no_filter", "{{ 1|nosuchfilter }}")
case("err_no_test", "{{ 1 is nosuchtest }}")
case("err_unhashable", "{{ {[]: 1} }}")
case("err_call_int", "{{ 1() }}")
case("err_iterate_int", "{% for x in 7 %}{% endfor %}")
case("err_compare_mixed", "{{ 1 < 'x' }}")
case("err_add_mixed", "{{ 1 + 'x' }}")


def make_env(spec, templates):
    kwargs = dict(spec)
    und = kwargs.pop("undefined", None)
    exts = kwargs.pop("extensions", None)
    null_trans = kwargs.pop("install_null_translations", False)
    policies = kwargs.pop("policies", None)
    if null_trans:
        exts = list(exts or []) + ["i18n"]
    if exts:
        kwargs["extensions"] = ["jinja2.ext." + e for e in exts]
    if und:
        kwargs["undefined"] = {"chainable": ChainableUndefined,
                               "debug": DebugUndefined,
                               "strict": StrictUndefined}[und]
    if templates:
        kwargs["loader"] = DictLoader(templates)
    env = Environment(**kwargs)
    if null_trans:
        env.install_null_translations()
    if policies:
        env.policies.update(policies)
    return env


# 已知文档化差异 (int64 vs Python 任意精度整数等)
KNOWN_DIVERGENCES = {
    "f_int_123456789012": "int64: Python 任意精度整数超出 int64 范围",
}


def main():
    out = []
    for c in CASES:
        env = make_env(c["env"], c["templates"])
        entry = {k: c[k] for k in ("name", "source", "ctx", "templates", "env")}
        if c["name"] in KNOWN_DIVERGENCES:
            entry["known_divergence"] = KNOWN_DIVERGENCES[c["name"]]
        try:
            entry["output"] = env.from_string(c["source"]).render(**c["ctx"])
        except (TemplateSyntaxError, TemplateRuntimeError, UndefinedError,
                TemplateNotFound, TemplatesNotFound, TypeError, LookupError) as e:
            entry["error_type"] = type(e).__name__
            entry["error"] = str(e)
        out.append(entry)
    json.dump(out, sys.stdout, indent=1, ensure_ascii=False)




# ============ M4: filters 全家桶 (对照 test_filters.py) ============
def filter_cases():
    case("f_capitalize", '{{ "foo bar"|capitalize }}')
    case("f_center", '{{ "foo"|center(9) }}|{{ "foo"|center(2) }}|{{ "fooo"|center(9) }}')
    case("f_default", "{{ missing|default('no') }}|{{ false|default('no') }}|{{ false|default('no', true) }}|{{ given|default('no') }}", ctx={"given": "yes"})
    for i, args in enumerate(["", "(true)", "(by='value')", "(reverse=true)"]):
        case(f"f_dictsort_{i}", "{{ {'aa': 0, 'b': 1, 'c': 2, 'AB': 3}|dictsort%s }}" % args)
    case("f_batch", "{{ foo|batch(3)|list }}|{{ foo|batch(3, 'X')|list }}", ctx={"foo": list(range(10))})
    case("f_slice", "{{ foo|slice(3)|list }}|{{ foo|slice(3, 'X')|list }}", ctx={"foo": list(range(10))})
    case("f_escape", """{{ '<">&'|escape }}""")
    for chars, expect in [(None, ""), ("None", ""), ("'  ..  '", "")]:
        pass
    case("f_trim_default", "{{ '  ..stays..  '|trim }}")
    case("f_trim_chars", "{{ '  ..stays..  '|trim('.') }}|{{ '  ..stays..  '|trim(' .') }}")
    case("f_striptags", "{{ s|striptags }}", ctx={"s": '  <p>just a small   \n <a href="#">example</a> link</p>\n<p>to a webpage</p> <!-- <p>and some commented stuff</p> -->'})
    case("f_filesizeformat", "{{ 100|filesizeformat }}|{{ 1000|filesizeformat }}|{{ 1000000|filesizeformat }}|{{ 1000000000|filesizeformat }}|{{ 1000000000000|filesizeformat }}|{{ 100|filesizeformat(true) }}|{{ 1000|filesizeformat(true) }}|{{ 1000000|filesizeformat(true) }}")
    case("f_filesizeformat2", "{{ 300|filesizeformat }}|{{ 3000|filesizeformat }}|{{ 3000000|filesizeformat }}|{{ 3000000000|filesizeformat }}|{{ 3000000000000|filesizeformat }}|{{ 300|filesizeformat(true) }}|{{ 3000|filesizeformat(true) }}|{{ 3000000|filesizeformat(true) }}|{{ 1|filesizeformat }}")
    case("f_first", "{{ foo|first }}", ctx={"foo": list(range(10))})
    for value, expect in [("42", "42.0"), ("abc", "0.0"), ("32.32", "32.32")]:
        case(f"f_float_{value}", "{{ '%s'|float }}" % value)
    case("f_float_default", "{{ 'abc'|float(default=1.0) }}")
    case("f_format", "{{ '%s|%s'|format('a', 'b') }}")
    case("f_indent", "{{ s|indent(2, false, false) }}X{{ s|indent(2, false, true) }}X{{ s|indent(2, true, false) }}X{{ s|indent(2, true, true) }}",
         ctx={"s": "\nfoo bar\n\"baz\"\n"})
    case("f_indent_width_string", "{{ 'jinja\nflask'|indent('>>> ', true) }}")
    case("f_indent_oneline", "{{ 'jinja'|indent }}")
    for value, expect in [("42", "42"), ("abc", "0"), ("32.32", "32"), ("12345678901234567890", "12345678901234567890")]:
        case(f"f_int_{value[:12]}", "{{ '%s'|int }}" % value)
    for value, base in [("0x4d32", 16), ("011", 8), ("0x33Z", 16)]:
        case(f"f_int_base_{value}", "{{ '%s'|int(0, %d) }}" % (value, base))
    case("f_int_default", "{{ 'abc'|int(default=42) }}")
    case("f_join", "{{ [1, 2, 3]|join('|') }}")
    case("f_join_autoescape", '{{ ["<foo>", "<span>foo</span>"|safe]|join }}', autoescape=True)
    case("f_join_attribute", "{{ users|join(', ', 'username') }}", ctx={"users": [{"username": "foo"}, {"username": "bar"}]})
    case("f_last", "{{ foo|last }}", ctx={"foo": list(range(10))})
    case("f_length", "{{ 'hello world'|length }}|{{ [1,2,3]|length }}|{{ {'a':1}|length }}")
    case("f_lower", "{{ 'FOO'|lower }}")
    case("f_items", "{{ d|items|list }}", ctx={"d": {"a": "x", "b": "y"}})
    case("f_items_undefined", "{{ missing|items|list }}")
    case("f_pprint", "{{ l|pprint }}", ctx={"l": [1, [2, "three"], {"k": "v"}]})
    case("f_string", "{{ obj|string }}", ctx={"obj": [1, 2, 3]})
    case("f_title", "{{ 'foo bar'|title }}|{{ \"foo's bar\"|title }}|{{ 'foo-bar'|title }}|{{ 'foo\tbar'|title }}|{{ 'FOO\tBAR'|title }}|{{ 'foo (bar)'|title }}|{{ 'foo {bar}'|title }}|{{ 'foo [bar]'|title }}|{{ 'foo <bar>'|title }}")
    case("f_truncate", "{{ data|truncate(15, true, '>>>') }}|{{ data|truncate(15, false, '>>>') }}|{{ smalldata|truncate(15) }}",
         ctx={"data": "foobar baz bar" * 10, "smalldata": "foobar baz bar"})
    case("f_truncate_very_short", "{{ 'foo bar baz'|truncate(9) }}|{{ 'foo bar baz'|truncate(9, true) }}")
    case("f_truncate_end_length", "{{ 'Joel is a slug'|truncate(7, true) }}")
    case("f_upper", "{{ 'foo'|upper }}")
    case("f_urlize", '{{ "foo example.org bar"|urlize }}')
    case("f_urlize_https", '{{ "foo https://www.example.com/ bar"|urlize }}')
    case("f_urlize_mail", '{{ "foo contact@example.com bar"|urlize }}')
    case("f_urlize_target", '{{ "foo example.org bar"|urlize(target="_blank") }}')
    case("f_urlize_trim", '{{ "foo http://www.example.com/longurl bar"|urlize(10, true) }}')
    case("f_wordcount", "{{ 'foo bar baz'|wordcount }}")
    case("f_block_filter", "{% filter lower|escape %}<HEHE>{% endfilter %}")
    case("f_chaining", """{{ ['<foo>', '<bar>']|first|upper|escape }}""")
    case("f_sum", "{{ [1, 2, 3, 4, 5, 6]|sum }}")
    case("f_sum_attributes", "{{ values|sum('value') }}", ctx={"values": [{"value": 23}, {"value": 1}, {"value": 18}]})
    case("f_sum_attributes_nested", "{{ values|sum('real.value') }}",
         ctx={"values": [{"real": {"value": 23}}, {"real": {"value": 1}}, {"real": {"value": 18}}]})
    case("f_sum_start", "{{ [1, 2]|sum(start=5) }}")
    case("f_abs", "{{ -1|abs }}|{{ 1|abs }}|{{ -1.5|abs }}")
    case("f_round_positive", "{{ 2.7|round }}|{{ 2.1|round }}|{{ 2.1234|round(3, 'floor') }}|{{ 2.1|round(0, 'ceil') }}")
    case("f_round_negative", "{{ 21.3|round(-1)}}|{{ 21.3|round(-1, 'ceil')}}|{{ 21.3|round(-1, 'floor')}}")
    case("f_round_halfeven", "{{ 0.5|round }}|{{ 1.5|round }}|{{ 2.5|round }}")
    case("f_xmlattr", "{{ {'foo': 42, 'bar': 23, 'fish': none, 'spam': missing, 'blub:blub': '<?>'}|xmlattr }}")
    case("f_sort1", "{{ [2, 3, 1]|sort }}|{{ [2, 3, 1]|sort(true) }}")
    case("f_sort2", '{{ "".join(["c", "A", "b", "D"]|sort) }}')
    case("f_sort3", "{{ ['foo', 'Bar', 'blah']|sort }}")
    case("f_sort4", "{{ items|sort(attribute='value')|join(',', 'value') }}", ctx={"items": [{"value": 3}, {"value": 2}, {"value": 4}, {"value": 1}]})
    case("f_sort5", "{{ items|sort(attribute='value.0')|join(',', 'value.0') }}", ctx={"items": [{"value": [3]}, {"value": [2]}, {"value": [4]}, {"value": [1]}]})
    case("f_sort6", "{{ items|sort(attribute='value1,value2')|join(',', 'value1') }}",
         ctx={"items": [{"value1": 3, "value2": 1}, {"value1": 2, "value2": 2}, {"value1": 2, "value2": 1}, {"value1": 1, "value2": 99}]})
    case("f_sort_multi_render", "{{ items|sort(attribute='a,b')|map(attribute='b')|join(',') }}",
         ctx={"items": [{"a": 1, "b": 9}, {"a": 1, "b": 2}, {"a": 0, "b": 5}]})
    case("f_unique", '{{ "".join(["b", "A", "a", "b"]|unique) }}')
    case("f_unique_case_sensitive", '{{ "".join(["b", "A", "a", "b"]|unique(true)) }}')
    case("f_unique_attribute", "{{ items|unique(attribute='value')|join(',', 'value') }}", ctx={"items": [{"value": 3}, {"value": 2}, {"value": 4}, {"value": 1}, {"value": 2}]})
    for name, source in [("min", "{{ ['a', 'B']|min }}"), ("min_cs", "{{ ['a', 'B']|min(case_sensitive=true) }}"),
                         ("min_empty", "{{ []|min }}"), ("max", "{{ ['a', 'B']|max }}"),
                         ("max_cs", "{{ ['a', 'B']|max(case_sensitive=true) }}"), ("max_empty", "{{ []|max }}")]:
        case(f"f_{name}", source)
    case("f_min_attribute", "{{ items|min(attribute='value') }}", ctx={"items": [{"value": 5}, {"value": 1}, {"value": 9}]})
    case("f_max_attribute", "{{ items|max(attribute='value') }}", ctx={"items": [{"value": 5}, {"value": 1}, {"value": 9}]})
    case("f_groupby", "{% for grouper, list in [{'foo': 1, 'bar': 2}, {'foo': 2, 'bar': 3}, {'foo': 1, 'bar': 1}, {'foo': 3, 'bar': 4}]|groupby('foo') %}{{ grouper }}{% for x in list %}: {{ x.foo }}, {{ x.bar }}{% endfor %}|{% endfor %}")
    case("f_groupby_tuple_index", "{% for grouper, list in [('a', 1), ('a', 2), ('b', 1)]|groupby(0) %}{{ grouper }}{% for x in list %}:{{ x.1 }}{% endfor %}|{% endfor %}")
    case("f_groupby_multidot", "{% for year, list in articles|groupby('date.year') %}{{ year }}{% for x in list %}[{{ x.title }}]{% endfor %}|{% endfor %}",
         ctx={"articles": [{"title": "aha", "date": {"day": 1, "month": 1, "year": 1970}}, {"title": "interesting", "date": {"day": 2, "month": 1, "year": 1970}}, {"title": "really?", "date": {"day": 3, "month": 1, "year": 1970}}, {"title": "totally not", "date": {"day": 1, "month": 1, "year": 1971}}]})
    case("f_groupby_default", "{% for city, items in users|groupby('city', default='NY') %}{{ city }}: {{ items|map(attribute='name')|join(', ') }}|{% endfor %}",
         ctx={"users": [{"name": "emma", "city": "NY"}, {"name": "smith", "city": "WA"}, {"name": "john"}]})
    for cs, name in [(False, "f_groupby_nocase"), (True, "f_groupby_case")]:
        case(name, "{% for k, vs in data|groupby('k', case_sensitive=" + ("true" if cs else "false") + ") %}{{ k }}={{ vs|join(':', 'v') }}|{% endfor %}",
             ctx={"data": [{"k": "a", "v": 1}, {"k": "B", "v": 2}, {"k": "b", "v": 3}, {"k": "A", "v": 4}]})
    case("f_filtertag", "{% filter upper|replace('FOO', 'foo') %}foobar{% endfilter %}")
    case("f_replace", "{{ string|replace('o', 42) }}", ctx={"string": "<foo>"})
    case("f_replace_autoescape", "{{ string|replace('o', 42) }}|{{ string|replace('<', 42) }}|{{ string|replace('o', ' '|safe) }}",
         ctx={"string": "<foo>"}, autoescape=True)
    case("f_forceescape", "{{ x|forceescape }}", ctx={"x": "<div />"})
    case("f_safe", "{{ '<div>foo</div>'|safe }}|{{ '<div>foo</div>' }}", autoescape=True)
    for value, expect in [("Hello, world!", ""), ("Hello, world‽", ""), ({"f": 1}, ""), ([("f", 1), ("z", 2)], "")]:
        pass
    case("f_urlencode_str", "{{ 'Hello, world!'|urlencode }}")
    case("f_urlencode_unicode", "{{ 'Hello, world‽'|urlencode }}")
    case("f_urlencode_dict", "{{ {'f': 1, 'z': 2}|urlencode }}")
    case("f_urlencode_slash", "{{ '/path/to thing'|urlencode }}")
    case("f_simple_map", '{{ ["1", "2", "3"]|map("int")|sum }}')
    case("f_map_sum", '{{ [[1,2], [3], [4,5,6]]|map("sum")|list }}')
    case("f_attribute_map", '{{ users|map(attribute="name")|join("|") }}', ctx={"users": [{"name": "john"}, {"name": "jane"}, {"name": "mike"}]})
    case("f_empty_map", '{{ none|map("upper")|list }}')
    case("f_map_default", '{{ users|map(attribute="lastname", default="smith")|join(", ") }}',
         ctx={"users": [{"firstname": "john", "lastname": "lennon"}, {"firstname": "jane", "lastname": "edwards"}, {"firstname": "jon"}]})
    case("f_map_default_list", '{{ users|map(attribute="lastname", default=["smith","x"])|list }}',
         ctx={"users": [{"firstname": "john", "lastname": "lennon"}, {"firstname": "jon"}]})
    case("f_simple_select", '{{ [1, 2, 3, 4, 5]|select("odd")|join("|") }}')
    case("f_bool_select", '{{ [none, false, 0, 1, 2, 3, 4, 5]|select|join("|") }}')
    case("f_simple_reject", '{{ [1, 2, 3, 4, 5]|reject("odd")|join("|") }}')
    case("f_bool_reject", '{{ [none, false, 0, 1, 2, 3, 4, 5]|reject|join("|") }}')
    case("f_simple_select_attr", '{{ users|selectattr("is_active")|map(attribute="name")|join("|") }}',
         ctx={"users": [{"name": "john", "is_active": True}, {"name": "jane", "is_active": True}, {"name": "mike", "is_active": False}]})
    case("f_simple_reject_attr", '{{ users|rejectattr("is_active")|map(attribute="name")|join("|") }}',
         ctx={"users": [{"name": "john", "is_active": True}, {"name": "jane", "is_active": True}, {"name": "mike", "is_active": False}]})
    case("f_func_select_attr", '{{ users|selectattr("id", "odd")|map(attribute="name")|join("|") }}',
         ctx={"users": [{"id": 1, "name": "john"}, {"id": 2, "name": "jane"}, {"id": 3, "name": "mike"}]})
    case("f_func_reject_attr", '{{ users|rejectattr("id", "odd")|map(attribute="name")|join("|") }}',
         ctx={"users": [{"id": 1, "name": "john"}, {"id": 2, "name": "jane"}, {"id": 3, "name": "mike"}]})
    case("f_json_dump", "{{ x|tojson }}", ctx={"x": {"foo": "bar"}})
    case("f_json_dump_str", '{{ "\'bar&baz\'"|tojson }}')
    case("f_json_dump_html", "{{ x|tojson }}", ctx={"x": "<bar>"})
    case("f_json_dump_indent", "{{ x|tojson(indent=2) }}", ctx={"x": {"b": 1, "a": [1, {"c": 2}]}})
    case("f_json_types", "{{ [1, 1.5, none, true, false, 'x', [1], {'k': 'v'}]|tojson }}")
    case("f_wordwrap", "{{ s|wordwrap(20) }}", ctx={"s": "Hello!\nThis is Jinja saying something."})
    case("f_wordwrap_long", "{{ s|wordwrap(10) }}", ctx={"s": "supercalifragilisticexpialidocious word"})
    case("f_filter_undefined_err", "{{ var|f }}")
    case("f_attr", "{{ d|attr('key') }}|{{ d|attr('items') is defined }}", ctx={"d": {"key": "value"}})

filter_cases()


# ============ M4: tests 全家桶 (对照 test_tests.py) ============
def test_cases():
    case("t_defined", "{{ missing is defined }}|{{ true is defined }}")
    case("t_even_odd", "{{ 1 is odd }}|{{ 2 is odd }}|{{ 1 is even }}|{{ 2 is even }}")
    case("t_lower_upper", "{{ 'foo' is lower }}|{{ 'FOO' is lower }}|{{ 'FOO' is upper }}|{{ 'foo' is upper }}")
    case("t_types", "{{ none is none }}|{{ false is none }}|{{ true is none }}|{{ 42 is none }}|"
                    "{{ none is true }}|{{ false is true }}|{{ true is true }}|{{ 0 is true }}|{{ 1 is true }}|{{ 42 is true }}|"
                    "{{ none is false }}|{{ false is false }}|{{ true is false }}|{{ 0 is false }}|"
                    "{{ none is boolean }}|{{ false is boolean }}|{{ 0 is boolean }}|{{ 0.0 is boolean }}|"
                    "{{ 42 is integer }}|{{ 42.0 is integer }}|{{ true is integer }}|"
                    "{{ 42.0 is float }}|{{ 42 is float }}|"
                    "{{ 42 is number }}|{{ 42.0 is number }}|{{ true is number }}|{{ 'x' is number }}")
    case("t_sequence", "{{ [1, 2, 3] is sequence }}|{{ 'foo' is sequence }}|{{ 42 is sequence }}")
    case("t_mapping", "{{ {} is mapping }}|{{ [] is mapping }}|{{ 'x' is mapping }}")
    case("t_iterable", "{{ [1] is iterable }}|{{ 'x' is iterable }}|{{ 42 is iterable }}|{{ none is iterable }}")
    case("t_callable", "{{ range is callable }}|{{ 42 is callable }}")
    case("t_sameas2", "{{ foo is sameas false }}|{{ 0 is sameas false }}", ctx={"foo": False})
    case("t_no_paren_for_arg1", "{{ foo is sameas none }}", ctx={"foo": None})
    case("t_escaped", "{{ x is escaped }}|{{ y is escaped }}", ctx={"x": "foo", "y": "foo"}, autoescape=True)
    case("t_escaped_safe", "{{ ('x'|safe) is escaped }}")
    case("t_greaterthan", "{{ 1 is greaterthan 0 }}|{{ 0 is greaterthan 1 }}")
    case("t_lessthan", "{{ 0 is lessthan 1 }}|{{ 1 is lessthan 0 }}")
    case("t_multiple_tests", "{{ 'us-west-1' is matching '(us-east-1|ap-northeast-1)' or 'stage' is matching '(dev|stage)' }}" if False else "{{ 1 is odd and 2 is even }}")
    case("t_in", "{{ 5 in [1, 2, 3] }}|{{ 2 in [1, 2, 3] }}")
    case("t_name_undefined_err", "{{ x is f }}")
    case("t_filter_test", "{{ 'upper' is filter }}|{{ 'nope' is filter }}|{{ 'odd' is test }}|{{ 'nope' is test }}")

test_cases()


# ============ M4: globals ============
def global_cases():
    case("g_range", "{{ range(5)|list }}|{{ range(2, 5)|list }}|{{ range(0, 10, 3)|list }}|{{ range(5, 0, -1)|list }}")
    case("g_dict", "{{ dict(a=1, b=2)|dictsort }}")
    case("g_cycler", "{% set c = cycler('a', 'b') %}{{ c.next() }}{{ c.next() }}{{ c.next() }}{{ c.current }}{% set _ = c.reset() %}{{ c.current }}")
    case("g_joiner", "{% set pipe = joiner('|') %}{{ pipe() }}1{{ pipe() }}2{{ pipe() }}3")
    case("g_lipsum_html", "{{ lipsum(2)[:3] }}")
    case("g_namespace_global", "{{ namespace(a=1).a }}")

global_cases()


# ============ M6: 扩展 (do / loopcontrols / i18n) ============
def ext_cases():
    case("ext_do", "{% set l = [] %}{% do l.append(1) if false else none %}{{ l }}|{% do 1 + 1 %}done",
         extensions=["do"])
    case("ext_do_expr", "{% set ns = namespace(x=0) %}{% do ns.__setattr__ if false else none %}{{ ns.x }}", extensions=["do"])
    case("ext_break", "{% for i in range(10) %}{% if i > 2 %}{% break %}{% endif %}{{ i }}{% endfor %}",
         extensions=["loopcontrols"])
    case("ext_continue", "{% for i in range(6) %}{% if i % 2 == 0 %}{% continue %}{% endif %}{{ i }}{% endfor %}",
         extensions=["loopcontrols"])
    case("ext_break_nested", "{% for i in [1, 2] %}{% for j in range(5) %}{% if j > i %}{% break %}{% endif %}{{ i }}{{ j }};{% endfor %}|{% endfor %}",
         extensions=["loopcontrols"])
    case("ext_trans_simple", "{% trans %}Hello {{ user }}!{% endtrans %}",
         ctx={"user": "World"}, install_null_translations=True)
    case("ext_trans_plural", "{% trans count=n %}{{ count }} apple{% pluralize %}{{ count }} apples{% endtrans %}",
         ctx={"n": 1}, install_null_translations=True)
    case("ext_trans_plural2", "{% trans count=n %}{{ count }} apple{% pluralize %}{{ count }} apples{% endtrans %}",
         ctx={"n": 3}, install_null_translations=True)
    case("ext_trans_vars", "{% trans a=1, b='x' %}{{ a }}-{{ b }}{% endtrans %}",
         install_null_translations=True)
    case("ext_trans_trimmed", "{% trans trimmed %}  hello\n  world  {% endtrans %}",
         install_null_translations=True)
    case("ext_trans_percent", "{% trans %}100%{% endtrans %}|{% trans x=1 %}{{ x }} is 100%{% endtrans %}",
         install_null_translations=True)
    case("ext_trans_autoescape", "{% trans u=user %}<b>{{ u }}</b>{% endtrans %}",
         ctx={"user": "<x>"}, autoescape=True, install_null_translations=True)
    case("ext_trans_nested_err", "{% trans %}a{% trans %}b{% endtrans %}{% endtrans %}",
         install_null_translations=True)
    case("ext_trans_control_err", "{% trans %}{% if x %}a{% endif %}{% endtrans %}",
         install_null_translations=True)
    case("ext_unknown_tag_without_ext", "{% do 1 %}")

ext_cases()


# ============ M5: loader 行为 (含 include/extends 交互) ============
def loader_cases():
    case("loader_include_chain", "{% include 'a' %}",
         templates={"a": "A{% include 'b' %}", "b": "B{% include 'c' %}", "c": "C"})
    case("loader_extends_include", "{% include 'child' %}",
         templates={"child": "{% extends 'base' %}{% block b %}child{% endblock %}",
                    "base": "[{% block b %}{% endblock %}]"})
    case("loader_template_not_found_msg", "{% include 'missing_template' %}",
         templates={"x": "y"})
    case("loader_select_not_found", "{% include ['m1', 'm2'] %}", templates={"x": "y"})

loader_cases()


# ============ M5: regression (对照 test_regression.py) ============
def regression_cases():
    case("r_keyword_folding", "{{ 'foo'|join_string(suffix='bar') }}" if False else "{{ true }}")
    case("r_empty_if_condition_err", "{% if %}....{% endif %}")
    case("r_recursive_loop_compile", "{% for p in foo recursive %}{{ p.bar }}{% for f in p.fields recursive %}{{ f.baz }}{% endfor %}{% endfor %}",
         ctx={"foo": []})
    case("r_else_loop_bug", "{% for x in [1] %}{{ loop.index0 }}{% else %}NONE{% endfor %}")
    case("r_correct_prefix_loader_name", "{% include 'p/x' %}", templates={})
    case("r_partial_conditional_assignments", "{% if b %}{% set a = 42 %}{% endif %}{{ a }}", ctx={"a": 23, "b": False})
    case("r_partial_conditional_assignments2", "{% if b %}{% set a = 42 %}{% endif %}{{ a }}", ctx={"a": 23, "b": True})
    case("r_stacked_locals_scoping", "{% for j in [1, 2] %}{% set x = 1 %}{% for i in [1, 2] %}{{ x }}{% if i % 2 == 0 %}{% set x = 2 %}{% endif %}{% endfor %}{% endfor %}{% if a %}{{ 1 }}{% elif b %}{{ 2 }}{% else %}{{ 3 }}{% endif %}",
         ctx={"a": False, "b": False})
    case("r_double_caller", "{% macro x() %}{{ caller() }}{% endmacro %}{% call x() %}aha!{% endcall %}")
    case("r_variable_reuse", "{% for x in x.y %}{{ x }}{% endfor %}", ctx={"x": {"y": [0, 1, 2]}})
    case("r_variable_reuse2", "{% for x in x.y %}{{ x }}{% endfor %}{% for x in x.x %}{{ x }}{% endfor %}",
         ctx={"x": {"y": [0, 1, 2], "x": [3, 4, 5]}})
    case("r_bug_with_temporary_variables", "{% for i, j in [(1, 2)] %}{{ i }}{{ j }}{% endfor %}")
    case("r_call_with_args_macro", """{% macro dump_users(users) -%}
    <ul>
      {%- for user in users -%}
        <li><p>{{ user.username|e }}</p>{{ caller(user) }}</li>
      {%- endfor -%}
      </ul>
    {%- endmacro -%}

    {% call(user) dump_users(list_of_user) -%}
      <dl>
        <dl>Realname</dl>
        <dd>{{ user.realname|e }}</dd>
        <dl>Description</dl>
        <dd>{{ user.description }}</dd>
      </dl>
    {%- endcall %}""",
         ctx={"list_of_user": [{"username": "apo", "realname": "something else", "description": "test"}]})
    case("r_empty_if", "{% if foo %}{% else %}bar{% endif %}", ctx={"foo": False})
    case("r_subproperty_if", "{% if object1.subproperty1 is eq object2.subproperty2 %}42{% endif %}",
         ctx={"object1": {"subproperty1": "value"}, "object2": {"subproperty2": "value"}})
    case("r_set_and_include", "{% set x = 1 %}{% include 'inner' %}", templates={"inner": "x is {{ x }}"})
    case("r_loop_include", "{% for i in [1, 2, 3] %}{% include 'inner' %}{% endfor %}", templates={"inner": "{{ i }}"})
    case("r_grouper_repr", "{{ users|groupby('foo')|first }}" if False else "{{ [('a', [1])]|first }}")
    case("r_scoped_block", "{% extends 'parent' %}{% block item scoped %}{{ item }}{% endblock %}",
         templates={"parent": "{% for item in foo %}[{% block item scoped %}{% endblock %}]{% endfor %}"},
         ctx={"foo": [1, 2, 3]})
    case("r_scoped_block_in_include", "{% include 'helper' %}",
         templates={"helper": "{% for item in [1, 2] %}{% block item scoped %}{{ item }}{% endblock %}{% endfor %}"})
    case("r_caller_scoping", "{% macro detail(icon) %}{{ caller() }}{{ icon }}{% endmacro %}{% call detail('x') %}[{{ icon|default('no') }}]{% endcall %}")
    case("r_block_set_with_extends", "{% extends 'main' %}{% set body = 'x' %}",
         templates={"main": "{{ body }}"})
    case("r_nested_for_else", "{% for x in [] %}{{ x }}{% else %}{% for i in range(3) %}{{ i }}{% endfor %}{% endfor %}")
    case("r_macro_var_unpacking", "{% macro m() %}{% for x, y in [(1, 2)] %}{{ x }}{{ y }}{% endfor %}{% endmacro %}{{ m() }}")
    case("r_nested_loop_scoping", "{% for i in [1] %}{% for j in [2] %}{{ i }}{{ j }}{% endfor %}{% endfor %}")
    case("r_weird_inline_comment_err", "{% for item in seq {# missing #}%}...{% endfor %}")
    case("r_markup_and_chainable_undefined", "{{ '<br>'|e + missing.x|default('') }}", undefined="chainable", autoescape=True)
    case("r_scoping_bug_macro_module", "{% macro foo() %}bar{% endmacro %}{% set x = foo() %}{{ x }}")
    case("r_macro_escaping", "{% macro m() %}<b>{% endmacro %}{{ m() }}|{{ m()|e }}", autoescape=True)
    case("r_recursive_loop_filter", "{% for p in [{'b': true, 'c': [{'b': false, 'c': []}, {'b': true, 'c': []}]}] recursive if p.b %}+{{ loop(p.c) }}{% endfor %}")
    case("r_empty_set_block", "{% set x %}{% endset %}[{{ x }}]")
    case("r_cond_expr_undefined", "{{ ('foo' if false)|default('bar') }}")
    case("r_nested_cond", "{{ 'a' if (true if x else false) else 'b' }}", ctx={"x": 1})
    case("r_unicode_whitespace", "你好\u2028世界")

regression_cases()


if __name__ == "__main__":
    main()

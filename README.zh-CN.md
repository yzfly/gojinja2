<div align="center">

# ⛩️ gojinja2

**以 100% 协议兼容为目标的 Go 语言 Jinja2 模板引擎——与 CPython 逐字符对齐验证。**

[![CI](https://github.com/yzfly/gojinja2/actions/workflows/ci.yml/badge.svg)](https://github.com/yzfly/gojinja2/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/yzfly/gojinja2.svg)](https://pkg.go.dev/github.com/yzfly/gojinja2)
[![Go Version](https://img.shields.io/badge/go-%3E%3D1.22-00ADD8?logo=go)](https://go.dev)
[![Conformance](https://img.shields.io/badge/CPython%20conformance-557%2F557-brightgreen)](#验收方法conformance)
[![License: CC BY-NC 4.0](https://img.shields.io/badge/license-CC%20BY--NC%204.0-lightgrey)](LICENSE)

[English](README.md) | 简体中文

</div>

---

现有的 Go 移植（pongo2、gonja 等）都止步于"模板解析器"。但 Jinja2 不是一种语法——它是**寄生在 Python 运行时上的语言**。`{{ -7 // 2 }}`、`{{ 1 == True }}`、`{{ "日本語"[1:] }}`、`{{ d.items() }}`：把这些做对，意味着要在 Go 里重新实现一小片 Python 的对象模型，而不只是它的模板文法。

**gojinja2 完整实现了这个语义层**，并用唯一有意义的方式证明它：所有测试语料都由官方 CPython Jinja2（pallets/jinja 3.1.6）真实运行生成，Go 引擎必须与之**逐字符一致**——渲染输出、token 序列、AST 形状、甚至错误信息。

## 特性

- **完整表达式文法** —— 链式比较、Python 切片、`*args` / `**kwargs`、三元表达式、filter/test 表达式、隐式元组
- **全部语句** —— `if` / `for`（含 `recursive`、完整 `loop` 对象、循环过滤）/ `set`（含块赋值与 `namespace()`）/ `with` / `macro`（caller、varargs、kwargs、闭包、调用期默认值）/ `call` / `filter` / `autoescape`
- **完整模板继承** —— 多层 `extends`、`block`（`scoped` / `required`）、`super()` 与 `super.super()`、`self`、动态父模板表达式
- **`include` 与 `import`** —— `ignore missing`、列表回退、`with` / `without context`
- **54 个内置 filter、39 个内置 test、全部 globals**（`range`、`dict`、`namespace`、`cycler`、`joiner`、`lipsum`）——包括长尾：`tojson`、`urlize`、`wordwrap`（忠实移植 `textwrap`）、带 `leeway` 的 `truncate`、带大小写还原的 `groupby`
- **Python 数值语义** —— 银行家舍入、地板除与取模的符号规则、`1 == 1.0 == True`、rune 级字符串索引、**保插入序的 dict**（含 Python 键归一语义）
- **4 种 Undefined** —— 默认 / Chainable / Debug / Strict 的完整行为矩阵
- **autoescape 与 `Markup` 传染语义** —— 贯穿 `~`、`+`、`%`、`join`、`replace` 的全部转义路径
- **token 级精确的空白控制** —— `trim_blocks`、`lstrip_blocks`、`{%-`、`{%+`、行语句/行注释、自定义定界符（PHP / ERB 风格）
- **扩展** —— `do`、`loopcontrols`（`break` / `continue`）、`i18n`（`trans` / `pluralize` / `trimmed` / gettext 上下文）
- **Loader** —— `DictLoader`、`FileSystemLoader`、`FSLoader`（支持 `embed.FS`）、`FunctionLoader`、`ChoiceLoader`、`PrefixLoader`

## 快速开始

```bash
go get github.com/yzfly/gojinja2
```

```go
package main

import (
    "embed"
    "fmt"

    gojinja2 "github.com/yzfly/gojinja2"
)

//go:embed templates
var templates embed.FS

func main() {
    // 字符串模板
    env := gojinja2.NewEnvironment()
    tpl, _ := env.FromString("Hello {{ name|title }}! {% for i in range(3) %}{{ i }}{% endfor %}")
    out, _ := tpl.Render(map[string]any{"name": "world"})
    fmt.Println(out) // Hello World! 012

    // 文件模板 + 继承 + 自动转义
    env2 := gojinja2.NewEnvironment()
    env2.Loader = gojinja2.NewFSLoader(templates, "templates")
    env2.Autoescape = true
    page, _ := env2.GetTemplate("child.html")
    html, _ := page.Render(map[string]any{"user": "<script>"})
    fmt.Println(html) // <script> 已被转义
}
```

### Go 值的适配

模板中的属性访问遵循 Python 语义（先属性后下标）：

- `map[string]any` 与嵌套结构 → dict 语义（键访问、`.items()`、`.get()` 等方法可用）
- 结构体字段与方法：精确名优先，`snake_case → CamelCase` 兜底（`{{ user.get_name() }}` 自动调用 `GetName()`）
- 数值统一为 `int64` / `float64`；切片/数组按 list 处理

## 验收方法（conformance）

不手写期望值。`tools/` 下的生成器把每个用例（模板 × 上下文 × 环境配置）喂给官方 CPython Jinja2，记录其真实输出（或异常），Go 测试逐字符对齐：

| 层 | 语料 | 对齐内容 |
|---|---|---|
| Lexer | 114 用例 / 632 token | token 类型、值、行号；错误信息逐字 |
| Parser | 110 用例 | AST 与 Python `repr(ast)` 逐字符一致；18 个错误用例 |
| Render | 333 用例 | 渲染输出逐字符一致；运行时错误信息逐字 |

当前通过率：**557/557**，另有 1 个文档化差异（见下）。

自行重新生成语料（需要本地 CPython 与参考源码）：

```bash
git clone --depth 1 --branch 3.1.6 https://github.com/pallets/jinja.git reference/jinja
PYTHONPATH=reference/jinja/src python3 tools/gen_lexer_fixtures.py  > lexer/testdata/lexer_fixtures.json
PYTHONPATH=reference/jinja/src python3 tools/gen_parser_fixtures.py > parser/testdata/parser_fixtures.json
PYTHONPATH=reference/jinja/src python3 tools/gen_render_fixtures.py > rendertest/testdata/render_fixtures.json
go test ./...
```

这条管线是项目真正的资产：扩充覆盖是机械劳动——加用例，CPython 自动产出期望值，Go 测试强制对齐。

## 已知文档化差异

诚实优先于营销。完整清单：

1. **整数精度** —— Python 是任意精度整数，gojinja2 使用 `int64`（`big.Int` 升级路径已预留）。超出 `int64` 的字面量会报错而非静默截断。
2. **原生 Go map 迭代顺序** —— 用户传入的 Go map 按键排序迭代（Go map 本身无序）；模板内创建的 dict 严格保插入序，与 Python 一致。
3. **声明性 out of scope** —— `sandbox`、`async`、字节码缓存：Python 生态特有机制，不计入兼容性。

## 架构

```
gojinja2/
├── lexer/        # 手写扫描器, 复刻官方正则语义, 惰性错误
├── parser/       # 1:1 移植 parser.py
├── nodes/        # AST 定义, 输出与 Python repr 对齐
├── runtime/      # Python 语义层: 值系统/运算/Undefined/Markup/保序 dict
├── exceptions/   # 错误类型 (对应 jinja2.exceptions)
├── rendertest/   # render 级 conformance 测试
├── tools/        # conformance 语料生成器 (以 CPython 为 ground truth)
└── *.go          # Environment / Template / 解释器 / filters / loaders / 扩展
```

两处与 CPython 实现的有意分歧，对模板均不可观测：

- Jinja2 把模板编译为 Python 源码；gojinja2 使用 AST 树遍历解释器（Go 没有 `exec`，conformance 套件证明了行为等价）。
- 官方 lexer 基于带 lookbehind/lookahead 的正则，Go 的 RE2 无法表达；扫描器为手写实现，逐条复刻原正则语义——包括惰性错误顺序。

## 参与贡献

见 [CONTRIBUTING.md](CONTRIBUTING.md)。黄金法则：任何行为主张都必须附带 CPython 生成的语料。

## 许可

[CC BY-NC 4.0](LICENSE)（非商用）。商业授权请联系作者。

## 作者

**云中江树 (yzfly)** —— 微信公众号：云中江树

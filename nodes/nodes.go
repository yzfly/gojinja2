// Package nodes 定义模板 AST, 移植自 jinja2/nodes.py (3.1.6).
//
// 与 Python 的差异: 二元/一元运算的各子类 (Add/Sub/...) 合并为带
// Op 字段的 BinExpr/UnaryExpr; repr.go 中的 PyRepr 仍按原类名输出.
package nodes

// Node 是所有 AST 节点的公共接口.
type Node interface {
	Lineno() int
	nodeTag()
}

// Expr 是表达式节点.
type Expr interface {
	Node
	exprTag()
}

type base struct{ Line int }

func (b base) Lineno() int { return b.Line }
func (b base) nodeTag()    {}

type exprBase struct{ base }

func (exprBase) exprTag() {}

// NewBase 供 parser 构造节点时设置行号.
func NewBase(line int) base { return base{Line: line} }

// ---- 模板与语句 ----

type Template struct {
	base
	Body []Node
}

type Output struct {
	base
	Nodes []Expr
}

type Extends struct {
	base
	Template Expr
}

type For struct {
	base
	Target    Expr
	Iter      Expr
	Body      []Node
	Else      []Node
	Test      Expr // 可为 nil
	Recursive bool
}

type If struct {
	base
	Test Expr
	Body []Node
	Elif []*If
	Else []Node
}

type Macro struct {
	base
	Name     string
	Args     []*Name
	Defaults []Expr
	Body     []Node
}

type CallBlock struct {
	base
	Call     *Call
	Args     []*Name
	Defaults []Expr
	Body     []Node
}

type FilterBlock struct {
	base
	Body   []Node
	Filter *Filter
}

type With struct {
	base
	Targets []Expr
	Values  []Expr
	Body    []Node
}

type Block struct {
	base
	Name     string
	Body     []Node
	Scoped   bool
	Required bool
}

type Include struct {
	base
	Template      Expr
	WithContext   bool
	IgnoreMissing bool
}

type Import struct {
	base
	Template    Expr
	Target      string
	WithContext bool
}

// ImportName 是 from-import 的一个名字; Alias 为空表示无别名.
type ImportName struct {
	Name  string
	Alias string
}

type FromImport struct {
	base
	Template    Expr
	Names       []ImportName
	WithContext bool
}

type ExprStmt struct {
	base
	Node Expr
}

type Assign struct {
	base
	Target Expr
	Node   Expr
}

type AssignBlock struct {
	base
	Target Expr
	Filter *Filter // 可为 nil
	Body   []Node
}

// Scope 是人工作用域 (autoescape 块等).
type Scope struct {
	base
	Body []Node
}

// ScopedEvalContextModifier 在子作用域里修改 EvalContext (autoescape).
type ScopedEvalContextModifier struct {
	base
	Options []*Keyword
	Body    []Node
}

// Break / Continue 来自 loopcontrols 扩展.
type Break struct{ base }
type Continue struct{ base }

// ---- 表达式 ----

// Name 上下文: "load" 读取, "store" 赋值, "param" 函数参数.
type Name struct {
	exprBase
	Name string
	Ctx  string
}

// NSRef 是 namespace 属性赋值引用 (ns.attr = ...).
type NSRef struct {
	exprBase
	Name string
	Attr string
}

// Const 常量. Value 为 int64 / float64 / string / bool / nil / []any(元组).
type Const struct {
	exprBase
	Value any
}

type TemplateData struct {
	exprBase
	Data string
}

type Tuple struct {
	exprBase
	Items []Expr
	Ctx   string
}

type List struct {
	exprBase
	Items []Expr
}

type Dict struct {
	exprBase
	Items []*Pair
}

type Pair struct {
	base
	Key   Expr
	Value Expr
}

func (*Pair) exprTag() {} // Helper, 但便于遍历

type Keyword struct {
	base
	Key   string
	Value Expr
}

type CondExpr struct {
	exprBase
	Test  Expr
	Expr1 Expr
	Expr2 Expr // 可为 nil
}

type Filter struct {
	exprBase
	Node      Expr // filter block 中可为 nil
	Name      string
	Args      []Expr
	Kwargs    []*Keyword
	DynArgs   Expr // 可为 nil
	DynKwargs Expr // 可为 nil
}

type Test struct {
	exprBase
	Node      Expr
	Name      string
	Args      []Expr
	Kwargs    []*Keyword
	DynArgs   Expr
	DynKwargs Expr
}

type Call struct {
	exprBase
	Node      Expr
	Args      []Expr
	Kwargs    []*Keyword
	DynArgs   Expr
	DynKwargs Expr
}

type Getitem struct {
	exprBase
	Node Expr
	Arg  Expr
	Ctx  string
}

type Getattr struct {
	exprBase
	Node Expr
	Attr string
	Ctx  string
}

type Slice struct {
	exprBase
	Start Expr // 均可为 nil
	Stop  Expr
	Step  Expr
}

type Concat struct {
	exprBase
	Nodes []Expr
}

type Compare struct {
	exprBase
	Expr Expr
	Ops  []*Operand
}

// Operand 的 Op: eq ne lt lteq gt gteq in notin.
type Operand struct {
	base
	Op   string
	Expr Expr
}

// BinExpr 的 Op: "+" "-" "*" "/" "//" "%" "**" "and" "or".
type BinExpr struct {
	exprBase
	Op    string
	Left  Expr
	Right Expr
}

// UnaryExpr 的 Op: "not" "-" "+".
type UnaryExpr struct {
	exprBase
	Op   string
	Node Expr
}

// MarkSafe 把表达式标记为安全 (Markup), 供扩展使用.
type MarkSafe struct {
	exprBase
	Expr Expr
}

// MarkSafeIfAutoescape 仅在 autoescape 开启时标记安全.
type MarkSafeIfAutoescape struct {
	exprBase
	Expr Expr
}

// InternalName 是编译器内部名 (扩展用).
type InternalName struct {
	exprBase
	Name string
}

// ContextReference / DerivedContextReference 供扩展引用上下文.
type ContextReference struct{ exprBase }
type DerivedContextReference struct{ exprBase }

// ---- ctx / 赋值检查 ----

// SetCtx 递归设置节点及子节点的 ctx 字段, 对应 Node.set_ctx.
func SetCtx(n Node, ctx string) {
	switch v := n.(type) {
	case *Name:
		v.Ctx = ctx
	case *Tuple:
		v.Ctx = ctx
		for _, item := range v.Items {
			SetCtx(item, ctx)
		}
	case *Getattr:
		v.Ctx = ctx
		SetCtx(v.Node, ctx)
	case *Getitem:
		v.Ctx = ctx
		SetCtx(v.Node, ctx)
		SetCtx(v.Arg, ctx)
	case *List:
		for _, item := range v.Items {
			SetCtx(item, ctx)
		}
	case *Const, *NSRef, *TemplateData:
		// 无 ctx 字段或无子节点
	}
}

// CanAssign 对应 Expr.can_assign.
func CanAssign(n Node) bool {
	switch v := n.(type) {
	case *Name:
		switch v.Name {
		case "true", "false", "none", "True", "False", "None":
			return false
		}
		return true
	case *NSRef:
		return true
	case *Tuple:
		for _, item := range v.Items {
			if !CanAssign(item) {
				return false
			}
		}
		return true
	}
	return false
}

// TypeName 返回与 Python 类名一致的节点类型名 (用于错误信息与 repr).
func TypeName(n Node) string {
	switch v := n.(type) {
	case *Template:
		return "Template"
	case *Output:
		return "Output"
	case *Extends:
		return "Extends"
	case *For:
		return "For"
	case *If:
		return "If"
	case *Macro:
		return "Macro"
	case *CallBlock:
		return "CallBlock"
	case *FilterBlock:
		return "FilterBlock"
	case *With:
		return "With"
	case *Block:
		return "Block"
	case *Include:
		return "Include"
	case *Import:
		return "Import"
	case *FromImport:
		return "FromImport"
	case *ExprStmt:
		return "ExprStmt"
	case *Assign:
		return "Assign"
	case *AssignBlock:
		return "AssignBlock"
	case *Scope:
		return "Scope"
	case *ScopedEvalContextModifier:
		return "ScopedEvalContextModifier"
	case *Break:
		return "Break"
	case *Continue:
		return "Continue"
	case *Name:
		return "Name"
	case *NSRef:
		return "NSRef"
	case *Const:
		return "Const"
	case *TemplateData:
		return "TemplateData"
	case *Tuple:
		return "Tuple"
	case *List:
		return "List"
	case *Dict:
		return "Dict"
	case *Pair:
		return "Pair"
	case *Keyword:
		return "Keyword"
	case *CondExpr:
		return "CondExpr"
	case *Filter:
		return "Filter"
	case *Test:
		return "Test"
	case *Call:
		return "Call"
	case *Getitem:
		return "Getitem"
	case *Getattr:
		return "Getattr"
	case *Slice:
		return "Slice"
	case *Concat:
		return "Concat"
	case *Compare:
		return "Compare"
	case *Operand:
		return "Operand"
	case *MarkSafe:
		return "MarkSafe"
	case *MarkSafeIfAutoescape:
		return "MarkSafeIfAutoescape"
	case *InternalName:
		return "InternalName"
	case *ContextReference:
		return "ContextReference"
	case *DerivedContextReference:
		return "DerivedContextReference"
	case *BinExpr:
		return binOpNames[v.Op]
	case *UnaryExpr:
		return unaryOpNames[v.Op]
	}
	return "Node"
}

var binOpNames = map[string]string{
	"+": "Add", "-": "Sub", "*": "Mul", "/": "Div", "//": "FloorDiv",
	"%": "Mod", "**": "Pow", "and": "And", "or": "Or",
}

var unaryOpNames = map[string]string{"not": "Not", "-": "Neg", "+": "Pos"}

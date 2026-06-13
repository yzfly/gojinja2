package nodes

// Children 返回节点的直接子节点, 对应 iter_child_nodes.
func Children(n Node) []Node {
	var out []Node
	add := func(ns ...Node) {
		for _, c := range ns {
			if !IsNil(c) {
				out = append(out, c)
			}
		}
	}
	addE := func(es ...Expr) {
		for _, e := range es {
			if !IsNil(e) {
				out = append(out, e)
			}
		}
	}
	addNodes := func(ns []Node) {
		for _, c := range ns {
			add(c)
		}
	}
	addExprs := func(es []Expr) {
		for _, e := range es {
			addE(e)
		}
	}
	switch v := n.(type) {
	case *Template:
		addNodes(v.Body)
	case *Output:
		addExprs(v.Nodes)
	case *Extends:
		addE(v.Template)
	case *For:
		addE(v.Target, v.Iter)
		addNodes(v.Body)
		addNodes(v.Else)
		addE(v.Test)
	case *If:
		addE(v.Test)
		addNodes(v.Body)
		for _, e := range v.Elif {
			add(e)
		}
		addNodes(v.Else)
	case *Macro:
		for _, a := range v.Args {
			add(a)
		}
		addExprs(v.Defaults)
		addNodes(v.Body)
	case *CallBlock:
		add(v.Call)
		for _, a := range v.Args {
			add(a)
		}
		addExprs(v.Defaults)
		addNodes(v.Body)
	case *FilterBlock:
		addNodes(v.Body)
		add(v.Filter)
	case *With:
		addExprs(v.Targets)
		addExprs(v.Values)
		addNodes(v.Body)
	case *Block:
		addNodes(v.Body)
	case *Include:
		addE(v.Template)
	case *Import:
		addE(v.Template)
	case *FromImport:
		addE(v.Template)
	case *ExprStmt:
		addE(v.Node)
	case *Assign:
		addE(v.Target, v.Node)
	case *AssignBlock:
		addE(v.Target)
		add(v.Filter)
		addNodes(v.Body)
	case *Scope:
		addNodes(v.Body)
	case *ScopedEvalContextModifier:
		for _, k := range v.Options {
			add(k)
		}
		addNodes(v.Body)
	case *Tuple:
		addExprs(v.Items)
	case *List:
		addExprs(v.Items)
	case *Dict:
		for _, p := range v.Items {
			add(p)
		}
	case *Pair:
		addE(v.Key, v.Value)
	case *Keyword:
		addE(v.Value)
	case *CondExpr:
		addE(v.Test, v.Expr1, v.Expr2)
	case *Filter:
		addE(v.Node)
		addExprs(v.Args)
		for _, k := range v.Kwargs {
			add(k)
		}
		addE(v.DynArgs, v.DynKwargs)
	case *Test:
		addE(v.Node)
		addExprs(v.Args)
		for _, k := range v.Kwargs {
			add(k)
		}
		addE(v.DynArgs, v.DynKwargs)
	case *Call:
		addE(v.Node)
		addExprs(v.Args)
		for _, k := range v.Kwargs {
			add(k)
		}
		addE(v.DynArgs, v.DynKwargs)
	case *Getitem:
		addE(v.Node, v.Arg)
	case *Getattr:
		addE(v.Node)
	case *Slice:
		addE(v.Start, v.Stop, v.Step)
	case *Concat:
		addExprs(v.Nodes)
	case *Compare:
		addE(v.Expr)
		for _, o := range v.Ops {
			add(o)
		}
	case *Operand:
		addE(v.Expr)
	case *BinExpr:
		addE(v.Left, v.Right)
	case *UnaryExpr:
		addE(v.Node)
	case *MarkSafe:
		addE(v.Expr)
	case *MarkSafeIfAutoescape:
		addE(v.Expr)
	}
	return out
}

// Walk 深度优先遍历; fn 返回 false 时不进入该节点的子树.
func Walk(n Node, fn func(Node) bool) {
	if IsNil(n) || !fn(n) {
		return
	}
	for _, c := range Children(n) {
		Walk(c, fn)
	}
}

// FindAll 收集满足谓词的所有节点.
func FindAll(n Node, pred func(Node) bool) []Node {
	var out []Node
	Walk(n, func(c Node) bool {
		if pred(c) {
			out = append(out, c)
		}
		return true
	})
	return out
}

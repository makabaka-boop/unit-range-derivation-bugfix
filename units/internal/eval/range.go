package eval

// 区间求值：对每个语法节点在基准单位下计算一个闭区间 [Lo, Hi]，
// 而不是“所有输入各取下端点 / 各取上端点”各算一遍——后者对减法、
// 含负值的乘除都会给出倒置或缺失的上下界。
//
// 区间运算规则（输入区间均为闭区间）：
//
//	-[a,b]        = [-b,-a]
//	[a,b]+[c,d]   = [a+c,b+d]
//	[a,b]-[c,d]   = [a-d,b-c]
//	[a,b]·[c,d]   = min/max{a c, a d, b c, b d}
//	[a,b]/[c,d]   = [a,b]·[1/d,1/c]，要求 [c,d] 不含 0；
//	               [c,d] 含 0（包括整段为 0）一律报错。
//
// 种类与维度规则与标量求值完全一致（复用 checkAddSub /
// resultKindMulDiv），因此每一步的种类、维度、错误码、错误区间都相同。
//
// 所有受支持单位的目标换算（含 °C/°F）在有理数上都是严格单调增的
// 仿射变换，所以基准区间的下端点换算后仍是下端点，无需再排序。

import (
	"fmt"
	"math/big"

	"units/internal/parse"
	"units/internal/phys"
)

// IVal 是基准单位（m, kg, s, K）表示的闭区间；恒有 Lo <= Hi。
type IVal struct {
	Lo *big.Rat
	Hi *big.Rat
}

func iv(a, b *big.Rat) IVal { return IVal{Lo: a, Hi: b} }

// iq 是一个带种类与维度的区间量。
type iq struct {
	Kind phys.Kind
	Dim  phys.Dim
	V    IVal
}

// RangeStep 是区间求值中一个语法节点的推导结果。
type RangeStep struct {
	NodeType string   `json:"nodeType"`
	Op       string   `json:"op,omitempty"`
	Src      string   `json:"source"`
	Pos      int      `json:"pos"`
	End      int      `json:"end"`
	Depth    int      `json:"depth"`
	Kind     string   `json:"kind"`
	Dim      phys.Dim `json:"dim"`
	DimName  string   `json:"dimName"`
	Lower    RatView  `json:"lowerBase"`
	Upper    RatView  `json:"upperBase"`
	Note     string   `json:"note"`
}

// RangeRootView 是根节点换算到目标单位后的区间。
type RangeRootView struct {
	Src       string   `json:"source"`
	Pos       int      `json:"pos"`
	End       int      `json:"end"`
	Kind      string   `json:"kind"`
	Dim       phys.Dim `json:"dim"`
	DimName   string   `json:"dimName"`
	Target    string   `json:"target"`
	TargetSym string   `json:"targetSymbol"`
	Lower     RatView  `json:"lower"`
	Upper     RatView  `json:"upper"`
	LowerBase RatView  `json:"lowerBase"`
	UpperBase RatView  `json:"upperBase"`
}

// RangeResult 是整棵树的区间求值结果。
type RangeResult struct {
	Steps []RangeStep   `json:"steps"`
	Root  RangeRootView `json:"root"`
}

type rangeEvaluator struct {
	steps []RangeStep
}

// EvalRange 对 AST 做区间求值；target 为空时自动选择目标单位。
// 标量字面量视为退化为一点的区间 [v,v]，因此不含区间字面量的表达式
// 与 /api/eval 给出同样的种类、维度、基准值与错误定位。
func EvalRange(a *parse.AST, target string) (*RangeResult, error) {
	ev := &rangeEvaluator{}
	q, err := ev.evalNode(a.Root)
	if err != nil {
		return nil, err
	}
	root, err := ev.convertRangeTarget(a.Root, q, target)
	if err != nil {
		return nil, err
	}
	return &RangeResult{Steps: ev.steps, Root: root}, nil
}

func (ev *rangeEvaluator) emit(n parse.Node, nt, op string, q iq, note string) {
	ev.steps = append(ev.steps, RangeStep{
		NodeType: nt, Op: op, Src: n.Src(), Pos: n.Pos(), End: n.End(),
		Depth: depthOf(n), Kind: q.Kind.JSON(), Dim: q.Dim, DimName: q.Dim.String(),
		Lower: view(q.V.Lo), Upper: view(q.V.Hi), Note: note,
	})
}

func (ev *rangeEvaluator) evalNode(n parse.Node) (iq, error) {
	switch x := n.(type) {
	case *parse.NumberNode:
		return ev.evalNumber(x)
	case *parse.UnaryNode:
		return ev.evalUnary(x)
	case *parse.GroupNode:
		return ev.evalGroup(x)
	case *parse.BinaryNode:
		return ev.evalBinary(x)
	}
	return iq{}, Error{Code: "INTERNAL", Message: "未知语法节点", Pos: n.Pos(), End: n.End()}
}

func (ev *rangeEvaluator) evalNumber(n *parse.NumberNode) (iq, error) {
	lo := new(big.Rat).Set(n.Value)
	hi := lo
	isRange := false
	if n.Upper != nil {
		hi = new(big.Rat).Set(n.Upper)
		isRange = true
	}
	if n.Unit == nil {
		q := iq{Kind: phys.Normal, Dim: phys.Dim{}, V: iv(lo, hi)}
		if isRange {
			ev.emit(n, "literal", "", q, fmt.Sprintf(
				"区间字面量 [%s, %s]（无量纲），两个端点都纳入推导",
				ratExact(lo), ratExact(hi)))
		} else {
			ev.emit(n, "literal", "", q, fmt.Sprintf("字面量 %s（无量纲）", ratExact(lo)))
		}
		return q, nil
	}

	u := n.Unit
	loBase, hiBase := u.ToBase(lo), u.ToBase(hi)
	q := iq{Kind: u.Kind, Dim: u.Dim, V: iv(loBase, hiBase)}
	baseUnit := []string{"m", "kg", "s", "K"}[baseDimIndex(u.Dim)]
	if u.Dim.IsZero() {
		baseUnit = "1"
	}
	if isRange {
		ev.emit(n, "literal", "", q, fmt.Sprintf(
			"区间字面量 [%s, %s] %s，两端分别换算到基准单位 = [%s, %s] %s",
			ratExact(lo), ratExact(hi), u.Sym,
			ratExact(loBase), ratExact(hiBase), baseUnit))
	} else {
		ev.emit(n, "literal", "", q, fmt.Sprintf(
			"字面量 %s %s，换算到基准单位 = %s %s",
			ratExact(lo), u.Sym, ratExact(loBase), baseUnit))
	}
	return q, nil
}

func (ev *rangeEvaluator) evalUnary(n *parse.UnaryNode) (iq, error) {
	c, err := ev.evalNode(n.Child)
	if err != nil {
		return iq{}, err
	}
	// 取负翻转两个端点：-[a,b] = [-b,-a]
	q := iq{Kind: c.Kind, Dim: c.Dim, V: iv(
		new(big.Rat).Neg(c.V.Hi), new(big.Rat).Neg(c.V.Lo))}
	ev.emit(n, "unary", "-", q, fmt.Sprintf(
		"一元负号翻转区间端点：-[%s, %s] = [%s, %s]，种类与维度不变",
		ratExact(c.V.Lo), ratExact(c.V.Hi), ratExact(q.V.Lo), ratExact(q.V.Hi)))
	return q, nil
}

func (ev *rangeEvaluator) evalGroup(n *parse.GroupNode) (iq, error) {
	c, err := ev.evalNode(n.Inner)
	if err != nil {
		return iq{}, err
	}
	q := iq{Kind: c.Kind, Dim: c.Dim, V: iv(
		new(big.Rat).Set(c.V.Lo), new(big.Rat).Set(c.V.Hi))}
	ev.emit(n, "group", "()", q, "括号只改变结合顺序，区间、种类与维度不变")
	return q, nil
}

func (ev *rangeEvaluator) evalBinary(n *parse.BinaryNode) (iq, error) {
	a, err := ev.evalNode(n.Left)
	if err != nil {
		return iq{}, err
	}
	b, err := ev.evalNode(n.Right)
	if err != nil {
		return iq{}, err
	}
	if n.Op == '*' || n.Op == '/' {
		return ev.evalMulDiv(n, a, b)
	}
	return ev.evalAddSub(n, a, b)
}

func (ev *rangeEvaluator) evalMulDiv(n *parse.BinaryNode, a, b iq) (iq, error) {
	if a.Kind == phys.Absolute || b.Kind == phys.Absolute {
		return iq{}, evalErrf(n, ErrAbsMul,
			"绝对温度不能相乘或相除：绝对温度不是可乘的普通数；请先取温差（绝对-绝对）再运算")
	}
	if n.Op == '/' {
		// 除数区间含零（含整段为零）时，所有可能取值里包含除以零，结果无界，拒绝。
		if b.V.Lo.Sign() <= 0 && b.V.Hi.Sign() >= 0 {
			if b.V.Lo.Sign() == 0 && b.V.Hi.Sign() == 0 {
				return iq{}, evalErrf(n, ErrDivZero, "除数恒为零")
			}
			return iq{}, evalErrf(n, ErrDivSpanZero, fmt.Sprintf(
				"除数区间 [%s, %s] 包含零，商会跨过无穷大，不存在有限的上下界",
				ratExact(b.V.Lo), ratExact(b.V.Hi)))
		}
	}

	var d phys.Dim
	var v IVal
	op := string(n.Op)
	if n.Op == '*' {
		d = a.Dim.Add(b.Dim)
		v = mulInterval(a.V, b.V)
	} else {
		d = a.Dim.Sub(b.Dim)
		v = divInterval(a.V, b.V)
	}
	k := resultKindMulDiv(a.Kind, b.Kind, d)
	q := iq{Kind: k, Dim: d, V: v}
	ev.emit(n, "binary", op, q, fmt.Sprintf(
		"区间%s：[%s, %s] %s [%s, %s] = [%s, %s]（%s）；维度 %s %s %s = %s",
		mulDivName(n.Op), ratExact(a.V.Lo), ratExact(a.V.Hi), op,
		ratExact(b.V.Lo), ratExact(b.V.Hi), ratExact(v.Lo), ratExact(v.Hi),
		cornerRule(n.Op), a.Dim.String(), op, b.Dim.String(), d.String()))
	return q, nil
}

func (ev *rangeEvaluator) evalAddSub(n *parse.BinaryNode, a, b iq) (iq, error) {
	k, d, code, msg := checkAddSub(n.Op, a.Kind, b.Kind, a.Dim, b.Dim)
	if code != "" {
		return iq{}, evalErrf(n, code, msg)
	}
	var v IVal
	if n.Op == '+' {
		// [a,b]+[c,d] = [a+c,b+d]
		v = iv(new(big.Rat).Add(a.V.Lo, b.V.Lo), new(big.Rat).Add(a.V.Hi, b.V.Hi))
	} else {
		// [a,b]-[c,d] = [a-d,b-c]：右端点必须用被减数的下端点，
		// 不能直接用“下界减下界、上界减上界”。
		v = iv(new(big.Rat).Sub(a.V.Lo, b.V.Hi), new(big.Rat).Sub(a.V.Hi, b.V.Lo))
	}
	q := iq{Kind: k, Dim: d, V: v}
	op := string(n.Op)
	ev.emit(n, "binary", op, q, fmt.Sprintf(
		"%s：[%s, %s] %s [%s, %s] = [%s, %s]",
		addSubNote(n.Op, a.Kind, b.Kind, k),
		ratExact(a.V.Lo), ratExact(a.V.Hi), op,
		ratExact(b.V.Lo), ratExact(b.V.Hi), ratExact(v.Lo), ratExact(v.Hi)))
	return q, nil
}

// corners 计算二元有理运算在四个角点上的结果并取最小/最大值。
func corners(a, b IVal, f func(x, y *big.Rat) *big.Rat) IVal {
	c1 := f(a.Lo, b.Lo)
	c2 := f(a.Lo, b.Hi)
	c3 := f(a.Hi, b.Lo)
	c4 := f(a.Hi, b.Hi)
	lo, hi := c1, c1
	for _, c := range []*big.Rat{c2, c3, c4} {
		if c.Cmp(lo) < 0 {
			lo = c
		}
		if c.Cmp(hi) > 0 {
			hi = c
		}
	}
	return iv(new(big.Rat).Set(lo), new(big.Rat).Set(hi))
}

func mulInterval(a, b IVal) IVal {
	return corners(a, b, func(x, y *big.Rat) *big.Rat {
		return new(big.Rat).Mul(x, y)
	})
}

func divInterval(a, b IVal) IVal {
	// 调用方已保证 b 不含零。
	return corners(a, b, func(x, y *big.Rat) *big.Rat {
		return new(big.Rat).Quo(x, y)
	})
}

func (ev *rangeEvaluator) convertRangeTarget(n parse.Node, q iq, target string) (RangeRootView, error) {
	rv := RangeRootView{
		Src: n.Src(), Pos: n.Pos(), End: n.End(),
		Kind: q.Kind.JSON(), Dim: q.Dim, DimName: q.Dim.String(),
		LowerBase: view(q.V.Lo), UpperBase: view(q.V.Hi),
	}
	scalarQ := Q{Kind: q.Kind, Dim: q.Dim, Value: q.V.Lo}
	name, sym, u, err := resolveTarget(n, scalarQ, target)
	if err != nil {
		return rv, err
	}
	rv.Target, rv.TargetSym = name, sym
	// 目标单位换算是严格单调增仿射变换，下端点换算后仍是下端点。
	rv.Lower = view(u.FromBase(q.V.Lo))
	rv.Upper = view(u.FromBase(q.V.Hi))
	return rv, nil
}

func cornerRule(op byte) string {
	if op == '*' {
		return "取四个角点乘积的最小、最大值"
	}
	return "除数不含零，取四个角点商的最小、最大值"
}

func mulDivName(op byte) string {
	if op == '*' {
		return "乘法"
	}
	return "除法"
}

func kindName(k phys.Kind) string {
	switch k {
	case phys.Delta:
		return "温差"
	case phys.Absolute:
		return "绝对温度"
	default:
		return "普通量"
	}
}

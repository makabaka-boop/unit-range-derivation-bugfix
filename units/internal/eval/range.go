// 区间求值：每个字面量 [下界,上界] 都是一个闭区间，所有运算按区间算术
// 在基准单位（m、kg、s、K）上逐节点传播：
//
//	[a,b] + [c,d] = [a+c, b+d]
//	[a,b] - [c,d] = [a-d, b-c]
//	[a,b] * [c,d] = 四个端点乘积的最小值/最大值
//	[a,b] / [c,d] = 四个端点商的最小值/最大值（除数区间不得包含 0）
//
// 单位换算的系数恒为正数（含摄氏/华氏平移），所以区间端点的次序在
// 换算前后保持一致；一元负号会交换上下界。
// 种类（普通量/温差/绝对温度）与维度规则与标量求值 eval.go 完全一致，
// 非法运算同样定位到最小子表达式区间。
package eval

import (
	"fmt"
	"math/big"

	"units/internal/parse"
	"units/internal/phys"
)

// 除数区间包含 0（含跨零与退化为 [0,0]）时，区间除法在 0 附近无界。
const ErrDivZeroRange = "DIVISION_BY_ZERO_RANGE"

// iq 是一个带种类与维度的闭区间物理量，lo/hi 始终以基准单位表示且 lo <= hi。
type iq struct {
	kind phys.Kind
	dim  phys.Dim
	lo   *big.Rat
	hi   *big.Rat
}

// RangeStep 是区间模式下一个语法节点的推导结果（基准值区间）。
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

// RangeRootView 是根节点区间换算到目标单位后的上下界。
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

// EvalRange 对 AST 做区间求值；target 为空字符串时自动选择目标单位。
// 标量字面量视为退化为单点的区间，因此本入口也接受不含区间字面量的表达式。
func EvalRange(a *parse.AST, target string) (*RangeResult, error) {
	ev := &rangeEvaluator{}
	q, err := ev.evalNode(a.Root)
	if err != nil {
		return nil, err
	}
	root, err := convertRangeTarget(a.Root, q, target)
	if err != nil {
		return nil, err
	}
	return &RangeResult{Steps: ev.steps, Root: root}, nil
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

func (ev *rangeEvaluator) emit(n parse.Node, nt, op string, q iq, note string) {
	ev.steps = append(ev.steps, RangeStep{
		NodeType: nt, Op: op, Src: n.Src(), Pos: n.Pos(), End: n.End(),
		Depth: depthOf(n), Kind: q.kind.JSON(), Dim: q.dim, DimName: q.dim.String(),
		Lower: view(q.lo), Upper: view(q.hi), Note: note,
	})
}

func (ev *rangeEvaluator) evalNumber(n *parse.NumberNode) (iq, error) {
	lo := new(big.Rat).Set(n.Value)
	isRange := n.Upper != nil
	hi := lo
	if isRange {
		hi = new(big.Rat).Set(n.Upper)
	}
	if n.Unit == nil {
		q := iq{kind: phys.Normal, dim: phys.Dim{}, lo: lo, hi: hi}
		note := fmt.Sprintf("字面量 %s（无量纲）", ratExact(lo))
		if isRange {
			note = fmt.Sprintf("区间字面量 [%s, %s]（无量纲），下界 = %s，上界 = %s",
				ratExact(n.Value), ratExact(n.Upper), ratExact(lo), ratExact(hi))
		}
		ev.emit(n, "literal", "", q, note)
		return q, nil
	}
	u := n.Unit
	lo = u.ToBase(lo)
	hi = u.ToBase(hi)
	if lo.Cmp(hi) > 0 {
		lo, hi = hi, lo
	}
	q := iq{kind: u.Kind, dim: u.Dim, lo: lo, hi: hi}
	baseUnit := []string{"m", "kg", "s", "K"}[baseDimIndex(u.Dim)]
	if u.Dim.IsZero() {
		baseUnit = "1"
	}
	note := fmt.Sprintf("字面量 %s %s，换算到基准单位 = %s %s",
		ratExact(n.Value), u.Sym, ratExact(lo), baseUnit)
	if isRange {
		note = fmt.Sprintf("区间字面量 [%s, %s] %s，分别换算到基准单位：下界 = %s %s，上界 = %s %s",
			ratExact(n.Value), ratExact(n.Upper), u.Sym,
			ratExact(lo), baseUnit, ratExact(hi), baseUnit)
	}
	ev.emit(n, "literal", "", q, note)
	return q, nil
}

func (ev *rangeEvaluator) evalUnary(n *parse.UnaryNode) (iq, error) {
	c, err := ev.evalNode(n.Child)
	if err != nil {
		return iq{}, err
	}
	// 负号使区间取反：-[a,b] = [-b,-a]，上下界交换，种类与维度不变。
	q := iq{kind: c.kind, dim: c.dim,
		lo: new(big.Rat).Neg(c.hi), hi: new(big.Rat).Neg(c.lo)}
	ev.emit(n, "unary", "-", q, fmt.Sprintf(
		"一元负号：-[%s, %s] = [%s, %s]（上下界交换），种类与维度不变",
		ratExact(c.lo), ratExact(c.hi), ratExact(q.lo), ratExact(q.hi)))
	return q, nil
}

func (ev *rangeEvaluator) evalGroup(n *parse.GroupNode) (iq, error) {
	c, err := ev.evalNode(n.Inner)
	if err != nil {
		return iq{}, err
	}
	q := iq{kind: c.kind, dim: c.dim,
		lo: new(big.Rat).Set(c.lo), hi: new(big.Rat).Set(c.hi)}
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
	if a.kind == phys.Absolute || b.kind == phys.Absolute {
		return iq{}, evalErrf(n, ErrAbsMul,
			"绝对温度不能相乘或相除：绝对温度不是可乘的普通数；请先取温差（绝对-绝对）再运算")
	}
	op := string(n.Op)
	var d phys.Dim
	var lo, hi *big.Rat
	if n.Op == '*' {
		d = a.dim.Add(b.dim)
		lo, hi = extrema(a.lo, a.hi, b.lo, b.hi, true)
	} else {
		// 除数区间包含 0 时，商在 0 附近无界（区间跨零或退化为 [0,0]）。
		if b.lo.Sign() <= 0 && b.hi.Sign() >= 0 {
			return iq{}, evalErrf(n, ErrDivZeroRange,
				"除数区间 [%s, %s] 包含零：区间除法在零附近无界，结果不能表示为有限闭区间",
				ratExact(b.lo), ratExact(b.hi))
		}
		d = a.dim.Sub(b.dim)
		lo, hi = extrema(a.lo, a.hi, b.lo, b.hi, false)
	}
	k := phys.Normal
	// 仅当结果温度维度恰好为 1 且来源含温差时，结果仍是温差。
	if d.Q == 1 && (a.kind == phys.Delta || b.kind == phys.Delta) {
		k = phys.Delta
	}
	q := iq{kind: k, dim: d, lo: lo, hi: hi}
	if n.Op == '*' {
		ev.emit(n, "binary", op, q, fmt.Sprintf(
			"区间乘法：枚举四个端点组合取最值，%s × %s = [%s, %s]；维度 %s * %s = %s",
			iv(a), iv(b), ratExact(lo), ratExact(hi), a.dim.String(), b.dim.String(), d.String()))
	} else {
		ev.emit(n, "binary", op, q, fmt.Sprintf(
			"区间除法：除数区间不含零，枚举四个端点商取最值，%s ÷ %s = [%s, %s]；维度 %s / %s = %s",
			iv(a), iv(b), ratExact(lo), ratExact(hi), a.dim.String(), b.dim.String(), d.String()))
	}
	return q, nil
}

func (ev *rangeEvaluator) evalAddSub(n *parse.BinaryNode, a, b iq) (iq, error) {
	op := string(n.Op)
	dimEq := a.dim.Equal(b.dim)

	// 绝对 ± 绝对
	if a.kind == phys.Absolute && b.kind == phys.Absolute {
		if n.Op == '-' {
			lo := new(big.Rat).Sub(a.lo, b.hi)
			hi := new(big.Rat).Sub(a.hi, b.lo)
			q := iq{kind: phys.Delta, dim: a.dim, lo: lo, hi: hi}
			ev.emit(n, "binary", op, q, fmt.Sprintf(
				"绝对温度 - 绝对温度 = 温差：[%s,%s] - [%s,%s] = [%s, %s]（K 基准值，交叉端点相减）",
				ratExact(a.lo), ratExact(a.hi), ratExact(b.lo), ratExact(b.hi),
				ratExact(lo), ratExact(hi)))
			return q, nil
		}
		return iq{}, evalErrf(n, ErrAbsAdd,
			"两个绝对温度不能相加：绝对温度的零点不是数量零点，相加没有物理意义")
	}

	// 至少一侧是绝对温度
	if a.kind == phys.Absolute || b.kind == phys.Absolute {
		if !dimEq {
			return iq{}, evalErrf(n, ErrDimMismatch,
				"维度不相容：%s（%s）与 %s（%s）不能相%s",
				a.dim.String(), a.kind.String(), b.dim.String(), b.kind.String(), opName(n.Op))
		}
		if a.kind == phys.Absolute {
			// 绝对 ± 温差 => 绝对；绝对 ± 普通量 => 种类不相容
			if b.kind != phys.Delta {
				return iq{}, evalErrf(n, ErrKindIncompat,
					"绝对温度只能与温差相加减，不能与普通量相%s", opName(n.Op))
			}
			var lo, hi *big.Rat
			if n.Op == '+' {
				lo = new(big.Rat).Add(a.lo, b.lo)
				hi = new(big.Rat).Add(a.hi, b.hi)
			} else {
				lo = new(big.Rat).Sub(a.lo, b.hi)
				hi = new(big.Rat).Sub(a.hi, b.lo)
			}
			q := iq{kind: phys.Absolute, dim: a.dim, lo: lo, hi: hi}
			ev.emit(n, "binary", op, q,
				"绝对温度 ± 温差 = 绝对温度：在 K 基准值上平移，同号端点相加/交叉端点相减")
			return q, nil
		}
		// 右侧是绝对：温差 + 绝对 => 绝对；温差 - 绝对非法
		if a.kind != phys.Delta {
			return iq{}, evalErrf(n, ErrKindIncompat,
				"普通量不能与绝对温度相%s", opName(n.Op))
		}
		if n.Op == '+' {
			lo := new(big.Rat).Add(a.lo, b.lo)
			hi := new(big.Rat).Add(a.hi, b.hi)
			q := iq{kind: phys.Absolute, dim: a.dim, lo: lo, hi: hi}
			ev.emit(n, "binary", op, q, "温差 + 绝对温度 = 绝对温度（同号端点相加）")
			return q, nil
		}
		return iq{}, evalErrf(n, ErrDeltaSubAbs,
			"温差减去绝对温度没有意义：只有 绝对-绝对、绝对±温差、温差+绝对 合法")
	}

	// 两侧均非绝对：维度必须相同；温差与同维度量相容，结果含温差即为温差。
	if !dimEq {
		return iq{}, evalErrf(n, ErrDimMismatch,
			"维度不相容：%s 与 %s 不能相%s", a.dim.String(), b.dim.String(), opName(n.Op))
	}
	var lo, hi *big.Rat
	if n.Op == '+' {
		lo = new(big.Rat).Add(a.lo, b.lo)
		hi = new(big.Rat).Add(a.hi, b.hi)
	} else {
		lo = new(big.Rat).Sub(a.lo, b.hi)
		hi = new(big.Rat).Sub(a.hi, b.lo)
	}
	k := phys.Normal
	if a.kind == phys.Delta || b.kind == phys.Delta {
		k = phys.Delta
	}
	q := iq{kind: k, dim: a.dim, lo: lo, hi: hi}
	note := fmt.Sprintf("同维度普通量区间相加减（基准单位相同）：%s %s %s = [%s, %s]",
		iv(a), op, iv(b), ratExact(lo), ratExact(hi))
	if k == phys.Delta {
		note = fmt.Sprintf("温差区间相加减，结果仍为温差（华氏温差已按 5/9 折算为 K）：%s %s %s = [%s, %s]",
			iv(a), op, iv(b), ratExact(lo), ratExact(hi))
	}
	ev.emit(n, "binary", op, q, note)
	return q, nil
}

// iv 格式化基准值区间，供推导说明使用。
func iv(q iq) string {
	if q.lo.Cmp(q.hi) == 0 {
		return ratExact(q.lo)
	}
	return "[" + ratExact(q.lo) + ", " + ratExact(q.hi) + "]"
}

// extrema 返回两个区间做乘（mul=true）或除（mul=false）时
// 四个端点组合结果的最小值与最大值。调用除法前必须先确认除数区间不含零。
func extrema(alo, ahi, blo, bhi *big.Rat, mul bool) (*big.Rat, *big.Rat) {
	calc := func(x, y *big.Rat) *big.Rat {
		if mul {
			return new(big.Rat).Mul(x, y)
		}
		return new(big.Rat).Quo(x, y)
	}
	cands := []*big.Rat{
		calc(alo, blo), calc(alo, bhi), calc(ahi, blo), calc(ahi, bhi),
	}
	lo := new(big.Rat).Set(cands[0])
	hi := new(big.Rat).Set(cands[0])
	for _, c := range cands[1:] {
		if c.Cmp(lo) < 0 {
			lo.Set(c)
		}
		if c.Cmp(hi) > 0 {
			hi.Set(c)
		}
	}
	return lo, hi
}

// convertRangeTarget 把根节点区间换算到目标单位。target 为空时自动选择。
func convertRangeTarget(n parse.Node, q iq, target string) (RangeRootView, error) {
	rv := RangeRootView{
		Src: n.Src(), Pos: n.Pos(), End: n.End(),
		Kind: q.kind.JSON(), Dim: q.dim, DimName: q.dim.String(),
		LowerBase: view(q.lo), UpperBase: view(q.hi),
	}
	if target == "" || target == "auto" {
		name, sym, lo, hi := autoRangeTarget(q)
		rv.Target, rv.TargetSym, rv.Lower, rv.Upper = name, sym, view(lo), view(hi)
		return rv, nil
	}
	u, ok := phys.ByName(target)
	if !ok {
		return rv, Error{Code: ErrUnknownTarget,
			Message: fmt.Sprintf("未知目标单位 %q", target), Pos: n.Pos(), End: n.End()}
	}
	if !u.Dim.Equal(q.dim) {
		return rv, evalErrf(n, ErrTargetDim,
			"目标单位 %s 的维度是 %s，与结果维度 %s 不一致", u.Sym, u.Dim.String(), q.dim.String())
	}
	if u.Kind != q.kind {
		return rv, evalErrf(n, ErrTargetKind,
			"目标单位 %s 属于%s，而结果是%s，不能这样表示", u.Sym, u.Kind.String(), q.kind.String())
	}
	lo, hi := u.FromBase(q.lo), u.FromBase(q.hi)
	if lo.Cmp(hi) > 0 {
		lo, hi = hi, lo
	}
	rv.Target, rv.TargetSym = u.Name, u.Sym
	rv.Lower, rv.Upper = view(lo), view(hi)
	return rv, nil
}

// autoRangeTarget 为结果选择默认目标单位并完成区间换算。
func autoRangeTarget(q iq) (name, sym string, lo, hi *big.Rat) {
	switch q.kind {
	case phys.Absolute:
		u, _ := phys.ByName("K")
		return "K", "K", u.FromBase(q.lo), u.FromBase(q.hi)
	case phys.Delta:
		u, _ := phys.ByName("dK")
		// dK 与基准 K 等大（Factor = 1），保持端点次序。
		return "dK", "dK", u.FromBase(q.lo), u.FromBase(q.hi)
	}
	return "base", derivedSymbol(q.dim),
		new(big.Rat).Set(q.lo), new(big.Rat).Set(q.hi)
}

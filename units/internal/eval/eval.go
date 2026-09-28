// Package eval 对已解析的语法树求值。
//
// 所有数值都是约分有理数（math/big.Rat），全部先换算到基准单位
// m、kg、s、K。每个节点产出一个 Q（种类 + 维度向量 + 基准值），
// 并以后序方式记录逐步推导。
package eval

import (
	"fmt"
	"math/big"
	"strings"

	"units/internal/parse"
	"units/internal/phys"
)

// Q 是一个带种类与维度的有理数物理量。
type Q struct {
	Kind  phys.Kind `json:"-"`
	Kind2 string    `json:"kind"`
	Dim   phys.Dim  `json:"dim"`
	// Value 始终以基准单位 m, kg, s, K 表示。
	Value *big.Rat `json:"-"`
}

func mkQ(k phys.Kind, d phys.Dim, v *big.Rat) Q {
	return Q{Kind: k, Kind2: k.JSON(), Dim: d, Value: v}
}

// 求值阶段错误码。
const (
	ErrAbsMul        = "ABSOLUTE_TEMPERATURE_MULTIPLY"
	ErrAbsAdd        = "ABSOLUTE_TEMPERATURE_ADD"
	ErrDeltaSubAbs   = "DELTA_MINUS_ABSOLUTE"
	ErrKindIncompat  = "KIND_INCOMPATIBLE"
	ErrDimMismatch   = "DIMENSION_MISMATCH"
	ErrDivZero       = "DIVISION_BY_ZERO"
	ErrTargetKind    = "TARGET_KIND_INCOMPATIBLE"
	ErrTargetDim     = "TARGET_DIMENSION_MISMATCH"
	ErrUnknownTarget = "UNKNOWN_TARGET_UNIT"
)

// Error 携带出错的最小子表达式区间。
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Pos     int    `json:"pos"`
	End     int    `json:"end"`
}

func (e Error) Error() string { return fmt.Sprintf("%s: %s (%d:%d)", e.Code, e.Message, e.Pos, e.End) }

func evalErrf(n parse.Node, code, format string, args ...any) Error {
	return Error{Code: code, Message: fmt.Sprintf(format, args...), Pos: n.Pos(), End: n.End()}
}

// Step 是一个语法节点的推导结果。
type Step struct {
	NodeType string   `json:"nodeType"`
	Op       string   `json:"op,omitempty"`
	Src      string   `json:"source"`
	Pos      int      `json:"pos"`
	End      int      `json:"end"`
	Depth    int      `json:"depth"`
	Kind     string   `json:"kind"`
	Dim      phys.Dim `json:"dim"`
	DimName  string   `json:"dimName"`
	Value    RatView  `json:"valueBase"`
	Note     string   `json:"note"`
}

// RatView 同时给出精确分数字符串与有限/截断小数。
type RatView struct {
	Exact   string `json:"exact"`
	Decimal string `json:"decimal"`
}

func view(r *big.Rat) RatView {
	return RatView{Exact: ratExact(r), Decimal: ratDecimal(r, 12)}
}

// ratExact 返回约分分数；分母为 1 时只返回整数。
func ratExact(r *big.Rat) string {
	if r.IsInt() {
		return r.Num().String()
	}
	return r.String()
}

// ratDecimal 返回最多 prec 位小数的十进制近似，去掉尾随 0。
func ratDecimal(r *big.Rat, prec int) string {
	f, _ := r.Float64()
	s := fmt.Sprintf("%.*f", prec, f)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(s, "0")
		s = strings.TrimRight(s, ".")
	}
	return s
}

// Result 是整棵树的求值结果。
type Result struct {
	Steps []Step   `json:"steps"`
	Root  RootView `json:"root"`
}

// RootView 是根节点换算到目标单位后的精确值。
type RootView struct {
	Src       string   `json:"source"`
	Pos       int      `json:"pos"`
	End       int      `json:"end"`
	Kind      string   `json:"kind"`
	Dim       phys.Dim `json:"dim"`
	DimName   string   `json:"dimName"`
	Target    string   `json:"target"`
	TargetSym string   `json:"targetSymbol"`
	Value     RatView  `json:"value"`
	ValueBase RatView  `json:"valueBase"`
}

type evaluator struct {
	steps []Step
}

// Eval 求 AST 的值；target 为空字符串时自动选择目标单位。
func Eval(a *parse.AST, target string) (*Result, error) {
	if n := firstRange(a.Root); n != nil {
		return nil, evalErrf(n, "RANGE_REQUIRES_RANGE_EVAL", "区间字面量请使用区间求值入口")
	}
	ev := &evaluator{}
	q, err := ev.evalNode(a.Root)
	if err != nil {
		return nil, err
	}
	root, err := convertTarget(a.Root, q, target)
	if err != nil {
		return nil, err
	}
	return &Result{Steps: ev.steps, Root: root}, nil
}

func firstRange(n parse.Node) parse.Node {
	switch x := n.(type) {
	case *parse.NumberNode:
		if x.Upper != nil {
			return x
		}
	case *parse.UnaryNode:
		return firstRange(x.Child)
	case *parse.GroupNode:
		return firstRange(x.Inner)
	case *parse.BinaryNode:
		if found := firstRange(x.Left); found != nil {
			return found
		}
		return firstRange(x.Right)
	}
	return nil
}

func (ev *evaluator) evalNode(n parse.Node) (Q, error) {
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
	return Q{}, Error{Code: "INTERNAL", Message: "未知语法节点", Pos: n.Pos(), End: n.End()}
}

func (ev *evaluator) emit(n parse.Node, nt, op string, q Q, note string) {
	ev.steps = append(ev.steps, Step{
		NodeType: nt, Op: op, Src: n.Src(), Pos: n.Pos(), End: n.End(),
		Depth: depthOf(n), Kind: q.Kind.JSON(), Dim: q.Dim, DimName: q.Dim.String(),
		Value: view(q.Value), Note: note,
	})
}

func depthOf(n parse.Node) int {
	type depther interface{ Depth() int }
	if d, ok := n.(depther); ok {
		return d.Depth()
	}
	return 0
}

func (ev *evaluator) evalNumber(n *parse.NumberNode) (Q, error) {
	v := new(big.Rat).Set(n.Value)
	var q Q
	if n.Unit == nil {
		q = mkQ(phys.Normal, phys.Dim{}, v)
		ev.emit(n, "literal", "", q, fmt.Sprintf("字面量 %s（无量纲）", ratExact(v)))
		return q, nil
	}
	u := n.Unit
	base := u.ToBase(v)
	q = mkQ(u.Kind, u.Dim, base)
	baseUnit := []string{"m", "kg", "s", "K"}[baseDimIndex(u.Dim)]
	if u.Dim.IsZero() {
		baseUnit = "1"
	}
	ev.emit(n, "literal", "", q,
		fmt.Sprintf("字面量 %s %s，换算到基准单位 = %s %s", ratExact(v), u.Sym, ratExact(base), baseUnit))
	return q, nil
}

// baseDimIndex 返回单位“纯一”维度对应的基准单位下标；仅用于展示。
func baseDimIndex(d phys.Dim) int {
	switch {
	case d.L != 0:
		return 0
	case d.M != 0:
		return 1
	case d.T != 0:
		return 2
	default:
		return 3
	}
}

func (ev *evaluator) evalUnary(n *parse.UnaryNode) (Q, error) {
	c, err := ev.evalNode(n.Child)
	if err != nil {
		return Q{}, err
	}
	// 负号对任何种类都只是基准值取负（负的绝对温度在数学上仍然成立）。
	q := mkQ(c.Kind, c.Dim, new(big.Rat).Neg(c.Value))
	ev.emit(n, "unary", "-", q,
		fmt.Sprintf("一元负号：-(%s) = %s，种类与维度不变", ratExact(c.Value), ratExact(q.Value)))
	return q, nil
}

func (ev *evaluator) evalGroup(n *parse.GroupNode) (Q, error) {
	c, err := ev.evalNode(n.Inner)
	if err != nil {
		return Q{}, err
	}
	q := mkQ(c.Kind, c.Dim, new(big.Rat).Set(c.Value))
	ev.emit(n, "group", "()", q, "括号只改变结合顺序，值、种类与维度不变")
	return q, nil
}

func (ev *evaluator) evalBinary(n *parse.BinaryNode) (Q, error) {
	a, err := ev.evalNode(n.Left)
	if err != nil {
		return Q{}, err
	}
	b, err := ev.evalNode(n.Right)
	if err != nil {
		return Q{}, err
	}

	if n.Op == '*' || n.Op == '/' {
		return ev.evalMulDiv(n, a, b)
	}
	return ev.evalAddSub(n, a, b)
}

func (ev *evaluator) evalMulDiv(n *parse.BinaryNode, a, b Q) (Q, error) {
	if a.Kind == phys.Absolute || b.Kind == phys.Absolute {
		return Q{}, evalErrf(n, ErrAbsMul,
			"绝对温度不能相乘或相除：绝对温度不是可乘的普通数；请先取温差（绝对-绝对）再运算")
	}
	if n.Op == '/' && b.Value.Sign() == 0 {
		return Q{}, evalErrf(n, ErrDivZero, "除数为零")
	}
	var d phys.Dim
	var v *big.Rat
	if n.Op == '*' {
		d = a.Dim.Add(b.Dim)
		v = new(big.Rat).Mul(a.Value, b.Value)
	} else {
		d = a.Dim.Sub(b.Dim)
		v = new(big.Rat).Quo(a.Value, b.Value)
	}
	k := phys.Normal
	// 仅当结果温度维度恰好为 1 且来源含温差时，结果仍是温差。
	if d.Q == 1 && (a.Kind == phys.Delta || b.Kind == phys.Delta) {
		k = phys.Delta
	}
	q := mkQ(k, d, v)
	op := string(n.Op)
	ev.emit(n, "binary", op, q, fmt.Sprintf(
		"乘除：%s %s %s = %s；维度 %s %s %s = %s",
		ratExact(a.Value), op, ratExact(b.Value), ratExact(v),
		a.Dim.String(), op, b.Dim.String(), d.String()))
	return q, nil
}

func (ev *evaluator) evalAddSub(n *parse.BinaryNode, a, b Q) (Q, error) {
	op := string(n.Op)
	dimEq := a.Dim.Equal(b.Dim)

	// 绝对 ± 绝对
	if a.Kind == phys.Absolute && b.Kind == phys.Absolute {
		if n.Op == '-' {
			q := mkQ(phys.Delta, a.Dim, new(big.Rat).Sub(a.Value, b.Value))
			ev.emit(n, "binary", op, q,
				"绝对温度 - 绝对温度 = 温差（K 基准值相减，结果为温差）")
			return q, nil
		}
		return Q{}, evalErrf(n, ErrAbsAdd,
			"两个绝对温度不能相加：绝对温度的零点不是数量零点，相加没有物理意义")
	}

	// 至少一侧是绝对温度
	if a.Kind == phys.Absolute || b.Kind == phys.Absolute {
		if !dimEq {
			return Q{}, evalErrf(n, ErrDimMismatch,
				"维度不相容：%s（%s）与 %s（%s）不能相%s",
				a.Dim.String(), a.Kind.String(), b.Dim.String(), b.Kind.String(), opName(n.Op))
		}
		if a.Kind == phys.Absolute {
			// 绝对 ± 温差 => 绝对；绝对 ± 普通量 => 种类不相容
			if b.Kind != phys.Delta {
				return Q{}, evalErrf(n, ErrKindIncompat,
					"绝对温度只能与温差相加减，不能与普通量相%s", opName(n.Op))
			}
			v := new(big.Rat)
			if n.Op == '+' {
				v.Add(a.Value, b.Value)
			} else {
				v.Sub(a.Value, b.Value)
			}
			q := mkQ(phys.Absolute, a.Dim, v)
			ev.emit(n, "binary", op, q,
				"绝对温度 ± 温差 = 绝对温度（在 K 基准值上平移）")
			return q, nil
		}
		// 右侧是绝对：温差 + 绝对 => 绝对；温差 - 绝对非法
		if a.Kind != phys.Delta {
			return Q{}, evalErrf(n, ErrKindIncompat,
				"普通量不能与绝对温度相%s", opName(n.Op))
		}
		if n.Op == '+' {
			q := mkQ(phys.Absolute, a.Dim, new(big.Rat).Add(a.Value, b.Value))
			ev.emit(n, "binary", op, q, "温差 + 绝对温度 = 绝对温度")
			return q, nil
		}
		return Q{}, evalErrf(n, ErrDeltaSubAbs,
			"温差减去绝对温度没有意义：只有 绝对-绝对、绝对±温差、温差+绝对 合法")
	}

	// 两侧均非绝对：维度必须相同；温差与同维度量相容，结果含温差即为温差。
	if !dimEq {
		return Q{}, evalErrf(n, ErrDimMismatch,
			"维度不相容：%s 与 %s 不能相%s", a.Dim.String(), b.Dim.String(), opName(n.Op))
	}
	var v *big.Rat
	if n.Op == '+' {
		v = new(big.Rat).Add(a.Value, b.Value)
	} else {
		v = new(big.Rat).Sub(a.Value, b.Value)
	}
	k := phys.Normal
	if a.Kind == phys.Delta || b.Kind == phys.Delta {
		k = phys.Delta
	}
	q := mkQ(k, dCopy(a.Dim), v)
	note := "同维度普通量相加减（基准单位相同，直接做有理数运算）"
	if k == phys.Delta {
		note = "温差与温差（或同维量）相加减，结果仍为温差；华氏温差已按 5/9 折算为 K"
	}
	ev.emit(n, "binary", op, q, note)
	return q, nil
}

func dCopy(d phys.Dim) phys.Dim { return d }

func opName(op byte) string {
	if op == '+' {
		return "加"
	}
	return "减"
}

// convertTarget 把根节点结果换算到目标单位。target 为空时自动选择。
func convertTarget(n parse.Node, q Q, target string) (RootView, error) {
	rv := RootView{
		Src: n.Src(), Pos: n.Pos(), End: n.End(),
		Kind: q.Kind.JSON(), Dim: q.Dim, DimName: q.Dim.String(),
		ValueBase: view(q.Value),
	}
	if target == "" || target == "auto" {
		name, sym, val := autoTarget(q)
		rv.Target, rv.TargetSym, rv.Value = name, sym, view(val)
		return rv, nil
	}
	u, ok := phys.ByName(target)
	if !ok {
		return rv, Error{Code: ErrUnknownTarget,
			Message: fmt.Sprintf("未知目标单位 %q", target), Pos: n.Pos(), End: n.End()}
	}
	if !u.Dim.Equal(q.Dim) {
		return rv, evalErrf(n, ErrTargetDim,
			"目标单位 %s 的维度是 %s，与结果维度 %s 不一致", u.Sym, u.Dim.String(), q.Dim.String())
	}
	if u.Kind != q.Kind {
		return rv, evalErrf(n, ErrTargetKind,
			"目标单位 %s 属于%s，而结果是%s，不能这样表示", u.Sym, u.Kind.String(), q.Kind.String())
	}
	rv.Target, rv.TargetSym = u.Name, u.Sym
	rv.Value = view(u.FromBase(q.Value))
	return rv, nil
}

// autoTarget 为结果选择默认目标单位并完成换算。
func autoTarget(q Q) (name, sym string, val *big.Rat) {
	switch q.Kind {
	case phys.Absolute:
		u, _ := phys.ByName("K")
		return "K", "K", u.FromBase(q.Value)
	case phys.Delta:
		u, _ := phys.ByName("dK")
		return "dK", "dK", u.FromBase(q.Value)
	}
	return "base", derivedSymbol(q.Dim), new(big.Rat).Set(q.Value)
}

func derivedSymbol(d phys.Dim) string {
	type bp struct {
		s string
		e int
	}
	var pos, neg []bp
	add := func(list *[]bp, s string, e int) {
		if e == 1 {
			*list = append(*list, bp{s, 1})
		} else {
			*list = append(*list, bp{fmt.Sprintf("%s^%d", s, e), e})
		}
	}
	if d.L > 0 {
		add(&pos, "m", d.L)
	} else if d.L < 0 {
		add(&neg, "m", -d.L)
	}
	if d.M > 0 {
		add(&pos, "kg", d.M)
	} else if d.M < 0 {
		add(&neg, "kg", -d.M)
	}
	if d.T > 0 {
		add(&pos, "s", d.T)
	} else if d.T < 0 {
		add(&neg, "s", -d.T)
	}
	if d.Q > 0 {
		add(&pos, "K", d.Q)
	} else if d.Q < 0 {
		add(&neg, "K", -d.Q)
	}
	join := func(xs []bp) string {
		out := ""
		for i, x := range xs {
			if i > 0 {
				out += "·"
			}
			out += x.s
		}
		return out
	}
	switch {
	case len(pos) == 0 && len(neg) == 0:
		return "1"
	case len(neg) == 0:
		return join(pos)
	case len(pos) == 0:
		if len(neg) == 1 {
			return "1/" + neg[0].s
		}
		return "1/(" + join(neg) + ")"
	default:
		s := join(pos) + "/"
		if len(neg) == 1 {
			s += neg[0].s
		} else {
			s += "(" + join(neg) + ")"
		}
		return s
	}
}

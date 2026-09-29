package eval_test

// 与 cross_test.go 相同思路：用独立实现的区间算术（不复用生产的
// range.go）对随机生成的小语法树做对拍。区间叶子渲染成 [lo,hi]，
// 独立求值器逐节点给出 [lo,hi] 与种类/维度，再与生产 EvalRange
// 比较；同时在每个角点调用生产标量 Eval，保证：
//   1. 区间结果的四个角点各自合法（标量入口与区间入口的合法性一致）；
//   2. 所有角点结果都落在生产给出的区间内；
//   3. 独立区间与生产区间上下界分别相等；
//   4. 非法时错误码一致（重点是跨零除数）。

import (
	"math/big"
	"math/rand"
	"testing"

	"units/internal/eval"
	"units/internal/parse"
)

type rgnode interface{ rgnode() }

type rgLeaf struct {
	lo, hi string
	unit   string
}
type rgNeg struct{ c rgnode }
type rgBin struct {
	op   byte
	l, r rgnode
}

func (rgLeaf) rgnode() {}
func (rgNeg) rgnode()  {}
func (rgBin) rgnode()  {}

// 与 cross_test.go 相同的独立分数类型（另起一份，避免跨测试耦合）。
type ifrac struct{ n, d *big.Int }

func inew(a, b int64) *ifrac {
	return (&ifrac{n: big.NewInt(a), d: big.NewInt(b)}).inorm()
}
func ifromRat(r *big.Rat) *ifrac {
	return (&ifrac{n: new(big.Int).Set(r.Num()), d: new(big.Int).Set(r.Denom())}).inorm()
}
func (f *ifrac) inorm() *ifrac {
	if f.d.Sign() < 0 {
		f.n.Neg(f.n)
		f.d.Neg(f.d)
	}
	g := new(big.Int).GCD(nil, nil, new(big.Int).Abs(f.n), f.d)
	f.n.Quo(f.n, g)
	f.d.Quo(f.d, g)
	return f
}
func iadd(a, b *ifrac) *ifrac {
	return (&ifrac{
		n: new(big.Int).Add(new(big.Int).Mul(a.n, b.d), new(big.Int).Mul(b.n, a.d)),
		d: new(big.Int).Mul(a.d, b.d)}).inorm()
}
func isub(a, b *ifrac) *ifrac {
	return (&ifrac{
		n: new(big.Int).Sub(new(big.Int).Mul(a.n, b.d), new(big.Int).Mul(b.n, a.d)),
		d: new(big.Int).Mul(a.d, b.d)}).inorm()
}
func imul(a, b *ifrac) *ifrac {
	return (&ifrac{n: new(big.Int).Mul(a.n, b.n), d: new(big.Int).Mul(a.d, b.d)}).inorm()
}
func idiv(a, b *ifrac) *ifrac {
	return (&ifrac{
		n: new(big.Int).Mul(a.n, new(big.Int).Neg(b.d)),
		d: new(big.Int).Mul(a.d, new(big.Int).Neg(b.n))}).inorm()
}
func ineg(a *ifrac) *ifrac {
	return &ifrac{n: new(big.Int).Neg(a.n), d: new(big.Int).Set(a.d)}
}
func (f *ifrac) isZero() bool { return f.n.Sign() == 0 }
func (f *ifrac) toRat() *big.Rat {
	return new(big.Rat).SetFrac(f.n, f.d)
}
func icmp(a, b *ifrac) int {
	return new(big.Int).Mul(a.n, b.d).Cmp(new(big.Int).Mul(b.n, a.d))
}

type iintv struct{ lo, hi *ifrac } // 恒有 lo <= hi

type iqty2 struct {
	kind int
	dim  idim
	v    iintv
}

// 独立区间运算（直接按定义写，不调用生产代码）。
func iintvNeg(a iintv) iintv { return iintv{ineg(a.hi), ineg(a.lo)} }
func iintvAdd(a, b iintv) iintv {
	return iintv{iadd(a.lo, b.lo), iadd(a.hi, b.hi)}
}
func iintvSub(a, b iintv) iintv {
	return iintv{isub(a.lo, b.hi), isub(a.hi, b.lo)}
}
func iintvCorners(a, b iintv, f func(x, y *ifrac) *ifrac) iintv {
	cs := []*ifrac{f(a.lo, b.lo), f(a.lo, b.hi), f(a.hi, b.lo), f(a.hi, b.hi)}
	lo, hi := cs[0], cs[0]
	for _, c := range cs[1:] {
		if icmp(c, lo) < 0 {
			lo = c
		}
		if icmp(c, hi) > 0 {
			hi = c
		}
	}
	return iintv{lo, hi}
}
func iintvMul(a, b iintv) iintv {
	return iintvCorners(a, b, imul)
}
func iintvDiv(a, b iintv) iintv {
	return iintvCorners(a, b, idiv)
}
func (v iintv) containsZero() bool {
	zero := inew(0, 1)
	return icmp(v.lo, zero) <= 0 && icmp(zero, v.hi) <= 0
}

// rgToBase 独立实现的单位换算（与 cross_test.go 的 toBase 同规则、不同类型）。
func rgToBase(u rgUnit, v *ifrac) *ifrac {
	switch u.conv {
	case 1:
		return iadd(v, inew(27315, 100))
	case 2:
		return iadd(imul(isub(v, inew(32, 1)), inew(5, 9)), inew(27315, 100))
	default:
		return imul(v, u.fac)
	}
}

// rgUnit 是本文件独立的单位表条目（独立分数类型）。
type rgUnit struct {
	dim  idim
	kind int
	conv int
	fac  *ifrac
}

func rgLin(d idim, k int, a, b int64) rgUnit {
	return rgUnit{dim: d, kind: k, conv: 0, fac: inew(a, b)}
}

var rgUnitTable = func() map[string]rgUnit {
	return map[string]rgUnit{
		"m":   rgLin(idim{l: 1}, kNormal, 1, 1),
		"cm":  rgLin(idim{l: 1}, kNormal, 1, 100),
		"kg":  rgLin(idim{m: 1}, kNormal, 1, 1),
		"g":   rgLin(idim{m: 1}, kNormal, 1, 1000),
		"s":   rgLin(idim{t: 1}, kNormal, 1, 1),
		"min": rgLin(idim{t: 1}, kNormal, 60, 1),
		"dK":  rgLin(idim{q: 1}, kDelta, 1, 1),
		"dC":  rgLin(idim{q: 1}, kDelta, 1, 1),
		"dF":  rgLin(idim{q: 1}, kDelta, 5, 9),
	}
}()

// 叶子数值：非负有理数（避开绝对温度非法负值与区间端点排序问题）。
var rgNums = []string{"0", "1", "2", "3", "4", "1/2", "2/3", "3/4", "1/100", "60", "100", "5/9", "9/5"}

// 只用线性单位：区间对拍集中在算术与维度上，温度种类由确定性用例覆盖。
var rgUnits = []string{"m", "cm", "kg", "g", "s", "min", "dK", "dC", "dF", ""}

func genRgLeaf(rng *rand.Rand) rgLeaf {
	a := rgNums[rng.Intn(len(rgNums))]
	b := rgNums[rng.Intn(len(rgNums))]
	ra, _ := new(big.Rat).SetString(a)
	rb, _ := new(big.Rat).SetString(b)
	if ra.Cmp(rb) > 0 {
		a, b = b, a
	}
	return rgLeaf{lo: a, hi: b, unit: rgUnits[rng.Intn(len(rgUnits))]}
}

func genRgTree(rng *rand.Rand, depth int) rgnode {
	if depth <= 0 || rng.Intn(100) < 30 {
		return genRgLeaf(rng)
	}
	if rng.Intn(8) == 0 {
		return rgNeg{c: genRgTree(rng, depth-1)}
	}
	ops := []byte{'+', '-', '*', '/'}
	return rgBin{op: ops[rng.Intn(4)], l: genRgTree(rng, depth-1), r: genRgTree(rng, depth-1)}
}

func renderRg(g rgnode) string {
	switch x := g.(type) {
	case rgLeaf:
		s := "[" + x.lo + "," + x.hi + "]"
		if x.unit != "" {
			s += " " + x.unit
		}
		return s
	case rgNeg:
		// 负号需要包住可能的乘积：用括号，保证 parser 重建同一棵树。
		return "(-" + renderRg(x.c) + ")"
	case rgBin:
		return "(" + renderRg(x.l) + " " + string(x.op) + " " + renderRg(x.r) + ")"
	}
	return ""
}

func rgEval(g rgnode) (iqty2, *ierr) {
	switch x := g.(type) {
	case rgLeaf:
		lo, _ := new(big.Rat).SetString(x.lo)
		hi, _ := new(big.Rat).SetString(x.hi)
		if x.unit == "" {
			return iqty2{kNormal, idim{}, iintv{ifromRat(lo), ifromRat(hi)}}, nil
		}
		u := rgUnitTable[x.unit]
		return iqty2{u.kind, u.dim, iintv{rgToBase(u, ifromRat(lo)), rgToBase(u, ifromRat(hi))}}, nil
	case rgNeg:
		q, e := rgEval(x.c)
		if e != nil {
			return iqty2{}, e
		}
		q.v = iintvNeg(q.v)
		return q, nil
	case rgBin:
		a, e := rgEval(x.l)
		if e != nil {
			return iqty2{}, e
		}
		b, e := rgEval(x.r)
		if e != nil {
			return iqty2{}, e
		}
		if x.op == '*' || x.op == '/' {
			if a.kind == kAbsolute || b.kind == kAbsolute {
				return iqty2{}, &ierr{eval.ErrAbsMul}
			}
			if x.op == '/' && b.v.containsZero() {
				if b.v.lo.isZero() && b.v.hi.isZero() {
					return iqty2{}, &ierr{eval.ErrDivZero}
				}
				return iqty2{}, &ierr{eval.ErrDivSpanZero}
			}
			var d idim
			var v iintv
			if x.op == '*' {
				d, v = a.dim.add(b.dim), iintvMul(a.v, b.v)
			} else {
				d, v = a.dim.sub(b.dim), iintvDiv(a.v, b.v)
			}
			k := kNormal
			if d.q == 1 && (a.kind == kDelta || b.kind == kDelta) {
				k = kDelta
			}
			return iqty2{k, d, v}, nil
		}
		k, d, code, _ := icheckAddSub(x.op, a.kind, b.kind, a.dim, b.dim)
		if code != "" {
			return iqty2{}, &ierr{code}
		}
		var v iintv
		if x.op == '+' {
			v = iintvAdd(a.v, b.v)
		} else {
			v = iintvSub(a.v, b.v)
		}
		return iqty2{k, d, v}, nil
	}
	return iqty2{}, &ierr{"BAD_TREE"}
}

// 与生产 checkAddSub 独立等价的规则实现（只需要错误码与结果种类/维度）。
func icheckAddSub(op byte, ka, kb int, da, db idim) (int, idim, string, string) {
	dimEq := da == db
	if ka == kAbsolute && kb == kAbsolute {
		if op == '-' {
			return kDelta, da, "", ""
		}
		return 0, idim{}, eval.ErrAbsAdd, ""
	}
	if ka == kAbsolute || kb == kAbsolute {
		if !dimEq {
			return 0, idim{}, eval.ErrDimMismatch, ""
		}
		if ka == kAbsolute {
			if kb != kDelta {
				return 0, idim{}, eval.ErrKindIncompat, ""
			}
			return kAbsolute, da, "", ""
		}
		if ka != kDelta {
			return 0, idim{}, eval.ErrKindIncompat, ""
		}
		if op == '+' {
			return kAbsolute, da, "", ""
		}
		return 0, idim{}, eval.ErrDeltaSubAbs, ""
	}
	if !dimEq {
		return 0, idim{}, eval.ErrDimMismatch, ""
	}
	k := kNormal
	if ka == kDelta || kb == kDelta {
		k = kDelta
	}
	return k, da, "", ""
}

// leafSels 生成全部 2^leaves 种叶子端点选择（'L'/'H'，按叶子出现顺序）。
func leafSels(g rgnode) [][]byte {
	var leaves int
	var count func(rgnode)
	count = func(n rgnode) {
		switch x := n.(type) {
		case rgLeaf:
			leaves++
		case rgNeg:
			count(x.c)
		case rgBin:
			count(x.l)
			count(x.r)
		}
	}
	count(g)
	var out [][]byte
	for mask := 0; mask < 1<<leaves; mask++ {
		s := make([]byte, leaves)
		for i := range s {
			if mask&(1<<i) == 0 {
				s[i] = 'L'
			} else {
				s[i] = 'H'
			}
		}
		out = append(out, s)
	}
	return out
}

var rgLeafIdx int

func renderWithSel(g rgnode, sel []byte) string {
	rgLeafIdx = 0
	var rec func(rgnode) string
	rec = func(n rgnode) string {
		switch x := n.(type) {
		case rgLeaf:
			v := x.lo
			if sel[rgLeafIdx] == 'H' {
				v = x.hi
			}
			rgLeafIdx++
			if x.unit != "" {
				return v + " " + x.unit
			}
			return v
		case rgNeg:
			return "(-" + rec(x.c) + ")"
		case rgBin:
			return "(" + rec(x.l) + " " + string(x.op) + " " + rec(x.r) + ")"
		}
		return ""
	}
	return rec(g)
}

func TestRangeCrossCheckRandomTrees(t *testing.T) {
	rng := rand.New(rand.NewSource(20260929))
	const n = 3000
	for i := 0; i < n; i++ {
		g := genRgTree(rng, 2+rng.Intn(2))
		src := renderRg(g)

		ast, perr := parse.Parse(src)
		if perr != nil {
			t.Fatalf("case %d: not parseable: %q -> %v", i, src, perr)
		}
		res, eerr := eval.EvalRange(ast, "")
		iq, ierrv := rgEval(g)

		if ierrv != nil {
			if eerr == nil {
				t.Fatalf("case %d: %q\nindependent rejected (%s), production accepted",
					i, src, ierrv.code)
			}
			ee := eerr.(eval.Error)
			if ee.Code != ierrv.code {
				t.Fatalf("case %d: %q\ncode: independent=%s production=%s",
					i, src, ierrv.code, ee.Code)
			}
			if ee.Pos < 0 || ee.End < ee.Pos || ee.End >= len([]rune(src)) {
				t.Fatalf("case %d: bad span [%d,%d] for %q", i, ee.Pos, ee.End, src)
			}
			continue
		}
		if eerr != nil {
			t.Fatalf("case %d: %q\nproduction rejected (%v), independent accepted kind=%s",
				i, src, eerr, kindName(iq.kind))
		}

		// 种类、维度一致。
		if res.Root.Kind != kindName(iq.kind) {
			t.Fatalf("case %d: %q\nkind: independent=%s production=%s",
				i, src, kindName(iq.kind), res.Root.Kind)
		}
		wantDim := [4]int{iq.dim.l, iq.dim.m, iq.dim.t, iq.dim.q}
		if got := res.Root.Dim.Vector(); got != wantDim {
			t.Fatalf("case %d: %q\ndim: independent=%v production=%v", i, src, wantDim, got)
		}

		// 区间上下界一致（独立区间算术 vs 生产）。
		lo := iq.v.lo.toRat()
		hi := iq.v.hi.toRat()
		if exactRat(t, res.Root.LowerBase).Cmp(lo) != 0 {
			t.Fatalf("case %d: %q\nlower: independent=%s production=%s",
				i, src, lo, res.Root.LowerBase.Exact)
		}
		if exactRat(t, res.Root.UpperBase).Cmp(hi) != 0 {
			t.Fatalf("case %d: %q\nupper: independent=%s production=%s",
				i, src, hi, res.Root.UpperBase.Exact)
		}

		// 每个角点走标量入口：必须全部成功且落在区间内，
		// 这同时验证了 Lo<=Hi 与“覆盖所有可能取值”。
		for _, sel := range leafSels(g) {
			cornerSrc := renderWithSel(g, sel)
			cast, cerr := parse.Parse(cornerSrc)
			if cerr != nil {
				t.Fatalf("case %d: corner not parseable: %q", i, cornerSrc)
			}
			cres, cerr := eval.Eval(cast, "")
			if cerr != nil {
				t.Fatalf("case %d: interval %q accepted but corner %q rejected: %v",
					i, src, cornerSrc, cerr)
			}
			cv := exactRat(t, cres.Root.ValueBase)
			if cv.Cmp(lo) < 0 || cv.Cmp(hi) > 0 {
				t.Fatalf("case %d: corner %q = %s outside [%s,%s] from %q",
					i, cornerSrc, cv, lo, hi, src)
			}
		}

		// 每个语法节点的展示区间都不能倒置。
		for j, s := range res.Steps {
			if exactRat(t, s.Lower).Cmp(exactRat(t, s.Upper)) > 0 {
				t.Fatalf("case %d: step %d inverted [%s,%s]", i, j, s.Lower.Exact, s.Upper.Exact)
			}
		}
	}
}

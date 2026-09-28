package eval_test

import (
	"fmt"
	"math/big"
	"math/rand"
	"testing"

	"units/internal/eval"
	"units/internal/parse"
)

// 本文件实现一个与生产代码完全独立的第二求值器：
//   - 自己的分数类型（big.Int 分子/分母，自行约分）
//   - 自己的单位表与维度/种类规则
//   - 自己的运算规则实现
// 然后对随机生成的小语法树渲染成表达式、经生产 parser 解析、
// 分别交给两个求值器，逐案比较：成功时比较根的种类、维度与精确有理值；
// 失败时比较错误码。

// ---- 独立分数类型 ----

type frac struct {
	n, d *big.Int // d > 0
}

func newFrac(a, b int64) *frac {
	f := &frac{n: big.NewInt(a), d: big.NewInt(b)}
	return f.norm()
}

func (f *frac) norm() *frac {
	if f.d.Sign() == 0 {
		panic("zero denominator in literal")
	}
	if f.d.Sign() < 0 {
		f.n.Neg(f.n)
		f.d.Neg(f.d)
	}
	g := new(big.Int).GCD(nil, nil, new(big.Int).Abs(f.n), f.d)
	f.n.Quo(f.n, g)
	f.d.Quo(f.d, g)
	return f
}

func (f *frac) clone() *frac { return &frac{n: new(big.Int).Set(f.n), d: new(big.Int).Set(f.d)} }

func fAdd(a, b *frac) *frac {
	n := new(big.Int).Add(new(big.Int).Mul(a.n, b.d), new(big.Int).Mul(b.n, a.d))
	return (&frac{n: n, d: new(big.Int).Mul(a.d, b.d)}).norm()
}
func fSub(a, b *frac) *frac {
	n := new(big.Int).Sub(new(big.Int).Mul(a.n, b.d), new(big.Int).Mul(b.n, a.d))
	return (&frac{n: n, d: new(big.Int).Mul(a.d, b.d)}).norm()
}
func fMul(a, b *frac) *frac {
	return (&frac{n: new(big.Int).Mul(a.n, b.n), d: new(big.Int).Mul(a.d, b.d)}).norm()
}
func fDiv(a, b *frac) *frac {
	return (&frac{n: new(big.Int).Mul(a.n, b.d), d: new(big.Int).Mul(a.d, b.n)}).norm()
}
func fNeg(a *frac) *frac { return &frac{n: new(big.Int).Neg(a.n), d: new(big.Int).Set(a.d)} }
func (f *frac) isZero() bool { return f.n.Sign() == 0 }
func (f *frac) String() string {
	if f.d.Cmp(big.NewInt(1)) == 0 {
		return f.n.String()
	}
	return f.n.String() + "/" + f.d.String()
}

// ---- 独立维度/种类/单位表 ----

type idim struct{ l, m, t, q int }

func (d idim) add(e idim) idim { return idim{d.l + e.l, d.m + e.m, d.t + e.t, d.q + e.q} }
func (d idim) sub(e idim) idim { return idim{d.l - e.l, d.m - e.m, d.t - e.t, d.q - e.q} }

const (
	kNormal = iota
	kDelta
	kAbsolute
)

// conv: 0 linear, 1 celsius, 2 fahrenheit
type iunit struct {
	name string
	dim  idim
	kind int
	conv int
	fac  *frac
}

var iunits = buildIUnits()

func buildIUnits() map[string]iunit {
	lin := func(name string, d idim, k int, a, b int64) iunit {
		return iunit{name: name, dim: d, kind: k, conv: 0, fac: newFrac(a, b)}
	}
	return map[string]iunit{
		"m":   lin("m", idim{l: 1}, kNormal, 1, 1),
		"cm":  lin("cm", idim{l: 1}, kNormal, 1, 100),
		"kg":  lin("kg", idim{m: 1}, kNormal, 1, 1),
		"g":   lin("g", idim{m: 1}, kNormal, 1, 1000),
		"s":   lin("s", idim{t: 1}, kNormal, 1, 1),
		"min": lin("min", idim{t: 1}, kNormal, 60, 1),
		"K":   lin("K", idim{q: 1}, kAbsolute, 1, 1),
		"C":   {"C", idim{q: 1}, kAbsolute, 1, nil},
		"F":   {"F", idim{q: 1}, kAbsolute, 2, nil},
		"dK":  lin("dK", idim{q: 1}, kDelta, 1, 1),
		"dC":  lin("dC", idim{q: 1}, kDelta, 1, 1),
		"dF":  lin("dF", idim{q: 1}, kDelta, 5, 9),
	}
}

func toBase(u iunit, v *frac) *frac {
	switch u.conv {
	case 1:
		return fAdd(v, newFrac(27315, 100))
	case 2:
		return fAdd(fMul(fSub(v, newFrac(32, 1)), newFrac(5, 9)), newFrac(27315, 100))
	default:
		return fMul(v, u.fac)
	}
}

type iqty struct {
	kind int
	dim  idim
	val  *frac
}

type ierr struct{ code string }

// ---- 独立运算规则（按需求文档独立实现一遍） ----

func iopMulDiv(op byte, a, b iqty) (iqty, *ierr) {
	if a.kind == kAbsolute || b.kind == kAbsolute {
		return iqty{}, &ierr{eval.ErrAbsMul}
	}
	if op == '/' && b.val.isZero() {
		return iqty{}, &ierr{eval.ErrDivZero}
	}
	var d idim
	var v *frac
	if op == '*' {
		d, v = a.dim.add(b.dim), fMul(a.val, b.val)
	} else {
		d, v = a.dim.sub(b.dim), fDiv(a.val, b.val)
	}
	k := kNormal
	if d.q == 1 && (a.kind == kDelta || b.kind == kDelta) {
		k = kDelta
	}
	return iqty{k, d, v}, nil
}

func iopAddSub(op byte, a, b iqty) (iqty, *ierr) {
	dimEq := a.dim == b.dim
	if a.kind == kAbsolute && b.kind == kAbsolute {
		if op == '-' {
			return iqty{kDelta, a.dim, fSub(a.val, b.val)}, nil
		}
		return iqty{}, &ierr{eval.ErrAbsAdd}
	}
	if a.kind == kAbsolute || b.kind == kAbsolute {
		if !dimEq {
			return iqty{}, &ierr{eval.ErrDimMismatch}
		}
		if a.kind == kAbsolute {
			if b.kind != kDelta {
				return iqty{}, &ierr{eval.ErrKindIncompat}
			}
			if op == '+' {
				return iqty{kAbsolute, a.dim, fAdd(a.val, b.val)}, nil
			}
			return iqty{kAbsolute, a.dim, fSub(a.val, b.val)}, nil
		}
		if a.kind != kDelta {
			return iqty{}, &ierr{eval.ErrKindIncompat}
		}
		if op == '+' {
			return iqty{kAbsolute, a.dim, fAdd(a.val, b.val)}, nil
		}
		return iqty{}, &ierr{eval.ErrDeltaSubAbs}
	}
	if !dimEq {
		return iqty{}, &ierr{eval.ErrDimMismatch}
	}
	var v *frac
	if op == '+' {
		v = fAdd(a.val, b.val)
	} else {
		v = fSub(a.val, b.val)
	}
	k := kNormal
	if a.kind == kDelta || b.kind == kDelta {
		k = kDelta
	}
	return iqty{k, a.dim, v}, nil
}

// ---- 语法树生成 ----

type gnode interface{ gnode() }

type gLeaf struct {
	num  string
	unit string // "" 表示无量纲
}
type gNeg struct{ c gnode }
type gGrp struct{ c gnode }
type gBin struct {
	op          byte
	l, r        gnode
	forceIllegal bool // 用于保证非法模式出现
}

func (gLeaf) gnode() {}
func (gNeg) gnode()  {}
func (gGrp) gnode()  {}
func (gBin) gnode()  {}

// 渲染为生产 parser 能吃的表达式；所有二元节点一律加括号，
// 保证 parser 重建出的逻辑树与生成树完全一致（不受优先级影响）。
func render(g gnode) string {
	switch x := g.(type) {
	case gLeaf:
		if x.unit == "" {
			return x.num
		}
		return x.num + " " + x.unit
	case gNeg:
		return "(-" + render(x.c) + ")"
	case gGrp:
		return "(" + render(x.c) + ")"
	case gBin:
		return "(" + render(x.l) + " " + string(x.op) + " " + render(x.r) + ")"
	}
	return ""
}

var leafNums = []string{"0", "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "12",
	"1/2", "2/3", "3/4", "5/6", "9/5", "32", "27315/100", "60", "100", "1/100", "1/1000"}

var allUnitNames = []string{"m", "cm", "kg", "g", "s", "min", "K", "C", "F", "dK", "dC", "dF", ""}

func genLeaf(rng *rand.Rand) gnode {
	return gLeaf{num: leafNums[rng.Intn(len(leafNums))], unit: allUnitNames[rng.Intn(len(allUnitNames))]}
}

func genTree(rng *rand.Rand, depth int) gnode {
	if depth <= 0 || rng.Intn(100) < 35 {
		return genLeaf(rng)
	}
	switch rng.Intn(10) {
	case 0, 1:
		return gNeg{c: genTree(rng, depth-1)}
	case 2:
		return gGrp{c: genTree(rng, depth-1)}
	default:
		ops := []byte{'+', '-', '*', '/'}
		return gBin{op: ops[rng.Intn(4)], l: genTree(rng, depth-1), r: genTree(rng, depth-1)}
	}
}

// genIllegal 保证生成需求中点名的非法结构。
func genIllegal(rng *rand.Rand) gnode {
	abs := func() gnode {
		u := []string{"K", "C", "F"}[rng.Intn(3)]
		return gLeaf{num: leafNums[rng.Intn(len(leafNums))], unit: u}
	}
	switch rng.Intn(3) {
	case 0: // 绝对 × 任意 或 任意 / 绝对
		if rng.Intn(2) == 0 {
			return gBin{op: '*', l: abs(), r: genLeaf(rng), forceIllegal: true}
		}
		return gBin{op: '/', l: genLeaf(rng), r: abs(), forceIllegal: true}
	case 1: // 绝对 + 绝对
		return gBin{op: '+', l: abs(), r: abs(), forceIllegal: true}
	default: // 温差 - 绝对
		d := gLeaf{num: "1", unit: []string{"dK", "dC", "dF"}[rng.Intn(3)]}
		return gBin{op: '-', l: d, r: abs(), forceIllegal: true}
	}
}

// 独立求值器入口：先 render，再用生产 parser 解析得到节点区间，
// 独立求值通过并行遍历生成树完成。
func iEval(g gnode) (iqty, *ierr) {
	switch x := g.(type) {
	case gLeaf:
		v, ok := new(big.Rat).SetString(x.num)
		if !ok {
			return iqty{}, &ierr{"BAD_LITERAL"}
		}
		if x.unit == "" {
			return iqty{kNormal, idim{}, &frac{n: v.Num(), d: v.Denom()}}, nil
		}
		u := iunits[x.unit]
		f := &frac{n: new(big.Int).Set(v.Num()), d: new(big.Int).Set(v.Denom())}
		return iqty{u.kind, u.dim, toBase(u, f.norm())}, nil
	case gNeg:
		q, e := iEval(x.c)
		if e != nil {
			return iqty{}, e
		}
		q.val = fNeg(q.val)
		return q, nil
	case gGrp:
		return iEval(x.c)
	case gBin:
		a, e := iEval(x.l)
		if e != nil {
			return iqty{}, e
		}
		b, e := iEval(x.r)
		if e != nil {
			return iqty{}, e
		}
		if x.op == '*' || x.op == '/' {
			return iopMulDiv(x.op, a, b)
		}
		return iopAddSub(x.op, a, b)
	}
	return iqty{}, &ierr{"BAD_TREE"}
}

func kindName(k int) string {
	switch k {
	case kDelta:
		return "delta"
	case kAbsolute:
		return "absolute"
	default:
		return "normal"
	}
}

// TestCrossCheckRandomTrees 对生成的小语法树与独立求值器对拍。
func TestCrossCheckRandomTrees(t *testing.T) {
	rng := rand.New(rand.NewSource(20260926))
	const n = 4000
	for i := 0; i < n; i++ {
		var g gnode
		if i%5 == 0 {
			g = genIllegal(rng)
		} else {
			g = genTree(rng, 2+rng.Intn(2)) // 深度 2~3 的小树
		}
		src := render(g)

		ast, perr := parse.Parse(src)
		iq, ierrv := iEval(g)

		if perr != nil {
			// 生成器只应生成可解析表达式
			t.Fatalf("case %d: generated source not parseable: %q -> %v", i, src, perr)
		}
		res, eerr := eval.Eval(ast, "")

		if ierrv != nil {
			if eerr == nil {
				t.Fatalf("case %d: %q\nindependent rejected (%s) but production accepted: %+v",
					i, src, ierrv.code, res.Root)
			}
			ee := eerr.(eval.Error)
			if ee.Code != ierrv.code {
				t.Fatalf("case %d: %q\nerror mismatch: independent=%s production=%s",
					i, src, ierrv.code, ee.Code)
			}
			// 错误区间必须非空且落在表达式内部。
			if ee.Pos < 0 || ee.End < ee.Pos || ee.End >= len([]rune(src)) {
				t.Fatalf("case %d: bad error span [%d,%d] for %q", i, ee.Pos, ee.End, src)
			}
			continue
		}
		if eerr != nil {
			t.Fatalf("case %d: %q\nproduction rejected (%v) but independent accepted kind=%s dim=%+v val=%s",
				i, src, eerr, kindName(iq.kind), iq.dim, iq.val)
		}
		if res.Root.Kind != kindName(iq.kind) {
			t.Fatalf("case %d: %q\nkind mismatch: independent=%s production=%s",
				i, src, kindName(iq.kind), res.Root.Kind)
		}
		wantDim := [4]int{iq.dim.l, iq.dim.m, iq.dim.t, iq.dim.q}
		gotDim := res.Root.Dim.Vector()
		if wantDim != gotDim {
			t.Fatalf("case %d: %q\ndim mismatch: independent=%v production=%v",
				i, src, wantDim, gotDim)
		}
		got := exactRat(t, res.Root.ValueBase)
		wantRat := &big.Rat{}
		wantRat.SetFrac(iq.val.n, iq.val.d)
		if got.Cmp(wantRat) != 0 {
			t.Fatalf("case %d: %q\nvalue mismatch: independent=%s production=%s",
				i, src, iq.val, got)
		}
	}
}

// TestCrossCheckSpansPrint 快速打印若干生成用例，便于人工检查渲染可读性。
func TestCrossCheckSamplesPrint(t *testing.T) {
	if testing.Verbose() {
		rng := rand.New(rand.NewSource(7))
		for i := 0; i < 10; i++ {
			src := render(genTree(rng, 3))
			t.Logf("sample: %s", src)
		}
	}
}

var _ = fmt.Sprintf

// ---- 区间求值的独立第二实现 ----
//
// 标量求值器对拍成功后，这里再用同一套独立单位表/种类规则实现一个区间
// 求值器：标量量退化为 [v,v]，一元负号交换端点，加减法交叉端点，乘除法
// 枚举四个端点组合取最值，除数区间包含 0 时报错。随机生成含区间字面量的
// 小树，与生产 eval.EvalRange 逐案比较种类、维度与基准区间端点。

type irng struct {
	kind int
	dim  idim
	lo   *frac
	hi   *frac
}

func iFrac(v *big.Rat) *frac {
	f := &frac{n: new(big.Int).Set(v.Num()), d: new(big.Int).Set(v.Denom())}
	return f.norm()
}

func iNumRange(g gnode) (irng, *ierr) {
	switch x := g.(type) {
	case gRngNum:
		lo, _ := new(big.Rat).SetString(x.lo)
		hi, _ := new(big.Rat).SetString(x.hi)
		if x.unit == "" {
			fl, fh := iFrac(lo), iFrac(hi)
			return irng{kNormal, idim{}, fl, fh}, nil
		}
		u := iunits[x.unit]
		bl, bh := toBase(u, iFrac(lo)), toBase(u, iFrac(hi))
		if fCmp(bl, bh) > 0 {
			bl, bh = bh, bl
		}
		return irng{u.kind, u.dim, bl, bh}, nil
	case gLeaf:
		v, ok := new(big.Rat).SetString(x.num)
		if !ok {
			return irng{}, &ierr{"BAD_LITERAL"}
		}
		if x.unit == "" {
			f := iFrac(v)
			return irng{kNormal, idim{}, f, f}, nil
		}
		u := iunits[x.unit]
		f := iFrac(v)
		b := toBase(u, f)
		return irng{u.kind, u.dim, b, b}, nil
	case gNeg:
		q, e := iNumRange(x.c)
		if e != nil {
			return irng{}, e
		}
		return irng{q.kind, q.dim, fNeg(q.hi), fNeg(q.lo)}, nil
	case gGrp:
		return iNumRange(x.c)
	case gBin:
		a, e := iNumRange(x.l)
		if e != nil {
			return irng{}, e
		}
		b, e := iNumRange(x.r)
		if e != nil {
			return irng{}, e
		}
		if x.op == '*' || x.op == '/' {
			return iRngMulDiv(x.op, a, b)
		}
		return iRngAddSub(x.op, a, b)
	}
	return irng{}, &ierr{"BAD_TREE"}
}

func iRngMulDiv(op byte, a, b irng) (irng, *ierr) {
	if a.kind == kAbsolute || b.kind == kAbsolute {
		return irng{}, &ierr{eval.ErrAbsMul}
	}
	if op == '/' {
		zeroIn := func(f *frac) bool { return f.n.Sign() == 0 }
		loNeg := b.lo.n.Sign() < 0
		hiPos := b.hi.n.Sign() > 0
		if zeroIn(b.lo) || zeroIn(b.hi) || (loNeg && hiPos) {
			return irng{}, &ierr{eval.ErrDivZeroRange}
		}
	}
	var d idim
	cands := make([]*frac, 0, 4)
	if op == '*' {
		d = a.dim.add(b.dim)
		for _, x := range []*frac{a.lo, a.hi} {
			for _, y := range []*frac{b.lo, b.hi} {
				cands = append(cands, fMul(x, y))
			}
		}
	} else {
		d = a.dim.sub(b.dim)
		for _, x := range []*frac{a.lo, a.hi} {
			for _, y := range []*frac{b.lo, b.hi} {
				cands = append(cands, fDiv(x, y))
			}
		}
	}
	lo, hi := cands[0], cands[0]
	for _, c := range cands[1:] {
		if fCmp(c, lo) < 0 {
			lo = c
		}
		if fCmp(c, hi) > 0 {
			hi = c
		}
	}
	k := kNormal
	if d.q == 1 && (a.kind == kDelta || b.kind == kDelta) {
		k = kDelta
	}
	return irng{k, d, lo, hi}, nil
}

// fCmp 比较两个分数：-1/0/1。
func fCmp(a, b *frac) int {
	l := new(big.Int).Mul(a.n, b.d)
	r := new(big.Int).Mul(b.n, a.d)
	return l.Cmp(r)
}

func iRngAddSub(op byte, a, b irng) (irng, *ierr) {
	dimEq := a.dim == b.dim
	if a.kind == kAbsolute && b.kind == kAbsolute {
		if op == '-' {
			return irng{kDelta, a.dim, fSub(a.lo, b.hi), fSub(a.hi, b.lo)}, nil
		}
		return irng{}, &ierr{eval.ErrAbsAdd}
	}
	if a.kind == kAbsolute || b.kind == kAbsolute {
		if !dimEq {
			return irng{}, &ierr{eval.ErrDimMismatch}
		}
		if a.kind == kAbsolute {
			if b.kind != kDelta {
				return irng{}, &ierr{eval.ErrKindIncompat}
			}
			if op == '+' {
				return irng{kAbsolute, a.dim, fAdd(a.lo, b.lo), fAdd(a.hi, b.hi)}, nil
			}
			return irng{kAbsolute, a.dim, fSub(a.lo, b.hi), fSub(a.hi, b.lo)}, nil
		}
		if a.kind != kDelta {
			return irng{}, &ierr{eval.ErrKindIncompat}
		}
		if op == '+' {
			return irng{kAbsolute, a.dim, fAdd(a.lo, b.lo), fAdd(a.hi, b.hi)}, nil
		}
		return irng{}, &ierr{eval.ErrDeltaSubAbs}
	}
	if !dimEq {
		return irng{}, &ierr{eval.ErrDimMismatch}
	}
	var lo, hi *frac
	if op == '+' {
		lo, hi = fAdd(a.lo, b.lo), fAdd(a.hi, b.hi)
	} else {
		lo, hi = fSub(a.lo, b.hi), fSub(a.hi, b.lo)
	}
	k := kNormal
	if a.kind == kDelta || b.kind == kDelta {
		k = kDelta
	}
	return irng{k, a.dim, lo, hi}, nil
}

// gRngLeaf 生成带区间字面量的叶子（区间两端点为不同有理数）。
func gRngLeaf(rng *rand.Rand) gnode {
	if rng.Intn(2) == 0 {
		return genLeaf(rng)
	}
	nums := []string{"-4", "-2", "-1", "0", "1", "2", "3", "5", "1/2", "3/2"}
	lo := nums[rng.Intn(len(nums))]
	hi := nums[rng.Intn(len(nums))]
	rlo, _ := new(big.Rat).SetString(lo)
	rhi, _ := new(big.Rat).SetString(hi)
	if rlo.Cmp(rhi) > 0 {
		lo, hi = hi, lo
	}
	unit := allUnitNames[rng.Intn(len(allUnitNames))]
	return gRngNum{lo: lo, hi: hi, unit: unit}
}

type gRngNum struct {
	lo, hi, unit string
}

func (gRngNum) gnode() {}

func renderRng(g gnode) string {
	switch x := g.(type) {
	case gRngNum:
		num := "[" + x.lo + "," + x.hi + "]"
		if x.unit == "" {
			return num
		}
		return num + " " + x.unit
	case gLeaf:
		if x.unit == "" {
			return x.num
		}
		return x.num + " " + x.unit
	case gNeg:
		return "(-" + renderRng(x.c) + ")"
	case gGrp:
		return "(" + renderRng(x.c) + ")"
	case gBin:
		return "(" + renderRng(x.l) + " " + string(x.op) + " " + renderRng(x.r) + ")"
	}
	return ""
}

func genRngTree(rng *rand.Rand, depth int) gnode {
	if depth <= 0 || rng.Intn(100) < 35 {
		return gRngLeaf(rng)
	}
	switch rng.Intn(10) {
	case 0, 1:
		return gNeg{c: genRngTree(rng, depth-1)}
	case 2:
		return gGrp{c: genRngTree(rng, depth-1)}
	default:
		ops := []byte{'+', '-', '*', '/'}
		return gBin{op: ops[rng.Intn(4)], l: genRngTree(rng, depth-1), r: genRngTree(rng, depth-1)}
	}
}

func TestCrossCheckRangeRandomTrees(t *testing.T) {
	rng := rand.New(rand.NewSource(20260928))
	const n = 4000
	for i := 0; i < n; i++ {
		g := genRngTree(rng, 2+rng.Intn(2))
		src := renderRng(g)

		ast, perr := parse.Parse(src)
		if perr != nil {
			t.Fatalf("case %d: generated source not parseable: %q -> %v", i, src, perr)
		}
		iq, ierrv := iNumRange(g)
		res, eerr := eval.EvalRange(ast, "")

		if ierrv != nil {
			if eerr == nil {
				t.Fatalf("case %d: %q\nindependent rejected (%s) but production accepted",
					i, src, ierrv.code)
			}
			ee := eerr.(eval.Error)
			if ee.Code != ierrv.code {
				t.Fatalf("case %d: %q\nerror mismatch: independent=%s production=%s",
					i, src, ierrv.code, ee.Code)
			}
			continue
		}
		if eerr != nil {
			t.Fatalf("case %d: %q\nproduction rejected (%v) but independent accepted kind=%s dim=%+v",
				i, src, eerr, kindName(iq.kind), iq.dim)
		}
		if res.Root.Kind != kindName(iq.kind) {
			t.Fatalf("case %d: %q\nkind mismatch: independent=%s production=%s",
				i, src, kindName(iq.kind), res.Root.Kind)
		}
		wantDim := [4]int{iq.dim.l, iq.dim.m, iq.dim.t, iq.dim.q}
		if wantDim != res.Root.Dim.Vector() {
			t.Fatalf("case %d: %q\ndim mismatch: independent=%v production=%v",
				i, src, wantDim, res.Root.Dim.Vector())
		}
		gotLo := exactRat(t, res.Root.LowerBase)
		wantLo := &big.Rat{}
		wantLo.SetFrac(iq.lo.n, iq.lo.d)
		if gotLo.Cmp(wantLo) != 0 {
			t.Fatalf("case %d: %q\nlower mismatch: independent=%s production=%s",
				i, src, iq.lo, gotLo)
		}
		gotHi := exactRat(t, res.Root.UpperBase)
		wantHi := &big.Rat{}
		wantHi.SetFrac(iq.hi.n, iq.hi.d)
		if gotHi.Cmp(wantHi) != 0 {
			t.Fatalf("case %d: %q\nupper mismatch: independent=%s production=%s",
				i, src, iq.hi, gotHi)
		}
	}
}

package eval_test

import (
	"testing"

	"units/internal/eval"
	"units/internal/parse"
)

func evRange(t *testing.T, src, target string) *eval.RangeResult {
	t.Helper()
	a, err := parse.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	r, err := eval.EvalRange(a, target)
	if err != nil {
		t.Fatalf("eval-range %q: %v", src, err)
	}
	return r
}

func expectRangeErr(t *testing.T, src, target, code string) {
	t.Helper()
	a, perr := parse.Parse(src)
	if perr != nil {
		if pe, ok := perr.(parse.Error); ok && pe.Code == code {
			return
		}
		t.Fatalf("expected error %s for %q, got parse error %v", code, src, perr)
	}
	_, err := eval.EvalRange(a, target)
	if err == nil {
		t.Fatalf("expected error %s for %q, got success", code, src)
	}
	if ee, ok := err.(eval.Error); !ok || ee.Code != code {
		t.Fatalf("expected error %s for %q, got %v", code, src, err)
	}
}

func TestRangeSubtraction(t *testing.T) {
	// [1,2] m - [0,1] m = [0,2] m，绝不能算成同向取值的 [1,1]。
	r := evRange(t, "[1,2] m - [0,1] m", "m")
	if got := exactRat(t, r.Root.Lower); got.Cmp(rat("0")) != 0 {
		t.Fatalf("lower = %s, want 0", got)
	}
	if got := exactRat(t, r.Root.Upper); got.Cmp(rat("2")) != 0 {
		t.Fatalf("upper = %s, want 2", got)
	}
	if r.Root.Kind != "normal" || r.Root.TargetSym != "m" {
		t.Fatalf("root = %+v", r.Root)
	}
	// 最后一个节点是减法，基准区间同样为 [0,2]。
	last := r.Steps[len(r.Steps)-1]
	if exactRat(t, last.Lower).Cmp(rat("0")) != 0 || exactRat(t, last.Upper).Cmp(rat("2")) != 0 {
		t.Fatalf("sub step = [%s,%s]", last.Lower.Exact, last.Upper.Exact)
	}
}

func TestRangeNegativeMulDiv(t *testing.T) {
	// [-2,-1] * [1,2]：四角点 -2,-4,-1,-2 => [-4,-1]。
	r := evRange(t, "[-2,-1] * [1,2]", "")
	if exactRat(t, r.Root.LowerBase).Cmp(rat("-4")) != 0 ||
		exactRat(t, r.Root.UpperBase).Cmp(rat("-1")) != 0 {
		t.Fatalf("[-2,-1]*[1,2] = [%s,%s], want [-4,-1]",
			r.Root.LowerBase.Exact, r.Root.UpperBase.Exact)
	}

	// [1,2] / [-2,-1]：四角点 -1/2,-1,-1,-2 => [-2,-1/2]。
	r = evRange(t, "[1,2] / [-2,-1]", "")
	if exactRat(t, r.Root.LowerBase).Cmp(rat("-2")) != 0 ||
		exactRat(t, r.Root.UpperBase).Cmp(rat("-1/2")) != 0 {
		t.Fatalf("[1,2]/[-2,-1] = [%s,%s], want [-2,-1/2]",
			r.Root.LowerBase.Exact, r.Root.UpperBase.Exact)
	}

	// 两侧都跨零的乘法：[-3,-2] * [-1,2]，角点 3,-6,2,-4 => [-6,3]。
	r = evRange(t, "[-3,-2] * [-1,2]", "")
	if exactRat(t, r.Root.LowerBase).Cmp(rat("-6")) != 0 ||
		exactRat(t, r.Root.UpperBase).Cmp(rat("3")) != 0 {
		t.Fatalf("[-3,-2]*[-1,2] = [%s,%s], want [-6,3]",
			r.Root.LowerBase.Exact, r.Root.UpperBase.Exact)
	}

	// 加法不受端点反向影响：[1,2] + [-2,-1] = [-1,1]。
	r = evRange(t, "[1,2] + [-2,-1]", "")
	if exactRat(t, r.Root.LowerBase).Cmp(rat("-1")) != 0 ||
		exactRat(t, r.Root.UpperBase).Cmp(rat("1")) != 0 {
		t.Fatalf("[1,2]+[-2,-1] = [%s,%s], want [-1,1]",
			r.Root.LowerBase.Exact, r.Root.UpperBase.Exact)
	}
}

func TestRangeUnaryMinus(t *testing.T) {
	// -[1,2] = [-2,-1]。
	r := evRange(t, "-[1,2] m", "m")
	if exactRat(t, r.Root.Lower).Cmp(rat("-2")) != 0 ||
		exactRat(t, r.Root.Upper).Cmp(rat("-1")) != 0 {
		t.Fatalf("-[1,2] = [%s,%s], want [-2,-1]", r.Root.Lower.Exact, r.Root.Upper.Exact)
	}
}

func TestRangeDivisorZero(t *testing.T) {
	// 除数区间整段为零 => DIVISION_BY_ZERO。
	expectRangeErr(t, "1 / [0,0]", "", eval.ErrDivZero)
	// 除数区间跨过零（含端点 0）=> 没有有限上下界。
	expectRangeErr(t, "1 / [-1,1]", "", eval.ErrDivSpanZero)
	expectRangeErr(t, "1 / [0,1]", "", eval.ErrDivSpanZero)
	expectRangeErr(t, "[1,2] m / [-1/2,1/2] s", "", eval.ErrDivSpanZero)

	// 错误区间落在除法节点本身。
	a, _ := parse.Parse("1 / [-1,1]")
	_, err := eval.EvalRange(a, "")
	ee := err.(eval.Error)
	if string([]rune("1 / [-1,1]")[ee.Pos:ee.End+1]) != "1 / [-1,1]" {
		t.Fatalf("span = [%d,%d]", ee.Pos, ee.End)
	}
}

func TestRangeTemperature(t *testing.T) {
	// [20,21]°C - [68,86]°F：K 基准 [293.15,294.15] - [293.15,303.15] = [-10,1] dK。
	r := evRange(t, "[20,21] C - [68,86] F", "dC")
	if r.Root.Kind != "delta" {
		t.Fatalf("kind = %s, want delta", r.Root.Kind)
	}
	if exactRat(t, r.Root.LowerBase).Cmp(rat("-10")) != 0 ||
		exactRat(t, r.Root.UpperBase).Cmp(rat("1")) != 0 {
		t.Fatalf("base = [%s,%s], want [-10,1]", r.Root.LowerBase.Exact, r.Root.UpperBase.Exact)
	}
	if exactRat(t, r.Root.Lower).Cmp(rat("-10")) != 0 ||
		exactRat(t, r.Root.Upper).Cmp(rat("1")) != 0 {
		t.Fatalf("dC = [%s,%s], want [-10,1]", r.Root.Lower.Exact, r.Root.Upper.Exact)
	}

	// 同一温差用 dF 表示：[-10,1] dK = [-18, 9/5] dF。
	r = evRange(t, "[20,21] C - [68,86] F", "dF")
	if exactRat(t, r.Root.Lower).Cmp(rat("-18")) != 0 ||
		exactRat(t, r.Root.Upper).Cmp(rat("9/5")) != 0 {
		t.Fatalf("dF = [%s,%s], want [-18,9/5]", r.Root.Lower.Exact, r.Root.Upper.Exact)
	}

	// 绝对温度 ± 温差区间：[20,21]°C + [1,2] dC = [21,23] °C。
	r = evRange(t, "[20,21] C + [1,2] dC", "C")
	if r.Root.Kind != "absolute" {
		t.Fatalf("kind = %s, want absolute", r.Root.Kind)
	}
	if exactRat(t, r.Root.Lower).Cmp(rat("21")) != 0 ||
		exactRat(t, r.Root.Upper).Cmp(rat("23")) != 0 {
		t.Fatalf("[20,21]C+[1,2]dC = [%s,%s] C, want [21,23]", r.Root.Lower.Exact, r.Root.Upper.Exact)
	}

	// 华氏温差区间折算：[10,20] dF = [50/9,100/9] dK，用 dC 表示数值相同。
	r = evRange(t, "[10,20] dF", "dC")
	if exactRat(t, r.Root.Lower).Cmp(rat("50/9")) != 0 ||
		exactRat(t, r.Root.Upper).Cmp(rat("100/9")) != 0 {
		t.Fatalf("[10,20] dF in dC = [%s,%s], want [50/9,100/9]",
			r.Root.Lower.Exact, r.Root.Upper.Exact)
	}
}

func TestRangeTargetValidation(t *testing.T) {
	expectRangeErr(t, "[1,2] m", "kg", eval.ErrTargetDim)
	expectRangeErr(t, "[1,2] K - [0,0] K", "K", eval.ErrTargetKind)
	expectRangeErr(t, "[1,2] m", "zzz", eval.ErrUnknownTarget)
}

func TestRangeScalarEquivalence(t *testing.T) {
	// 不含区间字面量时，区间入口与标量入口给出完全一致的结果。
	src, target := "1 m + 2 cm", "m"
	sa, _ := parse.Parse(src)
	scalar, err := eval.Eval(sa, target)
	if err != nil {
		t.Fatal(err)
	}
	ra, _ := parse.Parse(src)
	rng, err := eval.EvalRange(ra, target)
	if err != nil {
		t.Fatal(err)
	}
	if scalar.Root.Kind != rng.Root.Kind || scalar.Root.Dim != rng.Root.Dim ||
		scalar.Root.Target != rng.Root.Target {
		t.Fatalf("metadata mismatch: %+v vs %+v", scalar.Root, rng.Root)
	}
	want := exactRat(t, scalar.Root.ValueBase)
	if exactRat(t, rng.Root.LowerBase).Cmp(want) != 0 ||
		exactRat(t, rng.Root.UpperBase).Cmp(want) != 0 {
		t.Fatalf("point interval = [%s,%s], want both %s",
			rng.Root.LowerBase.Exact, rng.Root.UpperBase.Exact, want)
	}
	if exactRat(t, rng.Root.Lower).Cmp(exactRat(t, scalar.Root.Value)) != 0 ||
		exactRat(t, rng.Root.Upper).Cmp(exactRat(t, scalar.Root.Value)) != 0 {
		t.Fatalf("target interval = [%s,%s], want both %s",
			rng.Root.Lower.Exact, rng.Root.Upper.Exact, scalar.Root.Value.Exact)
	}
	if len(scalar.Steps) != len(rng.Steps) {
		t.Fatalf("steps %d vs %d", len(scalar.Steps), len(rng.Steps))
	}
}

func TestRangeStepsCarryBothBounds(t *testing.T) {
	r := evRange(t, "[1,2] m * [3,4] m", "")
	if len(r.Steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(r.Steps))
	}
	for i, s := range r.Steps {
		if s.Lower.Exact == "" || s.Upper.Exact == "" {
			t.Fatalf("step %d missing bounds: %+v", i, s)
		}
		if exactRat(t, s.Lower).Cmp(exactRat(t, s.Upper)) > 0 {
			t.Fatalf("step %d inverted: [%s,%s]", i, s.Lower.Exact, s.Upper.Exact)
		}
	}
	// [1,2] m * [3,4] m = [3,8] m^2。
	if exactRat(t, r.Root.LowerBase).Cmp(rat("3")) != 0 ||
		exactRat(t, r.Root.UpperBase).Cmp(rat("8")) != 0 {
		t.Fatalf("area = [%s,%s], want [3,8]", r.Root.LowerBase.Exact, r.Root.UpperBase.Exact)
	}
}

func TestRangeErrorSpan(t *testing.T) {
	// 维度不相容的错误区间是整个最小二元节点。
	a, _ := parse.Parse("[1,2] m + [1,2] kg")
	_, err := eval.EvalRange(a, "")
	ee := err.(eval.Error)
	if ee.Code != eval.ErrDimMismatch {
		t.Fatalf("code = %s", ee.Code)
	}
	if string([]rune("[1,2] m + [1,2] kg")[ee.Pos:ee.End+1]) != "[1,2] m + [1,2] kg" {
		t.Fatalf("span = [%d,%d]", ee.Pos, ee.End)
	}

	// 绝对温度不能相乘，规则与标量入口一致。
	expectRangeErr(t, "[20,21] C * 2", "", eval.ErrAbsMul)

	// 区间字面量仍不允许走标量入口。
	expectErr(t, "[1,2] m", "", "RANGE_REQUIRES_RANGE_EVAL")
}

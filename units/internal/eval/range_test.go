package eval_test

import (
	"testing"

	"units/internal/eval"
	"units/internal/parse"
)

func evRange(t *testing.T, src, target string) *eval.RangeResult {
	t.Helper()
	r, err := eval.EvalRange(mustParse(t, src), target)
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

func checkRange(t *testing.T, r *eval.RangeResult, wantLo, wantHi string) {
	t.Helper()
	lo := exactRat(t, r.Root.LowerBase)
	hi := exactRat(t, r.Root.UpperBase)
	if lo.Cmp(rat(wantLo)) != 0 || hi.Cmp(rat(wantHi)) != 0 {
		t.Fatalf("base range = [%s, %s], want [%s, %s]", lo.String(), hi.String(), wantLo, wantHi)
	}
	if lo.Cmp(hi) > 0 {
		t.Fatalf("inverted bounds: [%s, %s]", lo.String(), hi.String())
	}
}

func TestRangeSubtraction(t *testing.T) {
	// [10,20] m - [3,4] m = [6,17] m（朴素端点相减会错成 [7,16]）。
	r := evRange(t, "[10,20] m - [3,4] m", "")
	checkRange(t, r, "6", "17")
	if r.Root.TargetSym != "m" {
		t.Fatalf("target = %s", r.Root.TargetSym)
	}
	// 目标单位同样给出正确上下界：[600,1700] cm。
	r = evRange(t, "[10,20] m - [3,4] m", "cm")
	if r.Root.Lower.Exact != "600" || r.Root.Upper.Exact != "1700" {
		t.Fatalf("cm range = [%s,%s]", r.Root.Lower.Exact, r.Root.Upper.Exact)
	}
	// [3,4] m - [10,20] m = [-17,-6] m，负数下界。
	r = evRange(t, "[3,4] m - [10,20] m", "")
	checkRange(t, r, "-17", "-6")
}

func TestRangeMulDivNegative(t *testing.T) {
	// 含负值乘法必须枚举四个端点：[-2,-1] * [3,4] = [-8,-3]。
	r := evRange(t, "[-2,-1] * [3,4]", "")
	checkRange(t, r, "-8", "-3")
	// 两端均含负：[-2,3] * [-4,1] = [-12,8]。
	r = evRange(t, "[-2,3] * [-4,1]", "")
	checkRange(t, r, "-12", "8")
	// 含负值除法：[-2,-1] / [3,4] = [-2/3,-1/4]。
	r = evRange(t, "[-2,-1] / [3,4]", "")
	checkRange(t, r, "-2/3", "-1/4")
	// 跨负的被除数除以正除数：[-2,4] / [1,2] = [-2,4]。
	r = evRange(t, "[-2,4] / [1,2]", "")
	checkRange(t, r, "-2", "4")
}

func TestRangeUnaryMinusSwapsBounds(t *testing.T) {
	r := evRange(t, "-[-3,2]", "")
	checkRange(t, r, "-2", "3")
	// 负的温差区间：-[1,3] dC = [-3,-1] dK。
	r = evRange(t, "-[1,3] dC", "")
	checkRange(t, r, "-3", "-1")
}

func TestRangeCelsiusFahrenheitDelta(t *testing.T) {
	// README 的例子：[20,21]°C - [68,86]°F
	// °C 端点基准：[293.15,294.15]；°F 端点基准：[293.15,303.15]
	// 温差区间 = [-10,1] K（朴素端点会得到倒置的 [0,-9]）。
	r := evRange(t, "[20,21] C - [68,86] F", "")
	if r.Root.Kind != "delta" {
		t.Fatalf("kind = %s, want delta", r.Root.Kind)
	}
	checkRange(t, r, "-10", "1")
	// 换算到 dF：[-18, 9/5]。
	if r2 := evRange(t, "[20,21] C - [68,86] F", "dF"); r2.Root.Lower.Exact != "-18" || r2.Root.Upper.Exact != "9/5" {
		t.Fatalf("dF range = [%s,%s], want [-18,9/5]", r2.Root.Lower.Exact, r2.Root.Upper.Exact)
	}
	// 绝对温度区间本身换算保持次序：[32,212] °F = [0,100] °C。
	r = evRange(t, "[32,212] F", "C")
	if r.Root.Lower.Exact != "0" || r.Root.Upper.Exact != "100" {
		t.Fatalf("F->C = [%s,%s]", r.Root.Lower.Exact, r.Root.Upper.Exact)
	}
	// 绝对 ± 温差：[0,10] K + [1,2] dK = [1,12] K。
	r = evRange(t, "[0,10] K + [1,2] dK", "")
	if r.Root.Kind != "absolute" {
		t.Fatalf("kind = %s", r.Root.Kind)
	}
	checkRange(t, r, "1", "12")
}

func TestRangeDivisionByZero(t *testing.T) {
	// 除数区间跨零：必须报错而不是返回有限值，错误定位在除法子表达式。
	a := mustParse(t, "1 / [-1,1]")
	_, err := eval.EvalRange(a, "")
	ee, ok := err.(eval.Error)
	if !ok || ee.Code != eval.ErrDivZeroRange {
		t.Fatalf("err = %v", err)
	}
	if span := "1 / [-1,1]"; string([]rune(span)[ee.Pos:ee.End+1]) != span {
		t.Fatalf("error span = %q", span)
	}
	// 退化为 [0,0] 的除数同样按区间零除拒绝。
	expectRangeErr(t, "1 / [0,0]", "", eval.ErrDivZeroRange)
	// 嵌套时定位最小子表达式 "(1 / [-2,2])"。
	expectRangeErr(t, "2 * (1 / [-2,2])", "", eval.ErrDivZeroRange)
	// 除数区间贴零但不包含 0：[1,2]/[2,4] = [1/4,1]，合法。
	r := evRange(t, "[1,2] / [2,4]", "")
	checkRange(t, r, "1/4", "1")
}

func TestRangeStepsCarryBothBounds(t *testing.T) {
	r := evRange(t, "[10,20] m - [3,4] m", "")
	wantSrc := []string{"[10,20] m", "[3,4] m", "[10,20] m - [3,4] m"}
	if len(r.Steps) != len(wantSrc) {
		t.Fatalf("steps = %d, want %d", len(r.Steps), len(wantSrc))
	}
	for i, s := range r.Steps {
		if s.Src != wantSrc[i] {
			t.Fatalf("step %d src = %q", i, s.Src)
		}
		if s.Lower.Exact == "" || s.Upper.Exact == "" {
			t.Fatalf("step %d missing bounds: %+v", i, s)
		}
	}
	leaf := r.Steps[0]
	if leaf.Lower.Exact != "10" || leaf.Upper.Exact != "20" {
		t.Fatalf("leaf bounds = [%s,%s]", leaf.Lower.Exact, leaf.Upper.Exact)
	}
	bin := r.Steps[2]
	if bin.Lower.Exact != "6" || bin.Upper.Exact != "17" {
		t.Fatalf("binary bounds = [%s,%s]", bin.Lower.Exact, bin.Upper.Exact)
	}
}

func TestRangeScalarMixingAndEntries(t *testing.T) {
	// 标量与区间混合：[2,4] * 3 = [6,12]。
	r := evRange(t, "[2,4] * 3", "")
	checkRange(t, r, "6", "12")
	// 纯标量走区间入口：退化为单点开区间。
	r = evRange(t, "6 m / 3 s", "")
	checkRange(t, r, "2", "2")
	if r.Root.Lower.Exact != r.Root.Upper.Exact {
		t.Fatalf("degenerate range = [%s,%s]", r.Root.Lower.Exact, r.Root.Upper.Exact)
	}
	// 含区间字面量的 AST 仍不允许走标量入口。
	expectErr(t, "[1,2] m + 3 m", "", "RANGE_REQUIRES_RANGE_EVAL")
}

func TestRangeKindRulesStillEnforced(t *testing.T) {
	expectRangeErr(t, "[20,21] C * 2", "", eval.ErrAbsMul)
	expectRangeErr(t, "[20,21] C + [1,2] K", "", eval.ErrAbsAdd)
	expectRangeErr(t, "[1,2] dC - [0,1] C", "", eval.ErrDeltaSubAbs)
	expectRangeErr(t, "[1,2] m + [1,2] kg", "", eval.ErrDimMismatch)
	// 温差结果不能用绝对温度单位表示。
	expectRangeErr(t, "[20,21] C - [0,1] C", "C", eval.ErrTargetKind)
	// 未知目标单位。
	expectRangeErr(t, "[1,2] m", "zzz", eval.ErrUnknownTarget)
}

func TestRangeLowerNotGreaterThanUpperParsed(t *testing.T) {
	if _, err := parse.Parse("[5,1]"); err == nil {
		t.Fatal("parser should reject lower > upper")
	}
}

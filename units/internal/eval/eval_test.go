package eval_test

import (
	"math/big"
	"strings"
	"testing"

	"units/internal/eval"
	"units/internal/parse"
	"units/internal/phys"
)

func mustParse(t *testing.T, src string) *parse.AST {
	t.Helper()
	a, err := parse.Parse(src)
	if err != nil {
		t.Fatalf("parse %q: %v", src, err)
	}
	return a
}

func ev(t *testing.T, src, target string) *eval.Result {
	t.Helper()
	r, err := eval.Eval(mustParse(t, src), target)
	if err != nil {
		t.Fatalf("eval %q: %v", src, err)
	}
	return r
}

func expectErr(t *testing.T, src, target, code string) {
	t.Helper()
	a, perr := parse.Parse(src)
	if perr != nil {
		if pe, ok := perr.(parse.Error); ok && pe.Code == code {
			return
		}
		t.Fatalf("expected error %s for %q, got parse error %v", code, src, perr)
	}
	_, err := eval.Eval(a, target)
	if err == nil {
		t.Fatalf("expected error %s for %q, got success", code, src)
	}
	if ee, ok := err.(eval.Error); !ok || ee.Code != code {
		t.Fatalf("expected error %s for %q, got %v", code, src, err)
	}
}

func rat(s string) *big.Rat {
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		panic("bad rat " + s)
	}
	return r
}

func exactRat(t *testing.T, v eval.RatView) *big.Rat {
	t.Helper()
	r, ok := new(big.Rat).SetString(v.Exact)
	if !ok {
		t.Fatalf("bad exact rational: %q", v.Exact)
	}
	return r
}

func TestSpecExamples(t *testing.T) {
	// 20°C × 2：绝对温度不能乘。
	expectErr(t, "20 C × 2", "", eval.ErrAbsMul)

	// 两个绝对温度相加非法。
	expectErr(t, "20 C + 300 K", "", eval.ErrAbsAdd)

	// 温差 - 绝对 非法。
	expectErr(t, "10 dC - 5 C", "", eval.ErrDeltaSubAbs)

	// 绝对 - 绝对 = 温差；20°C - 0°C = 20 dK（K 基准），按 dF 输出 = 36 dF。
	r := ev(t, "20 C - 0 C", "dF")
	if r.Root.Kind != "delta" || exactRat(t, r.Root.ValueBase).Cmp(rat("20")) != 0 {
		t.Fatalf("abs-abs base = %+v, want 20 delta", r.Root)
	}
	if r.Root.Value.Exact != "36" {
		t.Fatalf("20 dK in dF = %s, want 36", r.Root.Value.Exact)
	}

	// K = C + 27315/100。
	r = ev(t, "0 C", "K")
	if r.Root.Value.Exact != "5463/20" { // 273.15 = 5463/20
		t.Fatalf("0 C in K = %s, want 5463/20", r.Root.Value.Exact)
	}
	r = ev(t, "100 C", "K")
	if r.Root.Value.Exact != "7463/20" {
		t.Fatalf("100 C in K = %s, want 7463/20", r.Root.Value.Exact)
	}

	// K = (F - 32) × 5/9 + 27315/100：32°F = 273.15 K，212°F = 373.15 K。
	r = ev(t, "32 F", "K")
	if r.Root.Value.Exact != "5463/20" {
		t.Fatalf("32 F in K = %s, want 5463/20", r.Root.Value.Exact)
	}
	r = ev(t, "212 F", "C")
	if r.Root.Value.Exact != "100" {
		t.Fatalf("212 F in C = %s, want 100", r.Root.Value.Exact)
	}

	// 华氏温差按 5/9 折算：9 dF = 5 dK。
	r = ev(t, "9 dF", "dK")
	if r.Root.Value.Exact != "5" {
		t.Fatalf("9 dF in dK = %s, want 5", r.Root.Value.Exact)
	}

	// 绝对 + 温差 = 绝对：0 K + 5 dF = 25/9 K。
	r = ev(t, "0 K + 9 dF", "K")
	if r.Root.Kind != "absolute" || r.Root.Value.Exact != "5" {
		t.Fatalf("0 K + 9 dF = %s %s, want 5 absolute K", r.Root.Value.Exact, r.Root.Kind)
	}

	// 温差 + 绝对 = 绝对。
	r = ev(t, "100 dC + 27315/100 K", "C")
	if r.Root.Value.Exact != "100" {
		t.Fatalf("100 dC + 273.15 K in C = %s, want 100", r.Root.Value.Exact)
	}
}

func TestLengthMassTime(t *testing.T) {
	// 1 m + 2 cm = 1.02 m = 51/50 m。
	r := ev(t, "1 m + 2 cm", "m")
	if r.Root.Value.Exact != "51/50" {
		t.Fatalf("1m+2cm = %s m, want 51/50", r.Root.Value.Exact)
	}
	// 1 kg - 100 g = 0.9 kg。
	r = ev(t, "1 kg - 100 g", "kg")
	if r.Root.Value.Exact != "9/10" {
		t.Fatalf("1kg-100g = %s kg, want 9/10", r.Root.Value.Exact)
	}
	// 2 min = 120 s。
	r = ev(t, "2 min", "s")
	if r.Root.Value.Exact != "120" {
		t.Fatalf("2 min = %s s, want 120", r.Root.Value.Exact)
	}
	// 速度：6 m / 3 s = 2 m/s（自动目标给基准导出单位）。
	r = ev(t, "6 m / 3 s", "")
	if r.Root.Value.Exact != "2" || r.Root.Dim.L != 1 || r.Root.Dim.T != -1 {
		t.Fatalf("speed = %+v", r.Root)
	}
	if r.Root.Target != "base" || !strings.Contains(r.Root.TargetSym, "m") {
		t.Fatalf("auto target symbol = %q", r.Root.TargetSym)
	}
	// 面积：2 m * 300 cm = 6 m²。
	r = ev(t, "2 m * 300 cm", "")
	if r.Root.Value.Exact != "6" || r.Root.Dim.L != 2 {
		t.Fatalf("area = %+v", r.Root)
	}
}

func TestIncompatible(t *testing.T) {
	expectErr(t, "1 m + 1 kg", "", eval.ErrDimMismatch)
	expectErr(t, "1 s - 1 m", "", eval.ErrDimMismatch)
	// 绝对温度与无量纲普通量相加：维度不同 => 维度不相容。
	expectErr(t, "1 K + 1", "", eval.ErrDimMismatch)
	// 绝对温度的乘法在到达加法前就被拒绝。
	expectErr(t, "1 K + 1 K * 1", "", eval.ErrAbsMul)
	// 温差 - 绝对 非法。
	expectErr(t, "(1 K - 0 K) - 1 K", "", eval.ErrDeltaSubAbs)
	// 除以零。
	expectErr(t, "1 m / (2 - 2)", "", eval.ErrDivZero)
}

func TestParseErrors(t *testing.T) {
	expectErr(t, "", "", parse.ErrEmpty)
	expectErr(t, "1 m +", "", parse.ErrSyntax)
	expectErr(t, "(1 m", "", parse.ErrSyntax)
	expectErr(t, "1 m)", "", parse.ErrSyntax)
	expectErr(t, "1 xyz", "", parse.ErrUnknown)
	expectErr(t, "1 2", "", parse.ErrSyntax)
	expectErr(t, "1 / 0", "", eval.ErrDivZero)
}

func TestMinimalErrorSpan(t *testing.T) {
	a, err := parse.Parse("1 m + (2 kg + 3 s)")
	if err != nil {
		t.Fatal(err)
	}
	_, err = eval.Eval(a, "")
	ee, ok := err.(eval.Error)
	if !ok {
		t.Fatal("want eval error")
	}
	// 最小区间应覆盖 "2 kg + 3 s"，而不是整个表达式。
	if got := []rune("1 m + (2 kg + 3 s)")[ee.Pos : ee.End+1]; string(got) != "2 kg + 3 s" {
		t.Fatalf("error span = %q", string(got))
	}
	// 绝对相乘的区间是乘法节点本身。
	a, _ = parse.Parse("(1 K) * 2")
	_, err = eval.Eval(a, "")
	ee = err.(eval.Error)
	if string([]rune("(1 K) * 2")[ee.Pos:ee.End+1]) != "(1 K) * 2" {
		t.Fatalf("mul span = [%d,%d]", ee.Pos, ee.End)
	}
}

func TestStepsRecordEveryNode(t *testing.T) {
	r := ev(t, "2 m * (3 m + 4 m)", "")
	// 节点：2m, 3m, 4m, 3m+4m 二元, 括号 group, 顶层乘法 —— 共 6 个。
	if len(r.Steps) != 6 {
		t.Fatalf("steps = %d, want 6", len(r.Steps))
	}
	for _, s := range r.Steps {
		if s.DimName == "" || s.Kind == "" || s.Value.Exact == "" {
			t.Fatalf("incomplete step: %+v", s)
		}
	}
}

func TestTargetValidation(t *testing.T) {
	// 用长度单位表示质量结果。
	expectErr(t, "1 kg", "m", eval.ErrTargetDim)
	// 用绝对温度单位表示温差。
	expectErr(t, "1 K - 0 K", "K", eval.ErrTargetKind)
	// 用温差单位表示绝对温度。
	expectErr(t, "1 K", "dK", eval.ErrTargetKind)
	// 未知目标单位。
	expectErr(t, "1 m", "zzz", eval.ErrUnknownTarget)
}

// 帮助函数：比较 big.Rat。
func init() {
	_ = phys.Dim{}
}

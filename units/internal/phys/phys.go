// Package phys 定义用约分有理数表示的物理量：
// 长度、质量、时间、温度四个维度的向量，以及“普通量 / 温差 / 绝对温度”种类。
package phys

import (
	"math/big"
	"strings"
)

// Kind 是数量种类。绝对温度与温差在维度上都是温度^1，
// 但允许参与的运算完全不同，必须区分。
type Kind int

const (
	Normal   Kind = iota // 普通量（包括复合维度）
	Delta                // 温差 dK/dC/dF
	Absolute             // 绝对温度 K/°C/°F
)

func (k Kind) String() string {
	switch k {
	case Delta:
		return "温差"
	case Absolute:
		return "绝对温度"
	default:
		return "普通量"
	}
}

// JSON 名称，供 API 使用。
func (k Kind) JSON() string {
	switch k {
	case Delta:
		return "delta"
	case Absolute:
		return "absolute"
	default:
		return "normal"
	}
}

// Dim 是 (长度, 质量, 时间, 温度) 四维指数向量。
type Dim struct {
	L int `json:"l"`
	M int `json:"m"`
	T int `json:"t"`
	Q int `json:"q"`
}

func (d Dim) Add(e Dim) Dim {
	return Dim{d.L + e.L, d.M + e.M, d.T + e.T, d.Q + e.Q}
}

func (d Dim) Sub(e Dim) Dim {
	return Dim{d.L - e.L, d.M - e.M, d.T - e.T, d.Q - e.Q}
}

func (d Dim) Equal(e Dim) bool { return d == e }

func (d Dim) IsZero() bool { return d == Dim{} }

var dimNames = [4]string{"长度", "质量", "时间", "温度"}

// String 输出人类可读的维度名，如“长度/时间^2”、“无量纲”。
func (d Dim) String() string {
	at := func(i int) int {
		switch i {
		case 0:
			return d.L
		case 1:
			return d.M
		case 2:
			return d.T
		default:
			return d.Q
		}
	}
	part := func(name string, e int) string {
		if e == 1 {
			return name
		}
		return name + "^" + itoa(e)
	}
	var pos, neg []string
	for i := 0; i < 4; i++ {
		e := at(i)
		switch {
		case e > 0:
			pos = append(pos, part(dimNames[i], e))
		case e < 0:
			neg = append(neg, part(dimNames[i], -e))
		}
	}
	if len(pos) == 0 && len(neg) == 0 {
		return "无量纲"
	}
	if len(pos) == 0 {
		if len(neg) == 1 {
			return "1/" + neg[0]
		}
		return "1/(" + strings.Join(neg, "·") + ")"
	}
	s := strings.Join(pos, "·")
	if len(neg) == 1 {
		s += "/" + neg[0]
	} else if len(neg) > 1 {
		s += "/(" + strings.Join(neg, "·") + ")"
	}
	return s
}

// Vector 返回维度向量，供 JSON 使用。
func (d Dim) Vector() [4]int { return [4]int{d.L, d.M, d.T, d.Q} }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// ConvKind 决定单位与基准单位之间如何换算。
type ConvKind int

const (
	Linear   ConvKind = iota // 基准 = 值 × Factor
	Celsius                  // K = C + 27315/100
	Fahrenheit               // K = (F - 32) × 5/9 + 27315/100
)

// Unit 描述一个受支持的单位。
type Unit struct {
	Name   string     // 规范名（JSON 与目标单位选择用）
	Sym    string     // 展示符号
	Dim    Dim        // 维度
	Kind   Kind       // Normal / Delta / Absolute
	Conv   ConvKind   // 换算方式
	Factor *big.Rat   // Linear 时相对基准 (m, kg, s, K) 的倍率
}

var (
	r100  = big.NewRat(1, 100)
	r1000 = big.NewRat(1, 1000)
	r60   = big.NewRat(60, 1)
	r5_9  = big.NewRat(5, 9)
	r9_5  = big.NewRat(9, 5)
	r32   = big.NewRat(32, 1)
	r273  = big.NewRat(27315, 100)
)

func linear(name, sym string, d Dim, k Kind, f *big.Rat) *Unit {
	return &Unit{Name: name, Sym: sym, Dim: d, Kind: k, Conv: Linear, Factor: new(big.Rat).Set(f)}
}

// units 中同一物理单位可有多个可接受的拼写（如 C 与 °C）。
var units = func() []*Unit {
	dL := Dim{L: 1}
	dM := Dim{M: 1}
	dT := Dim{T: 1}
	dQ := Dim{Q: 1}
	return []*Unit{
		linear("m", "m", dL, Normal, big.NewRat(1, 1)),
		linear("cm", "cm", dL, Normal, r100),
		linear("kg", "kg", dM, Normal, big.NewRat(1, 1)),
		linear("g", "g", dM, Normal, r1000),
		linear("s", "s", dT, Normal, big.NewRat(1, 1)),
		linear("min", "min", dT, Normal, r60),

		linear("K", "K", dQ, Absolute, big.NewRat(1, 1)),
		{Name: "C", Sym: "°C", Dim: dQ, Kind: Absolute, Conv: Celsius},
		{Name: "F", Sym: "°F", Dim: dQ, Kind: Absolute, Conv: Fahrenheit},

		linear("dK", "dK", dQ, Delta, big.NewRat(1, 1)),
		linear("dC", "dC", dQ, Delta, big.NewRat(1, 1)),
		linear("dF", "dF", dQ, Delta, r5_9),
	}
}()

// ByName 按输入名查找单位，支持 °C/°F 拼写。
func ByName(name string) (*Unit, bool) {
	for _, u := range units {
		if u.Name == name {
			return u, true
		}
	}
	switch name {
	case "°C", "℃":
		return ByName("C")
	case "°F", "℉":
		return ByName("F")
	}
	return nil, false
}

// AllUnits 返回单位表副本（供目标单位下拉等使用）。
func AllUnits() []*Unit {
	out := make([]*Unit, len(units))
	copy(out, units)
	return out
}

// ToBase 把以该单位表示的有理数 n 换算到基准单位表示。
func (u *Unit) ToBase(n *big.Rat) *big.Rat {
	switch u.Conv {
	case Celsius:
		return new(big.Rat).Add(n, r273)
	case Fahrenheit:
		// (n - 32) * 5/9 + 27315/100
		t := new(big.Rat).Sub(n, r32)
		t.Mul(t, r5_9)
		return t.Add(t, r273)
	default:
		return new(big.Rat).Mul(n, u.Factor)
	}
}

// FromBase 把基准单位表示的 n 换算到该单位。
func (u *Unit) FromBase(n *big.Rat) *big.Rat {
	switch u.Conv {
	case Celsius:
		return new(big.Rat).Sub(n, r273)
	case Fahrenheit:
		// (n - 27315/100) * 9/5 + 32
		t := new(big.Rat).Sub(n, r273)
		t.Mul(t, r9_5)
		return t.Add(t, r32)
	default:
		return new(big.Rat).Quo(n, u.Factor)
	}
}

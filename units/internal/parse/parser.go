package parse

import (
	"fmt"
	"math/big"
	"strings"

	"units/internal/phys"
)

// 错误码：词法/语法阶段。
const (
	ErrLex     = "LEX_ERROR"
	ErrEmpty   = "EMPTY_EXPRESSION"
	ErrSyntax  = "SYNTAX_ERROR"
	ErrUnknown = "UNKNOWN_UNIT"
)

// Error 携带最小表达式区间 [Pos, End]（rune 偏移，闭区间）。
type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Pos     int    `json:"pos"`
	End     int    `json:"end"`
}

func (e Error) Error() string { return fmt.Sprintf("%s: %s (%d:%d)", e.Code, e.Message, e.Pos, e.End) }

type parser struct {
	src   string
	r     []rune
	toks  []token
	i     int
	depth int
}

// Parse 解析整个表达式。
func Parse(src string) (*AST, error) {
	if strings.TrimSpace(src) == "" {
		return nil, Error{Code: ErrEmpty, Message: "表达式为空", Pos: 0, End: len([]rune(src))}
	}
	toks, err := tokenize(src)
	if err != nil {
		return nil, err
	}
	p := &parser{src: src, r: []rune(src), toks: toks}
	root, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tEOF {
		t := p.peek()
		return nil, p.errf(t.pos, t.end, "多余的输入 %q，检查是否缺少运算符", t.lit)
	}
	return &AST{Root: root, Source: src}, nil
}

func (p *parser) peek() token    { return p.toks[p.i] }
func (p *parser) advance() token { t := p.toks[p.i]; p.i++; return t }

func (p *parser) errf(pos, end int, format string, args ...any) Error {
	return Error{Code: ErrSyntax, Message: fmt.Sprintf(format, args...), Pos: pos, End: end}
}

func (p *parser) srcOf(pos, end int) string {
	if end >= len(p.r) {
		end = len(p.r) - 1
	}
	if pos > end || pos >= len(p.r) {
		return ""
	}
	return string(p.r[pos : end+1])
}

func (p *parser) parseExpr() (Node, error) {
	left, err := p.parseTerm()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tPlus || p.peek().kind == tMinus {
		op := p.advance()
		right, err := p.parseTerm()
		if err != nil {
			return nil, err
		}
		left = &BinaryNode{
			baseNode: p.mkNode(left.Pos(), right.End(), p.depth),
			Op:       op.kind.toByte(),
			Left:     left, Right: right, OpPos: op.pos,
		}
	}
	return left, nil
}

func (p *parser) parseTerm() (Node, error) {
	left, err := p.parseFactor()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == tStar || p.peek().kind == tSlash {
		op := p.advance()
		right, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		left = &BinaryNode{
			baseNode: p.mkNode(left.Pos(), right.End(), p.depth),
			Op:       op.kind.toByte(),
			Left:     left, Right: right, OpPos: op.pos,
		}
	}
	return left, nil
}

func (p *parser) parseFactor() (Node, error) {
	t := p.peek()
	switch t.kind {
	case tMinus:
		p.advance()
		child, err := p.parseFactor()
		if err != nil {
			return nil, err
		}
		return &UnaryNode{baseNode: p.mkNode(t.pos, child.End(), p.depth), Child: child}, nil
	case tLParen:
		p.advance()
		p.depth++
		inner, err := p.parseExpr()
		p.depth--
		if err != nil {
			return nil, err
		}
		cl := p.peek()
		if cl.kind != tRParen {
			return nil, p.errf(t.pos, t.pos, "左括号缺少匹配的右括号")
		}
		p.advance()
		return &GroupNode{baseNode: p.mkNode(t.pos, cl.pos, p.depth), Inner: inner}, nil
	case tNum:
		return p.parseNumber()
	case tLBracket:
		return p.parseRangeNumber()
	case tEOF:
		return nil, p.errf(t.pos, t.pos, "表达式在这里结束，但还缺少一个操作数")
	default:
		return nil, p.errf(t.pos, t.end, "这里应为数字或左括号，而不是 %q", t.lit)
	}
}

func (p *parser) parseNumber() (Node, error) {
	nt := p.advance()
	val, _ := new(big.Rat).SetString(nt.lit)
	end := nt.end
	var unit *phys.Unit
	if p.peek().kind == tIdent {
		ut := p.advance()
		u, ok := phys.ByName(ut.lit)
		if !ok {
			return nil, Error{
				Code:    ErrUnknown,
				Message: fmt.Sprintf("未知单位 %q（支持 m/cm、kg/g、s/min、K/°C/°F、dK/dC/dF）", ut.lit),
				Pos:     ut.pos, End: ut.end,
			}
		}
		unit = u
		end = ut.end
	}
	return &NumberNode{
		baseNode: p.mkNode(nt.pos, end, p.depth),
		Value:    val,
		Unit:     unit,
	}, nil
}

func (p *parser) mkNode(pos, end, depth int) baseNode {
	return baseNode{src: p.srcOf(pos, end), pos: pos, end: end, depth: depth}
}

func (k tokenKind) toByte() byte {
	switch k {
	case tPlus:
		return '+'
	case tMinus:
		return '-'
	case tStar:
		return '*'
	default:
		return '/'
	}
}

// parseRangeNumber accepts a pair of ordered rational bounds with one shared unit.
func (p *parser) parseRangeNumber() (Node, error) {
	open := p.advance()
	read := func() (*big.Rat, error) {
		negative := false
		if p.peek().kind == tMinus {
			p.advance()
			negative = true
		}
		if p.peek().kind != tNum {
			t := p.peek()
			return nil, p.errf(t.pos, t.end, "区间端点必须是有理数")
		}
		t := p.advance()
		v, _ := new(big.Rat).SetString(t.lit)
		if negative {
			v.Neg(v)
		}
		return v, nil
	}
	lo, err := read()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != tComma {
		t := p.peek()
		return nil, p.errf(t.pos, t.end, "区间需要逗号分隔上下界")
	}
	p.advance()
	hi, err := read()
	if err != nil {
		return nil, err
	}
	close := p.peek()
	if close.kind != tRBracket {
		return nil, p.errf(close.pos, close.end, "区间缺少右方括号")
	}
	p.advance()
	if lo.Cmp(hi) > 0 {
		return nil, p.errf(open.pos, close.end, "区间下界大于上界")
	}
	end := close.end
	var unit *phys.Unit
	if p.peek().kind == tIdent {
		t := p.advance()
		u, ok := phys.ByName(t.lit)
		if !ok {
			return nil, Error{Code: ErrUnknown, Message: fmt.Sprintf("未知单位 %q", t.lit), Pos: t.pos, End: t.end}
		}
		unit, end = u, t.end
	}
	return &NumberNode{baseNode: p.mkNode(open.pos, end, p.depth), Value: lo, Upper: hi, Unit: unit}, nil
}

package parse

import (
	"math/big"

	"units/internal/phys"
)

// Node 是语法树节点。Pos/End 为 rune 偏移闭区间，Src 是该子表达式原文。
type Node interface {
	Pos() int
	End() int
	Src() string
	node()
}

type baseNode struct {
	src   string
	pos   int
	end   int
	depth int // 括号/一元负号造成的嵌套深度，供前端缩进展示
}

func (n *baseNode) Pos() int    { return n.pos }
func (n *baseNode) End() int    { return n.end }
func (n *baseNode) Src() string { return n.src }
func (n *baseNode) Depth() int  { return n.depth }

// NumberNode 是有理数字面量，Unit 为 nil 时表示无单位（无量纲）。
type NumberNode struct {
	baseNode
	Value *big.Rat
	Upper *big.Rat // nil for a scalar literal; non-nil for [lower,upper]
	Unit  *phys.Unit
}

// UnaryNode 是一元负号。
type UnaryNode struct {
	baseNode
	Child Node
}

// BinaryNode 是二元运算，Op 为 + - * / 之一。
type BinaryNode struct {
	baseNode
	Op    byte
	Left  Node
	Right Node
	OpPos int
}

// GroupNode 是括号表达式。
type GroupNode struct {
	baseNode
	Inner Node
}

func (*NumberNode) node() {}
func (*UnaryNode) node()  {}
func (*BinaryNode) node() {}
func (*GroupNode) node()  {}

// AST 是解析结果。
type AST struct {
	Root   Node
	Source string
}

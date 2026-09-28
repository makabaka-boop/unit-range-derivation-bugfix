package eval

import (
	"math/big"
	"units/internal/parse"
)

// EvalRange returns the range calculation response.
func EvalRange(a *parse.AST, target string) (map[string]any, error) {
	saved := map[*parse.NumberNode]*big.Rat{}
	collect(a.Root, saved)
	pick(a.Root, false, saved)
	lower, err := Eval(a, target)
	if err != nil {
		return nil, err
	}
	pick(a.Root, true, saved)
	upper, err := Eval(a, target)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"root": map[string]any{
			"kind": lower.Root.Kind, "dim": lower.Root.Dim,
			"target": lower.Root.Target, "targetSymbol": lower.Root.TargetSym,
			"lower": lower.Root.Value, "upper": upper.Root.Value,
		},
		"steps": lower.Steps,
	}, nil
}

func collect(n parse.Node, saved map[*parse.NumberNode]*big.Rat) {
	switch x := n.(type) {
	case *parse.NumberNode:
		if x.Upper != nil {
			saved[x] = x.Upper
		}
	case *parse.UnaryNode:
		collect(x.Child, saved)
	case *parse.GroupNode:
		collect(x.Inner, saved)
	case *parse.BinaryNode:
		collect(x.Left, saved)
		collect(x.Right, saved)
	}
}

func pick(n parse.Node, upper bool, saved map[*parse.NumberNode]*big.Rat) {
	switch x := n.(type) {
	case *parse.NumberNode:
		if hi, ok := saved[x]; ok {
			if upper {
				x.Value = hi
			}
			x.Upper = nil
		}
	case *parse.UnaryNode:
		pick(x.Child, upper, saved)
	case *parse.GroupNode:
		pick(x.Inner, upper, saved)
	case *parse.BinaryNode:
		pick(x.Left, upper, saved)
		pick(x.Right, upper, saved)
	}
}

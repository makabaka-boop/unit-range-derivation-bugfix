// Package parse 实现表达式的词法分析与递归下降解析。
//
// 文法：
//
//	expr    := term (('+' | '-') term)*
//	term    := factor (('*' | '/') factor)*
//	factor  := '-' factor | '(' expr ')' | number [unit]
//
// 有理数字面量支持整数、小数、分数（如 3、1.5、3/4）。
// 分数字面量中 '/' 两侧不能有空格，带空格的 a / b 一律视为除法。
package parse

import (
	"fmt"
	"math/big"
	"unicode"
)

type tokenKind int

const (
	tEOF tokenKind = iota
	tNum
	tIdent
	tPlus
	tMinus
	tStar
	tSlash
	tLParen
	tRParen
	tLBracket
	tRBracket
	tComma
)

type token struct {
	kind tokenKind
	lit  string
	// pos/end 均为 rune 偏移，end 是最后一个 rune 的位置（闭区间）
	pos int
	end int
}

type lexer struct {
	r []rune
	i int
}

func isDigit(ch rune) bool { return ch >= '0' && ch <= '9' }

func (lx *lexer) skipSpace() {
	for lx.i < len(lx.r) && unicode.IsSpace(lx.r[lx.i]) {
		lx.i++
	}
}

// numTail 在已读到数字或小数点的前提下，解析小数与“无空格分数”部分。
func (lx *lexer) numTail(start int) (token, error) {
	scanDigits := func(dot *bool) error {
		for lx.i < len(lx.r) && (isDigit(lx.r[lx.i]) || lx.r[lx.i] == '.') {
			if lx.r[lx.i] == '.' {
				if *dot {
					return lx.errf(lx.i, lx.i, "数字中出现了第二个小数点")
				}
				*dot = true
			}
			lx.i++
		}
		return nil
	}
	dot := false
	if err := scanDigits(&dot); err != nil {
		return token{}, err
	}
	// 紧贴的分数：1/2、3/7 等；要求 '/' 右侧不能有空格（左侧按扫描位置自然紧贴）。
	if lx.i < len(lx.r) && lx.r[lx.i] == '/' &&
		lx.i+1 < len(lx.r) && isDigit(lx.r[lx.i+1]) {
		lx.i++ // 跳过 '/'
		dot2 := false
		if err := scanDigits(&dot2); err != nil {
			return token{}, err
		}
	}
	lit := string(lx.r[start:lx.i])
	if _, ok := new(big.Rat).SetString(lit); !ok {
		return token{}, lx.errf(start, lx.i-1, "无法识别的有理数 %q", lit)
	}
	return token{kind: tNum, lit: lit, pos: start, end: lx.i - 1}, nil
}

func isIdentStart(ch rune) bool {
	return ch == '°' || ch == '℃' || ch == '℉' ||
		ch == 'μ' || unicode.IsLetter(ch)
}

func isIdentPart(ch rune) bool {
	return isIdentStart(ch) || unicode.IsDigit(ch)
}

func (lx *lexer) next() (token, error) {
	lx.skipSpace()
	if lx.i >= len(lx.r) {
		return token{kind: tEOF, pos: len(lx.r), end: len(lx.r)}, nil
	}
	start := lx.i
	ch := lx.r[lx.i]
	switch {
	case isDigit(ch):
		return lx.numTail(start)
	case ch == '.':
		if lx.i+1 < len(lx.r) && isDigit(lx.r[lx.i+1]) {
			return lx.numTail(start)
		}
		return token{}, lx.errf(start, start, "孤立的小数点")
	case isIdentStart(ch):
		lx.i++
		for lx.i < len(lx.r) && isIdentPart(lx.r[lx.i]) {
			lx.i++
		}
		return token{kind: tIdent, lit: string(lx.r[start:lx.i]), pos: start, end: lx.i - 1}, nil
	}
	lx.i++
	var k tokenKind
	switch ch {
	case '+', '＋':
		k = tPlus
	case '-', '－', '−':
		k = tMinus
	case '*', '×', '✕', '·':
		k = tStar
	case '/', '÷':
		k = tSlash
	case '(', '（':
		k = tLParen
	case ')', '）':
		k = tRParen
	case '[':
		k = tLBracket
	case ']':
		k = tRBracket
	case ',':
		k = tComma
	default:
		return token{}, lx.errf(start, start, "无法识别的字符 %q", string(ch))
	}
	return token{kind: k, lit: string(ch), pos: start, end: start}, nil
}

func (lx *lexer) errf(pos, end int, format string, args ...any) Error {
	return Error{Code: ErrLex, Message: fmt.Sprintf(format, args...), Pos: pos, End: end}
}

func tokenize(src string) ([]token, error) {
	lx := &lexer{r: []rune(src)}
	var toks []token
	for {
		t, err := lx.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.kind == tEOF {
			return toks, nil
		}
	}
}

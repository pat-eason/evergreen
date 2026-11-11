package api

import (
	"errors"
	"math"
	"strconv"
	"strings"
)

// EvalExpr evaluates a simple arithmetic expression (+ - * / and parentheses),
// the way budget spreadsheet cells do: "=(1368.23*2)+(3192.56*2)". A leading
// "=", "$" signs and thousands separators are ignored.
func EvalExpr(s string) (float64, error) {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "=")
	s = strings.NewReplacer("$", "", ",", "", " ", "").Replace(s)
	if s == "" {
		return 0, errors.New("empty expression")
	}
	p := &exprParser{s: s}
	v, err := p.expr()
	if err != nil {
		return 0, err
	}
	if p.i != len(p.s) {
		return 0, errors.New("unexpected character")
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, errors.New("invalid result")
	}
	return v, nil
}

type exprParser struct {
	s     string
	i     int
	depth int
}

func (p *exprParser) expr() (float64, error) {
	v, err := p.term()
	if err != nil {
		return 0, err
	}
	for p.i < len(p.s) && (p.s[p.i] == '+' || p.s[p.i] == '-') {
		op := p.s[p.i]
		p.i++
		r, err := p.term()
		if err != nil {
			return 0, err
		}
		if op == '+' {
			v += r
		} else {
			v -= r
		}
	}
	return v, nil
}

func (p *exprParser) term() (float64, error) {
	v, err := p.factor()
	if err != nil {
		return 0, err
	}
	for p.i < len(p.s) && (p.s[p.i] == '*' || p.s[p.i] == '/') {
		op := p.s[p.i]
		p.i++
		r, err := p.factor()
		if err != nil {
			return 0, err
		}
		if op == '*' {
			v *= r
		} else {
			if r == 0 {
				return 0, errors.New("division by zero")
			}
			v /= r
		}
	}
	return v, nil
}

func (p *exprParser) factor() (float64, error) {
	if p.i >= len(p.s) {
		return 0, errors.New("unexpected end")
	}
	switch c := p.s[p.i]; {
	case c == '-':
		p.i++
		v, err := p.factor()
		return -v, err
	case c == '+':
		p.i++
		return p.factor()
	case c == '(':
		p.depth++
		if p.depth > 32 {
			return 0, errors.New("too deeply nested")
		}
		p.i++
		v, err := p.expr()
		if err != nil {
			return 0, err
		}
		if p.i >= len(p.s) || p.s[p.i] != ')' {
			return 0, errors.New("missing )")
		}
		p.i++
		p.depth--
		return v, nil
	default:
		start := p.i
		for p.i < len(p.s) && (p.s[p.i] >= '0' && p.s[p.i] <= '9' || p.s[p.i] == '.') {
			p.i++
		}
		if start == p.i {
			return 0, errors.New("expected number")
		}
		return strconv.ParseFloat(p.s[start:p.i], 64)
	}
}

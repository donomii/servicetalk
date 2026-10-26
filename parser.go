package main

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

type NodeKind int

const (
	NodeList NodeKind = iota
	NodeSymbol
	NodeNumber
	NodeString
)

type Node struct {
	Kind     NodeKind `json:"k"`
	Num      float64  `json:"n,omitempty"`
	Str      string   `json:"s,omitempty"` // symbol name or string value
	Children []*Node  `json:"c,omitempty"`
	Line     int      `json:"l,omitempty"`
}

func (n *Node) String() string {
	switch n.Kind {
	case NodeNumber:
		if n.Num == float64(int64(n.Num)) {
			return strconv.FormatInt(int64(n.Num), 10)
		}
		return strconv.FormatFloat(n.Num, 'f', -1, 64)
	case NodeString:
		return fmt.Sprintf("%q", n.Str)
	case NodeSymbol:
		return n.Str
	case NodeList:
		parts := make([]string, len(n.Children))
		for i, c := range n.Children {
			parts[i] = c.String()
		}
		return "(" + strings.Join(parts, " ") + ")"
	}
	return "?"
}

type parser struct {
	src []rune
	pos int
	line int
}

// Parse parses a source string into a slice of top-level nodes.
func Parse(src string) ([]*Node, error) {
	p := &parser{src: []rune(src), line: 1}
	var nodes []*Node
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			break
		}
		n, err := p.expr()
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, n)
	}
	return nodes, nil
}

func (p *parser) peek() rune {
	if p.pos >= len(p.src) {
		return 0
	}
	return p.src[p.pos]
}

func (p *parser) next() rune {
	ch := p.src[p.pos]
	p.pos++
	if ch == '\n' {
		p.line++
	}
	return ch
}

func (p *parser) skipSpace() {
	for p.pos < len(p.src) {
		ch := p.peek()
		if ch == ';' {
			for p.pos < len(p.src) && p.src[p.pos] != '\n' {
				p.pos++
			}
		} else if unicode.IsSpace(ch) {
			p.next()
		} else {
			break
		}
	}
}

func (p *parser) expr() (*Node, error) {
	p.skipSpace()
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("unexpected EOF at line %d", p.line)
	}
	ch := p.peek()
	switch {
	case ch == '(':
		return p.list()
	case ch == '"':
		return p.str()
	case ch >= '0' && ch <= '9':
		return p.number()
	case ch == '-' && p.pos+1 < len(p.src) && p.src[p.pos+1] >= '0' && p.src[p.pos+1] <= '9':
		return p.number()
	default:
		return p.symbol()
	}
}

func (p *parser) list() (*Node, error) {
	line := p.line
	p.next() // consume '('
	var children []*Node
	for {
		p.skipSpace()
		if p.pos >= len(p.src) {
			return nil, fmt.Errorf("unclosed '(' at line %d", line)
		}
		if p.peek() == ')' {
			p.next()
			break
		}
		n, err := p.expr()
		if err != nil {
			return nil, err
		}
		children = append(children, n)
	}
	return &Node{Kind: NodeList, Children: children, Line: line}, nil
}

func (p *parser) str() (*Node, error) {
	line := p.line
	p.next() // consume '"'
	var sb strings.Builder
	for p.pos < len(p.src) {
		ch := p.next()
		if ch == '"' {
			break
		}
		if ch == '\\' && p.pos < len(p.src) {
			esc := p.next()
			switch esc {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case '"':
				sb.WriteRune('"')
			case '\\':
				sb.WriteRune('\\')
			default:
				sb.WriteRune('\\')
				sb.WriteRune(esc)
			}
		} else {
			sb.WriteRune(ch)
		}
	}
	return &Node{Kind: NodeString, Str: sb.String(), Line: line}, nil
}

func (p *parser) number() (*Node, error) {
	line := p.line
	start := p.pos
	if p.peek() == '-' {
		p.next()
	}
	for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.next()
	}
	if p.pos < len(p.src) && p.src[p.pos] == '.' {
		p.next()
		for p.pos < len(p.src) && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
			p.next()
		}
	}
	n, err := strconv.ParseFloat(string(p.src[start:p.pos]), 64)
	if err != nil {
		p.pos = start
		return p.symbol()
	}
	return &Node{Kind: NodeNumber, Num: n, Line: line}, nil
}

func isSymChar(ch rune) bool {
	if unicode.IsLetter(ch) || unicode.IsDigit(ch) {
		return true
	}
	return strings.ContainsRune("_:!?+-*/=<>%&|^~@#$.,\\", ch)
}

func (p *parser) symbol() (*Node, error) {
	line := p.line
	start := p.pos
	for p.pos < len(p.src) && isSymChar(p.peek()) {
		p.next()
	}
	if p.pos == start {
		return nil, fmt.Errorf("unexpected character %q at line %d", p.peek(), p.line)
	}
	return &Node{Kind: NodeSymbol, Str: string(p.src[start:p.pos]), Line: line}, nil
}

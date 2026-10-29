package main

// Smalltalk-syntax front end.
//
// Produces the same *Node tree the S-expression parser produces, so the
// evaluator in eval.go works unchanged.  Both syntaxes coexist:
//   - foo.smt   → S-expression syntax  (Parse, ParseClassSource)
//   - foo.st    → Smalltalk syntax     (ParseSmalltalk, ParseSmalltalkClassSource)
//
// The mapping is intentionally compact — enough Smalltalk to be recognisable,
// not enough to claim spec compliance.

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// ---- lexer ----

type stTokKind int

const (
	stEOF stTokKind = iota
	stIdent    // abc
	stKeyword  // abc:
	stNumber   // 42, 3.14
	stString   // 'hello'
	stBinop    // + - * / = < > <= >= ~= // ** , @ & %
	stAssign   // :=
	stCaret    // ^
	stLParen   // (
	stRParen   // )
	stLBracket // [
	stRBracket // ]
	stLBrace   // {  (dynamic array literal)
	stRBrace   // }
	stDot      // .
	stPipe     // |
	stBlockArg // :ident  (for [:x :y | ...])
	stSemi     // ;  (cascades)
	stSymbol   // #name, #'name', #+ — evaluates to String
	stObjId    // #@id — evaluates to ObjVal(id)
	stLitArray // #(  — literal-array open; elements are literals/symbols
	stChar     // $a — character literal; num holds the rune
)

type stToken struct {
	start int
	end int
	kind stTokKind
	text string
	num  float64
	line int
}

type stLexer struct {
	src  []rune
	pos  int
	line int
}

func newStLexer(src string) *stLexer {
	return &stLexer{src: []rune(src), line: 1}
}

func (l *stLexer) peekAt(off int) rune {
	if l.pos+off >= len(l.src) {
		return 0
	}
	return l.src[l.pos+off]
}

func (l *stLexer) advance() rune {
	ch := l.src[l.pos]
	l.pos++
	if ch == '\n' {
		l.line++
	}
	return ch
}

// Smalltalk comments: "like this".  Whitespace otherwise.
func (l *stLexer) skipTrivia() {
	for l.pos < len(l.src) {
		ch := l.peekAt(0)
		if unicode.IsSpace(ch) {
			l.advance()
			continue
		}
		if ch == '"' {
			l.advance()
			for l.pos < len(l.src) && l.peekAt(0) != '"' {
				l.advance()
			}
			if l.pos < len(l.src) {
				l.advance() // closing "
			}
			continue
		}
		return
	}
}

func isStBinopChar(ch rune) bool {
	return strings.ContainsRune("+-*/<>=~@%&?!,\\", ch)
}

func isStIdentStart(ch rune) bool {
	return unicode.IsLetter(ch) || ch == '_'
}

func isStIdentCont(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}

func (l *stLexer) tokenize() ([]stToken, error) {
	var toks []stToken
	start := 0
	for {
		if len(toks)>0 { toks[len(toks)-1].start=start; toks[len(toks)-1].end=l.pos } else {}
		l.skipTrivia()
		start=l.pos
		if l.pos >= len(l.src) {
			toks = append(toks, stToken{kind: stEOF, line: l.line})
			return toks, nil
		}
		line := l.line
		ch := l.peekAt(0)

		// numbers — also allow unary minus when clearly a number literal
		if unicode.IsDigit(ch) ||
			(ch == '-' && unicode.IsDigit(l.peekAt(1)) && canPrecedeNumber(toks)) {
			tk, err := l.readNumber(line)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tk)
			continue
		}

		if isStIdentStart(ch) {
			toks = append(toks, l.readIdent(line))
			continue
		}

		if ch == '\'' {
			toks = append(toks, l.readString(line))
			continue
		}

		// := assignment
		if ch == ':' && l.peekAt(1) == '=' {
			l.advance()
			l.advance()
			toks = append(toks, stToken{kind: stAssign, line: line, text: ":="})
			continue
		}

		// :ident — block parameter marker
		if ch == ':' && isStIdentStart(l.peekAt(1)) {
			l.advance() // ':'
			start := l.pos
			for l.pos < len(l.src) && isStIdentCont(l.peekAt(0)) {
				l.advance()
			}
			toks = append(toks, stToken{
				kind: stBlockArg,
				text: string(l.src[start:l.pos]),
				line: line,
			})
			continue
		}

		// # — symbol or objid literal
		if ch == '#' {
			tk, err := l.readHashLiteral(line)
			if err != nil {
				return nil, err
			}
			toks = append(toks, tk)
			continue
		}

		// $x — character literal: `$` followed by exactly one rune.
		// `$ ` is the space character; `$$` is the dollar character.
		if ch == '$' {
			l.advance() // consume '$'
			if l.pos >= len(l.src) {
				return nil, fmt.Errorf("bare '$' with no character at line %d", line)
			}
			r := l.advance()
			toks = append(toks, stToken{kind: stChar, num: float64(r), line: line})
			continue
		}

		switch ch {
		case '^':
			l.advance()
			toks = append(toks, stToken{kind: stCaret, line: line, text: "^"})
			continue
		case '(':
			l.advance()
			toks = append(toks, stToken{kind: stLParen, line: line})
			continue
		case ')':
			l.advance()
			toks = append(toks, stToken{kind: stRParen, line: line})
			continue
		case '[':
			l.advance()
			toks = append(toks, stToken{kind: stLBracket, line: line})
			continue
		case ']':
			l.advance()
			toks = append(toks, stToken{kind: stRBracket, line: line})
			continue
		case '{':
			l.advance()
			toks = append(toks, stToken{kind: stLBrace, line: line})
			continue
		case '}':
			l.advance()
			toks = append(toks, stToken{kind: stRBrace, line: line})
			continue
		case '.':
			l.advance()
			toks = append(toks, stToken{kind: stDot, line: line})
			continue
		case '|':
			l.advance()
			toks = append(toks, stToken{kind: stPipe, line: line, text: "|"})
			continue
		case ';':
			l.advance()
			toks = append(toks, stToken{kind: stSemi, line: line})
			continue
		}

		if isStBinopChar(ch) {
			start := l.pos
			for l.pos < len(l.src) && isStBinopChar(l.peekAt(0)) {
				l.advance()
			}
			toks = append(toks, stToken{
				kind: stBinop,
				text: string(l.src[start:l.pos]),
				line: line,
			})
			continue
		}

		return nil, fmt.Errorf("unexpected character %q at line %d", ch, line)
	}
}

// canPrecedeNumber returns true if the previous token permits a following
// literal number (as opposed to binary subtraction).  Conservative: treat
// only clearly-non-expression tokens as "before a number".
func canPrecedeNumber(toks []stToken) bool {
	if len(toks) == 0 {
		return true
	}
	switch toks[len(toks)-1].kind {
	case stBinop, stKeyword, stAssign, stCaret, stLParen, stLBracket,
		stLBrace, stLitArray, stDot, stPipe, stSemi, stBlockArg:
		return true
	}
	return false
}

func (l *stLexer) readNumber(line int) (stToken, error) {
	start := l.pos
	if l.peekAt(0) == '-' {
		l.advance()
	}
	for l.pos < len(l.src) && unicode.IsDigit(l.peekAt(0)) {
		l.advance()
	}
	if l.peekAt(0) == '.' && unicode.IsDigit(l.peekAt(1)) {
		l.advance()
		for l.pos < len(l.src) && unicode.IsDigit(l.peekAt(0)) {
			l.advance()
		}
	}
	text := string(l.src[start:l.pos])
	n, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return stToken{}, fmt.Errorf("bad number %q at line %d: %v", text, line, err)
	}
	return stToken{kind: stNumber, num: n, line: line, text: text}, nil
}

func (l *stLexer) readIdent(line int) stToken {
	start := l.pos
	for l.pos < len(l.src) && isStIdentCont(l.peekAt(0)) {
		l.advance()
	}
	text := string(l.src[start:l.pos])
	// keyword selector: ident:  (but not :=  because '=' is only handled above)
	if l.pos < len(l.src) && l.peekAt(0) == ':' && l.peekAt(1) != '=' {
		l.advance()
		return stToken{kind: stKeyword, text: text + ":", line: line}
	}
	return stToken{kind: stIdent, text: text, line: line}
}

// readHashLiteral parses what comes after '#':
//   #@id           → stObjId       (id is alphanum/underscore — matches the
//                                    Value.String() print form so REPL output
//                                    is round-trippable)
//   #'arbitrary'   → stSymbol      (quoted name — supports spaces, punctuation)
//   #ident         → stSymbol      (bare identifier)
//   #+, #,, #=     → stSymbol      (binary-operator selector)
func (l *stLexer) readHashLiteral(line int) (stToken, error) {
	l.advance() // consume '#'
	if l.pos >= len(l.src) {
		return stToken{}, fmt.Errorf("bare '#' at line %d", line)
	}
	ch := l.peekAt(0)
	switch {
	case ch == '(':
		l.advance() // consume '(' — the matching ')' is a normal stRParen
		return stToken{kind: stLitArray, line: line}, nil
	case ch == '@':
		l.advance() // consume '@'
		start := l.pos
		for l.pos < len(l.src) && isObjIdChar(l.peekAt(0)) {
			l.advance()
		}
		if l.pos == start {
			return stToken{}, fmt.Errorf("'#@' with no id at line %d", line)
		}
		return stToken{kind: stObjId, text: string(l.src[start:l.pos]), line: line}, nil
	case ch == '\'':
		s := l.readString(line)
		s.kind = stSymbol
		return s, nil
	case isStIdentStart(ch):
		// Bare-identifier symbol, including keyword selectors: #add:,
		// #at:put: lex as one symbol (the colons are part of the
		// selector name, not separate tokens).
		start := l.pos
		for l.pos < len(l.src) && (isStIdentCont(l.peekAt(0)) || l.peekAt(0) == ':') {
			l.advance()
		}
		return stToken{kind: stSymbol, text: string(l.src[start:l.pos]), line: line}, nil
	case isStBinopChar(ch):
		start := l.pos
		for l.pos < len(l.src) && isStBinopChar(l.peekAt(0)) {
			l.advance()
		}
		return stToken{kind: stSymbol, text: string(l.src[start:l.pos]), line: line}, nil
	}
	return stToken{}, fmt.Errorf("unexpected character %q after '#' at line %d", ch, line)
}

// isObjIdChar — alphanum or underscore.  Covers hex objids ("a1b2…"),
// the special "coordinator" id, and underscore-flanked ids like
// "__workspace__".
func isObjIdChar(ch rune) bool {
	return unicode.IsLetter(ch) || unicode.IsDigit(ch) || ch == '_'
}

func (l *stLexer) readString(line int) stToken {
	l.advance() // opening '
	var sb strings.Builder
	for l.pos < len(l.src) {
		ch := l.advance()
		if ch == '\'' {
			if l.pos < len(l.src) && l.peekAt(0) == '\'' {
				sb.WriteRune('\'')
				l.advance()
				continue
			}
			break
		}
		sb.WriteRune(ch)
	}
	return stToken{kind: stString, text: sb.String(), line: line}
}

// ---- parser ----

type stParser struct {
	recordSelectors bool
	selectorSites []SelectorSite
	toks []stToken
	pos  int
}

func (p *stParser) peek() stToken {
	return p.toks[p.pos]
}

func (p *stParser) peekAt(off int) stToken {
	i := p.pos + off
	if i >= len(p.toks) {
		return p.toks[len(p.toks)-1] // always terminated with EOF
	}
	return p.toks[i]
}

func (p *stParser) advance() stToken {
	t := p.toks[p.pos]
	if t.kind != stEOF {
		p.pos++
	}
	return t
}

func (p *stParser) accept(k stTokKind) (stToken, bool) {
	if p.peek().kind == k {
		return p.advance(), true
	}
	return stToken{}, false
}

func (p *stParser) errAt(tok stToken, format string, args ...interface{}) error {
	return fmt.Errorf("line %d: "+format, append([]interface{}{tok.line}, args...)...)
}

// node helpers (to keep the parser readable)

func sym(name string, line int) *Node {
	return &Node{Kind: NodeSymbol, Str: name, Line: line}
}
func num(n float64, line int) *Node {
	return &Node{Kind: NodeNumber, Num: n, Line: line}
}
func str(s string, line int) *Node {
	return &Node{Kind: NodeString, Str: s, Line: line}
}
func list(line int, children ...*Node) *Node {
	return &Node{Kind: NodeList, Children: children, Line: line}
}

// ---- top-level API ----

// parseWorkspaceSource parses one REPL line as Smalltalk.  Earlier
// versions routed lines starting with `(` or `;` to the S-expression
// parser, but that surprised everyone — `(coordinator allObjects)`
// looks like a parenthesized Smalltalk send, not an S-expr — and
// kept us from ever evaluating a Smalltalk expression that happens
// to start with `(`.  S-expr stays available for class files
// (`.smt` extension); REPL is Smalltalk-only.
func parseWorkspaceSource(src string) ([]*Node, error) {
	return ParseSmalltalk(src)
}

// ParseSmalltalk parses one or more top-level expressions / class defs and
// returns them as S-expression-equivalent *Node values.
func ParseSmalltalk(src string) ([]*Node, error) {
	lex := newStLexer(src)
	toks, err := lex.tokenize()
	if err != nil {
		return nil, err
	}
	p := &stParser{toks: toks}
	var out []*Node
	for p.peek().kind != stEOF {
		n, err := p.parseTopLevel()
		if err != nil {
			return nil, err
		}
		out = append(out, n)
		// optional trailing '.'
		for {
			if _, ok := p.accept(stDot); ok {
				continue
			}
			break
		}
	}
	return out, nil
}

// ParseSmalltalkClassSource parses a .st file body into a ClassDef.
func ParseSmalltalkClassSource(src string) (*ClassDef, error) {
	nodes, err := ParseSmalltalk(src)
	if err != nil {
		return nil, err
	}
	for _, n := range nodes {
		if n.Kind == NodeList && len(n.Children) >= 2 &&
			n.Children[0].Kind == NodeSymbol && n.Children[0].Str == "class" {
			return parseClassNode(n)
		}
	}
	return nil, fmt.Errorf("no `class Name [ ... ]` form found")
}

// ---- grammar ----

// top-level form: class definition or a statement.
func (p *stParser) parseTopLevel() (*Node, error) {
	t := p.peek()
	if t.kind == stIdent && t.text == "class" {
		return p.parseClass()
	}
	return p.parseStatement()
}

// class Name ( extends Parent )? [ ( | var* | )?  methodDef* ]
func (p *stParser) parseClass() (*Node, error) {
	classTok := p.advance() // 'class'
	nameTok, ok := p.accept(stIdent)
	if !ok {
		return nil, p.errAt(p.peek(), "expected class name after `class`")
	}

	children := []*Node{
		sym("class", classTok.line),
		sym(nameTok.text, nameTok.line),
	}

	// optional `extends Parent`
	if p.peek().kind == stIdent && p.peek().text == "extends" {
		p.advance() // 'extends'
		parentTok, ok := p.accept(stIdent)
		if !ok {
			return nil, p.errAt(p.peek(), "expected superclass name after `extends`")
		}
		children = append(children, &Node{
			Kind: NodeList,
			Children: []*Node{
				sym("superclass", parentTok.line),
				sym(parentTok.text, parentTok.line),
			},
			Line: parentTok.line,
		})
	}

	if _, ok := p.accept(stLBracket); !ok {
		return nil, p.errAt(p.peek(), "expected `[` to open class body")
	}

	// optional | var var var |
	if p.peek().kind == stPipe {
		pipeLine := p.advance().line // first |
		varsChildren := []*Node{sym("vars", pipeLine)}
		for p.peek().kind == stIdent {
			v := p.advance()
			varsChildren = append(varsChildren, sym(v.text, v.line))
		}
		if _, ok := p.accept(stPipe); !ok {
			return nil, p.errAt(p.peek(), "expected closing `|` after instance vars")
		}
		children = append(children, &Node{
			Kind:     NodeList,
			Children: varsChildren,
			Line:     pipeLine,
		})
	}

	// methods until ]
	for p.peek().kind != stRBracket && p.peek().kind != stEOF {
		m, err := p.parseMethod()
		if err != nil {
			return nil, err
		}
		children = append(children, m)
	}
	if _, ok := p.accept(stRBracket); !ok {
		return nil, p.errAt(p.peek(), "expected `]` to close class body")
	}
	return &Node{Kind: NodeList, Children: children, Line: classTok.line}, nil
}

// methodDef:
//   unaryHeader  [ body ]        → (method name () body...)
//   binopHeader  [ body ]        → (method op   (p) body...)
//   keywordHeader [ body ]       → (method a:b: (p q) body...)
func (p *stParser) parseMethod() (*Node, error) {
	headerStart := p.pos
	startLine := p.peek().line
	var name string
	var params []*Node

	switch p.peek().kind {
	case stIdent:
		// unary
		t := p.advance()
		name = t.text
	case stBinop:
		op := p.advance()
		pTok, ok := p.accept(stIdent)
		if !ok {
			return nil, p.errAt(p.peek(), "expected parameter name after binary selector `%s`", op.text)
		}
		name = op.text
		params = append(params, sym(pTok.text, pTok.line))
	case stKeyword:
		var sb strings.Builder
		for p.peek().kind == stKeyword {
			k := p.advance()
			sb.WriteString(k.text)
			pTok, ok := p.accept(stIdent)
			if !ok {
				return nil, p.errAt(p.peek(), "expected parameter name after keyword `%s`", k.text)
			}
			params = append(params, sym(pTok.text, pTok.line))
		}
		name = sb.String()
	default:
		return nil, p.errAt(p.peek(), "expected method header (got %q)", p.peek().text)
	}

	if _, ok := p.accept(stLBracket); !ok {
		return nil, p.errAt(p.peek(), "expected `[` to open method body for `%s`", name)
	}
	p.noteMethodSelector(name,p.toks[headerStart:p.pos-1])
	body, err := p.parseBlockBody(stRBracket)
	if err != nil {
		return nil, err
	}
	if _, ok := p.accept(stRBracket); !ok {
		return nil, p.errAt(p.peek(), "expected `]` to close method body for `%s`", name)
	}

	paramsList := &Node{Kind: NodeList, Children: params, Line: startLine}
	children := []*Node{
		sym("method", startLine),
		sym(name, startLine),
		paramsList,
	}
	children = append(children, body...)
	return &Node{Kind: NodeList, Children: children, Line: startLine}, nil
}

// block:  [ p1 p2 | body ]   or   [ :p1 :p2 | body ]   or   [ body ]
//
// The plain `[x y | …]` form is preferred — the `|` already separates
// params from body, so the leading colon adds nothing.  The legacy
// `[:x :y | …]` form parses too, for code that grew up on Smalltalk
// muscle memory.
func (p *stParser) parseBlock() (*Node, error) {
	open := p.advance() // '['
	var params []*Node

	if p.blockHasParams() {
		for {
			t := p.peek()
			if t.kind != stBlockArg && t.kind != stIdent {
				break
			}
			p.advance()
			params = append(params, sym(t.text, t.line))
		}
		if _, ok := p.accept(stPipe); !ok {
			return nil, p.errAt(p.peek(), "expected `|` after block parameters")
		}
	}

	body, err := p.parseBlockBody(stRBracket)
	if err != nil {
		return nil, err
	}
	if _, ok := p.accept(stRBracket); !ok {
		return nil, p.errAt(p.peek(), "expected `]` to close block")
	}

	paramsList := &Node{Kind: NodeList, Children: params, Line: open.line}
	children := []*Node{sym("block", open.line), paramsList}
	children = append(children, body...)
	return &Node{Kind: NodeList, Children: children, Line: open.line}, nil
}

// parseBraceArray parses a dynamic array literal `{ e1. e2. e3 }`.
// Elements are full expressions separated by `.`; each is evaluated at
// runtime and collected into a fresh Array.  Desugars to the `(array …)`
// special form.
func (p *stParser) parseBraceArray() (*Node, error) {
	open := p.advance() // '{'
	children := []*Node{sym("array", open.line)}
	for p.peek().kind != stRBrace && p.peek().kind != stEOF {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		children = append(children, e)
		for p.peek().kind == stDot {
			p.advance()
		}
	}
	if _, ok := p.accept(stRBrace); !ok {
		return nil, p.errAt(p.peek(), "expected `}` to close array literal")
	}
	return &Node{Kind: NodeList, Children: children, Line: open.line}, nil
}

// parseLiteralArray parses a literal array `#( … )`.  Elements are
// space-separated literals: numbers, strings, and bare words.  A bare
// word becomes a symbol (a string in this dialect) — except true / false
// / nil, which keep their value semantics.  Nested `#( … )` and `( … )`
// produce sub-arrays.  Desugars to `(array …)` like the brace form.
func (p *stParser) parseLiteralArray() (*Node, error) {
	open := p.advance() // '#('
	children := []*Node{sym("array", open.line)}
	for {
		t := p.peek()
		if t.kind == stRParen {
			p.advance()
			break
		}
		if t.kind == stEOF {
			return nil, p.errAt(t, "expected `)` to close #( literal array")
		}
		switch t.kind {
		case stNumber:
			p.advance()
			children = append(children, num(t.num, t.line))
		case stString, stSymbol:
			p.advance()
			children = append(children, str(t.text, t.line))
		case stIdent:
			p.advance()
			switch t.text {
			case "true", "false", "nil":
				children = append(children, sym(t.text, t.line))
			default:
				children = append(children, str(t.text, t.line))
			}
		case stBinop, stKeyword:
			// operator / keyword token as a symbol literal, e.g. #(+ - foo:)
			p.advance()
			children = append(children, str(t.text, t.line))
		case stLitArray, stLParen:
			sub, err := p.parseLiteralArray()
			if err != nil {
				return nil, err
			}
			children = append(children, sub)
		default:
			return nil, p.errAt(t, "unexpected %q in #( literal array", t.text)
		}
	}
	return &Node{Kind: NodeList, Children: children, Line: open.line}, nil
}

// blockHasParams scans forward from the current token through any run
// of identifiers / `:ident`s; if that run terminates at a `|`, this is
// a parameter list.  Anything else (an operator, a `[`, end-of-block,
// etc.) means the same identifiers are the start of the body.
func (p *stParser) blockHasParams() bool {
	for off := 0; ; off++ {
		t := p.peekAt(off)
		switch t.kind {
		case stIdent, stBlockArg:
			continue
		case stPipe:
			return true
		default:
			return false
		}
	}
}

// Statements separated by `.` up to (but not consuming) the closing token.
func (p *stParser) parseBlockBody(closer stTokKind) ([]*Node, error) {
	var body []*Node
	for p.peek().kind != closer && p.peek().kind != stEOF {
		s, err := p.parseStatement()
		if err != nil {
			return nil, err
		}
		body = append(body, s)
		// consume zero or more separators
		for p.peek().kind == stDot {
			p.advance()
		}
	}
	return body, nil
}

// statement: assignment | returnStmt | expression
func (p *stParser) parseStatement() (*Node, error) {
	// ^ expr
	if p.peek().kind == stCaret {
		c := p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return list(c.line, sym("^", c.line), e), nil
	}
	// ident := expr
	if p.peek().kind == stIdent && p.peekAt(1).kind == stAssign {
		name := p.advance()
		p.advance() // :=
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		return list(name.line,
			sym("set", name.line),
			sym(name.text, name.line),
			e,
		), nil
	}
	return p.parseExpr()
}

// expression — cascade level (lowest precedence), wrapping keyword sends.
func (p *stParser) parseExpr() (*Node, error) {
	return p.parseCascade()
}

// cascade:  keywordExpr ( ';' message )*
//
// `recv m1; m2; m3` sends m1 to recv, then m2 and m3 to the *same*
// receiver (the receiver of m1), yielding the last message's value.
// Two desugarings:
//
//   - If the first message folded to `(print …)` (a `Transcript show:`
//     chain), the whole cascade becomes `(begin (print …) …)` — each
//     tail message is mapped through the same Transcript rule.  This is
//     the common `Transcript show: a; show: b; cr` idiom.
//   - Otherwise it becomes `(cascade recv (sel arg…) …)`, evaluated by
//     the shared cascade special form.
func (p *stParser) parseCascade() (*Node, error) {
	first, err := p.parseKeyword()
	if err != nil {
		return nil, err
	}
	if p.peek().kind != stSemi {
		return first, nil
	}

	// Transcript cascade: first already became (print …).
	if isPrintNode(first) {
		msgs := []*Node{first}
		for p.peek().kind == stSemi {
			p.advance() // ';'
			sel, args, line, err := p.parseCascadeMessage(true)
			if err != nil {
				return nil, err
			}
			node, ok := transcriptCascadeNode(sel, args, line)
			if !ok {
				return nil, p.errAt(p.peek(), "cascade after `Transcript show:` only understands show:/showCr:/display:/print:/cr/nl, got %q", sel)
			}
			msgs = append(msgs, node)
		}
		children := append([]*Node{sym("begin", first.Line)}, msgs...)
		return &Node{Kind: NodeList, Children: children, Line: first.Line}, nil
	}

	// Normal case: the head is a `(send recv sel …)`, so the cascade
	// receiver is recv and the head message is replayed as the first
	// cascade message — strict Smalltalk semantics.
	//
	// Fallback: the head is some other expression (`Counter new`,
	// `{1.2.3}`, a parenthesised expr).  Use the head expression *itself*
	// as the receiver, with no replayed message.  This makes
	// `Counter new; increment; value` work — the cascade sends to the new
	// instance.  (Strict Smalltalk would send to `Counter` because `new`
	// is just a unary message; here `new` is a spawn and the instance is
	// the receiver you actually want.)
	recv, firstMsg, ok := sendReceiverAndMessage(first)
	var children []*Node
	if ok {
		children = []*Node{sym("cascade", first.Line), recv, firstMsg}
	} else {
		children = []*Node{sym("cascade", first.Line), first}
	}
	for p.peek().kind == stSemi {
		p.advance() // ';'
		sel, args, line, err := p.parseCascadeMessage()
		if err != nil {
			return nil, err
		}
		msg := append([]*Node{sym(sel, line)}, args...)
		children = append(children, &Node{Kind: NodeList, Children: msg, Line: line})
	}
	return &Node{Kind: NodeList, Children: children, Line: first.Line}, nil
}

// parseCascadeMessage parses one receiver-less message tail — unary,
// binary, or keyword — and returns its combined selector plus argument
// expressions.  Used for the messages after each ';' in a cascade.
func (p *stParser) parseCascadeMessage(skipOuter ...bool) (selector string, args []*Node, line int, err error) {
	t := p.peek()
	capture := len(skipOuter)==0 || !skipOuter[0]
	line = t.line
	switch t.kind {
	case stIdent:
		p.advance()
		if capture { p.noteSelector(t.text,"send",[]stToken{t}) } else {}
		return t.text, nil, line, nil
	case stBinop:
		p.advance()
		rhs, err := p.parseUnary()
		if err != nil {
			return "", nil, line, err
		}
		if capture { p.noteSelector(t.text,"send",[]stToken{t}) } else {}
		return t.text, []*Node{rhs}, line, nil
	case stKeyword:
		var sb strings.Builder
		var selectorTokens []stToken
		for p.peek().kind == stKeyword {
			k := p.advance()
			selectorTokens=append(selectorTokens,k)
			sb.WriteString(k.text)
			a, err := p.parseBinary()
			if err != nil {
				return "", nil, line, err
			}
			args = append(args, a)
		}
		if capture { p.noteSelector(sb.String(),"send",selectorTokens) } else {}
		return sb.String(), args, line, nil
	}
	return "", nil, line, p.errAt(t, "expected a message after ';' in cascade, got %q", t.text)
}

// isPrintNode reports whether n is a `(print …)` list — the shape the
// Smalltalk front end folds `Transcript show:`/`cr`/`nl` into.
func isPrintNode(n *Node) bool {
	return n.Kind == NodeList && len(n.Children) >= 1 &&
		n.Children[0].Kind == NodeSymbol && n.Children[0].Str == "print"
}

// sendReceiverAndMessage splits a `(send recv sel arg…)` node into its
// receiver and a cascade message `(sel arg…)`.  Returns ok=false for
// any other shape (e.g. `(new …)`), which the caller reports as a
// cascade-on-non-send error.
func sendReceiverAndMessage(n *Node) (recv *Node, msg *Node, ok bool) {
	if n.Kind != NodeList || len(n.Children) < 3 ||
		n.Children[0].Kind != NodeSymbol || n.Children[0].Str != "send" {
		return nil, nil, false
	}
	recv = n.Children[1]
	msgChildren := append([]*Node{n.Children[2]}, n.Children[3:]...)
	msg = &Node{Kind: NodeList, Children: msgChildren, Line: n.Line}
	return recv, msg, true
}

// transcriptCascadeNode maps a cascade message that follows a
// `Transcript show:` into the same `(print …)` form buildSend produces
// for the head message.  Returns ok=false for selectors that aren't a
// Transcript output method.
func transcriptCascadeNode(selector string, args []*Node, line int) (*Node, bool) {
	switch selector {
	case "show:", "showCr:", "display:", "print:":
		if len(args) == 1 {
			return list(line, sym("print", line), args[0]), true
		}
	case "cr", "nl":
		if len(args) == 0 {
			return list(line, sym("print", line), str("", line)), true
		}
	}
	return nil, false
}

// keyword send: binaryExpr (KEYWORD binaryExpr)+
//
// As a shell-flavoured affordance, a statement that STARTS with a
// keyword token (no receiver) is parsed as a self-keyword-send.
// `cd: '/tmp'` becomes `(send self cd: '/tmp')`.  Real Smalltalk
// rejects this; we accept it because at the REPL it's how a shell
// user writes a command with args.
func (p *stParser) parseKeyword() (*Node, error) {
	var (
		recv *Node
		err  error
	)
	if p.peek().kind == stKeyword {
		recv = sym("self", p.peek().line)
	} else {
		recv, err = p.parseBinary()
		if err != nil {
			return nil, err
		}
	}
	if p.peek().kind != stKeyword {
		return recv, nil
	}
	var sb strings.Builder
	var args []*Node
	var selectorTokens []stToken
	firstLine := p.peek().line
	for p.peek().kind == stKeyword {
		k := p.advance()
		selectorTokens=append(selectorTokens,k)
		sb.WriteString(k.text)
		a, err := p.parseBinary()
		if err != nil {
			return nil, err
		}
		args = append(args, a)
	}
	result := p.buildSend(recv, sb.String(), args, firstLine)
	p.noteParsedSend(result,sb.String(),selectorTokens)
	return result, nil
}

// binary send: unary (BINOP unary)*
func (p *stParser) parseBinary() (*Node, error) {
	left, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == stBinop || (p.peek().kind == stPipe && p.peekAt(1).kind != stIdent) {
		// Treat `|` as binop only when an expression follows — not when it
		// appears as a block-param separator (handled at block open).
		op := p.advance()
		right, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		left = list(op.line,
			sym("send", op.line),
			left,
			sym(op.text, op.line),
			right,
		)
		p.noteSelector(op.text,"send",[]stToken{op})
	}
	return left, nil
}

// unary send: primary IDENT*   (left-to-right)
func (p *stParser) parseUnary() (*Node, error) {
	recv, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for p.peek().kind == stIdent {
		// A bare identifier here is a unary selector.
		t := p.advance()
		recv = p.buildSend(recv, t.text, nil, t.line)
		p.noteParsedSend(recv,t.text,[]stToken{t})
	}
	return recv, nil
}

// primary: literal | ident | self | (expr) | block | #symbol | #@objid
func (p *stParser) parsePrimary() (*Node, error) {
	t := p.peek()
	switch t.kind {
	case stNumber:
		p.advance()
		return num(t.num, t.line), nil
	case stString:
		p.advance()
		return str(t.text, t.line), nil
	case stSymbol:
		// Symbol literal — evaluates to a String.  The dialect has no
		// distinct Symbol type; #foo and 'foo' produce the same value.
		p.advance()
		return str(t.text, t.line), nil
	case stChar:
		// Character literal — desugars to (char <codepoint>), mirroring how
		// #@id desugars to (objid …).  Evaluated by evalList's "char" case.
		p.advance()
		return list(t.line, sym("char", t.line), num(t.num, t.line)), nil
	case stObjId:
		// Objid literal — evaluates to ObjVal(id).  Wrapped in
		// (objid "id") so the evaluator can tell it apart from a
		// regular string at eval time (parser produces only Nodes).
		p.advance()
		return list(t.line, sym("objid", t.line), str(t.text, t.line)), nil
	case stIdent:
		p.advance()
		// reserved words map to themselves as symbols; evaluator handles them
		return sym(t.text, t.line), nil
	case stLParen:
		p.advance()
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if _, ok := p.accept(stRParen); !ok {
			return nil, p.errAt(p.peek(), "expected `)`")
		}
		return e, nil
	case stLBracket:
		return p.parseBlock()
	case stLBrace:
		return p.parseBraceArray()
	case stLitArray:
		return p.parseLiteralArray()
	case stBinop:
		// Handle prefix minus: -42 (number literal) and -x (variable → x negated).
		if t.text == "-" {
			if p.peekAt(1).kind == stNumber {
				p.advance()
				n := p.advance()
				return num(-n.num, n.line), nil
			}
			if p.peekAt(1).kind == stIdent {
				p.advance() // consume -
				id := p.advance()
				return p.buildSend(sym(id.text, id.line), "negated", nil, t.line), nil
			}
		}
	}
	return nil, p.errAt(t, "unexpected token %q", t.text)
}

// buildSend folds Smalltalk-style sends into S-expression equivalents,
// applying the special mappings:
//   Transcript show: x        → (print x)
//   ClassName new             → (new ClassName)
//   ClassName kw: v kw2: v2   → (new ClassName v v2)
func (p *stParser) buildSend(recv *Node, selector string, args []*Node, line int) *Node {
	// Transcript show: x  →  (print x)
	if recv.Kind == NodeSymbol && recv.Str == "Transcript" &&
		(selector == "show:" || selector == "showCr:" ||
			selector == "display:" || selector == "print:") &&
		len(args) == 1 {
		return list(line, sym("print", line), args[0])
	}
	// Transcript cr / nl  →  (print "") — a blank transcript line.  The
	// transcript is line-oriented (one line per print), so a bare cr is
	// just an empty line.  Lets a `… ; cr` cascade tail and a standalone
	// `Transcript cr` both resolve without a Transcript object existing.
	if recv.Kind == NodeSymbol && recv.Str == "Transcript" &&
		(selector == "cr" || selector == "nl") && len(args) == 0 {
		return list(line, sym("print", line), str("", line))
	}

	// JSON encode: x  →  (jsonEncode x);  JSON decode: s  →  (jsonDecode s).
	// The S-expression evaluator has these as top-level forms but the
	// Smalltalk parser has no function-call notation, so a pseudo-class
	// `JSON` with keyword selectors is the bridge — `.st` method bodies
	// can now reach JSON without the round-trip through a `.smt` helper.
	if recv.Kind == NodeSymbol && recv.Str == "JSON" && len(args) == 1 {
		switch selector {
		case "encode:":
			return list(line, sym("jsonEncode", line), args[0])
		case "decode:":
			return list(line, sym("jsonDecode", line), args[0])
		}
	}

	// Capitalised receiver treated as a class: unary `new` or any keyword send
	// becomes a spawn.  (There are no user-defined class-side methods in this
	// runtime, so this is unambiguous.)
	if recv.Kind == NodeSymbol && isLikelyClassName(recv.Str) {
		if selector == "new" && len(args) == 0 {
			return list(line, sym("new", line), sym(recv.Str, recv.Line))
		}
		if strings.HasSuffix(selector, ":") {
			// keyword send on a class  ⇒  (new Class arg1 arg2 ...)
			children := []*Node{sym("new", line), sym(recv.Str, recv.Line)}
			children = append(children, args...)
			return &Node{Kind: NodeList, Children: children, Line: line}
		}
	}

	// Default: (send recv selector args...)
	children := []*Node{
		sym("send", line),
		recv,
		sym(selector, line),
	}
	children = append(children, args...)
	return &Node{Kind: NodeList, Children: children, Line: line}
}

func isLikelyClassName(s string) bool {
	if s == "" {
		return false
	}
	switch s {
	case "self", "super", "nil", "true", "false", "coordinator",
		"Transcript", "JSON":
		return false
	}
	r := []rune(s)[0]
	return unicode.IsUpper(r)
}

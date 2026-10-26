package main

import (
	"strings"
	"testing"
)

// Each case parses Smalltalk and expects the .String() of the resulting
// Node list to equal the S-expression form.
func TestSmalltalkToSExpr(t *testing.T) {
	cases := []struct {
		name string
		st   string
		want string
	}{
		{"number", `42`, `42`},
		{"string", `'hello'`, `"hello"`},
		{"ident", `self`, `self`},
		{"assignment", `x := 1`, `(set x 1)`},
		{"caret", `^ 42`, `(^ 42)`},
		{"unary", `5 factorial`, `(send 5 factorial)`},
		{"binary", `1 + 2`, `(send 1 + 2)`},
		{"binary chain", `1 + 2 * 3`, `(send (send 1 + 2) * 3)`},
		{"keyword", `dict at: 1 put: 2`, `(send dict at:put: 1 2)`},
		{"unary before keyword",
			`arr first between: 1 and: 10`,
			`(send (send arr first) between:and: 1 10)`},
		{"block no args",
			`[42]`,
			`(block () 42)`},
		{"block with args",
			`[:x :y | x + y]`,
			`(block (x y) (send x + y))`},
		{"class new unary",
			`Counter new`,
			`(new Counter)`},
		{"class new keyword",
			`Dog name: 'Rex' breed: 'Lab'`,
			`(new Dog "Rex" "Lab")`},
		{"transcript",
			`Transcript show: 'hi'`,
			`(print "hi")`},
		{"statements",
			`x := 1. y := 2. ^ x + y`,
			`(set x 1) (set y 2) (^ (send x + y))`},
		{"negative number",
			`-5 abs`,
			`(send -5 abs)`},
		{"json encode",
			`JSON encode: d`,
			`(jsonEncode d)`},
		{"json decode",
			`JSON decode: s`,
			`(jsonDecode s)`},
		{"cascade keyword",
			`d at: 1 put: 2; at: 3 put: 4`,
			`(cascade d (at:put: 1 2) (at:put: 3 4))`},
		{"cascade unary",
			`c increment; increment; value`,
			`(cascade c (increment) (increment) (value))`},
		{"cascade on new",
			`Counter new; increment; value`,
			`(cascade (new Counter) (increment) (value))`},
		{"transcript cascade",
			`Transcript show: 'a'; show: 'b'; cr`,
			`(begin (print "a") (print "b") (print ""))`},
		{"brace array",
			`{1. 2. 3}`,
			`(array 1 2 3)`},
		{"brace array exprs",
			`{x + 1. y}`,
			`(array (send x + 1) y)`},
		{"literal array",
			`#('a' 'b' 'c')`,
			`(array "a" "b" "c")`},
		{"literal array words",
			`#(foo bar true)`,
			`(array "foo" "bar" true)`},
		{"character literal",
			`$a`,
			`(char 97)`},
		{"character literal space",
			`$ `,
			`(char 32)`},
		{"super unary",
			`super initialize`,
			`(send super initialize)`},
		{"super keyword",
			`super at: 1 put: 2`,
			`(send super at:put: 1 2)`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			nodes, err := ParseSmalltalk(tc.st)
			if err != nil {
				t.Fatalf("parse error: %v", err)
			}
			var got string
			for i, n := range nodes {
				if i > 0 {
					got += " "
				}
				got += n.String()
			}
			if got != tc.want {
				t.Fatalf("\ninput: %s\n  got: %s\n want: %s", tc.st, got, tc.want)
			}
		})
	}
}

func TestSmalltalkComplete(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{`x := 1`, true},
		{`1 to: 5 do: [i |`, false},
		{`1 to: 5 do: [i | i ]`, true},
		{`class Foo [`, false},
		{"class Foo [\n  bar [ ^ 1 ]\n]", true},
		{`d at: '[' put: ']'`, true},  // brackets inside a string don't count
		{`x := 1 "comment with ] [`, false}, // unterminated comment
		{`foo ]]]`, true},                   // stray closers clamp at 0
		{`{1. 2. 3`, false},
		{`{1. 2. 3}`, true},
	}
	for _, tc := range cases {
		if got := smalltalkComplete(tc.src); got != tc.want {
			t.Errorf("smalltalkComplete(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}

func TestSmalltalkClass(t *testing.T) {
	src := `
class Counter [
    | count |
    initialize [ count := 0 ]
    increment [ count := count + 1 ]
    add: n    [ count := count + n ]
    value     [ ^ count ]
]`
	cd, err := ParseSmalltalkClassSource(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cd.Name != "Counter" {
		t.Fatalf("class name: got %q want Counter", cd.Name)
	}
	if len(cd.Vars) != 1 || cd.Vars[0] != "count" {
		t.Fatalf("vars: got %v want [count]", cd.Vars)
	}
	wantMethods := []string{"initialize", "increment", "add:", "value"}
	for _, m := range wantMethods {
		if _, ok := cd.Methods[m]; !ok {
			t.Errorf("method %q missing", m)
		}
	}
	if p := cd.Methods["add:"].Params; len(p) != 1 || p[0] != "n" {
		t.Errorf("add: params: got %v want [n]", p)
	}
}

func TestSmalltalkClassExtends(t *testing.T) {
	src := `
class Dog extends Animal [
    | tricks |
    initialize [ super initialize. tricks := 0 ]
    bark [ ^ 'woof' ]
]`
	cd, err := ParseSmalltalkClassSource(src)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if cd.Name != "Dog" {
		t.Fatalf("class name: got %q want Dog", cd.Name)
	}
	if cd.SuperclassName != "Animal" {
		t.Fatalf("superclass: got %q want Animal", cd.SuperclassName)
	}
	if len(cd.Vars) != 1 || cd.Vars[0] != "tricks" {
		t.Fatalf("vars: got %v want [tricks]", cd.Vars)
	}
	for _, m := range []string{"initialize", "bark"} {
		if _, ok := cd.Methods[m]; !ok {
			t.Errorf("method %q missing", m)
		}
	}
}

// TestLookupMethod checks superclass-chain method resolution and the
// defining-class returned alongside it.
func TestLookupMethod(t *testing.T) {
	base := &ClassDef{
		Name:    "Base",
		Methods: map[string]*MethodDef{"greet": {Name: "greet"}, "shared": {Name: "shared"}},
	}
	derived := &ClassDef{
		Name:       "Derived",
		Superclass: base,
		Methods:    map[string]*MethodDef{"greet": {Name: "greet"}, "own": {Name: "own"}},
	}

	// Override: Derived.greet wins, defined by Derived.
	if m, dc, ok := lookupMethod(derived, "greet"); !ok || dc != derived || m == nil {
		t.Errorf("greet: got (%v, %v, %v), want Derived's", m, dc, ok)
	}
	// Inherited: shared resolves to Base.
	if m, dc, ok := lookupMethod(derived, "shared"); !ok || dc != base || m == nil {
		t.Errorf("shared: got (%v, %v, %v), want Base's", m, dc, ok)
	}
	// Child-only.
	if _, dc, ok := lookupMethod(derived, "own"); !ok || dc != derived {
		t.Errorf("own: got dc=%v ok=%v, want Derived", dc, ok)
	}
	// Absent.
	if _, _, ok := lookupMethod(derived, "missing"); ok {
		t.Error("missing should not resolve")
	}
}

func TestWorkspaceSourceDispatch(t *testing.T) {
	// REPL is Smalltalk-only (S-expr stays in .smt class files).  A
	// leading `(` is a parenthesized Smalltalk expression, not an
	// S-expression list.
	nodes, err := parseWorkspaceSource(`1 + 2`)
	if err != nil {
		t.Fatalf("bare smalltalk: %v", err)
	}
	if got := nodes[0].String(); got != "(send 1 + 2)" {
		t.Fatalf("bare smalltalk: got %s", got)
	}
	nodes, err = parseWorkspaceSource(`(1 + 2)`)
	if err != nil {
		t.Fatalf("parenthesized: %v", err)
	}
	if got := nodes[0].String(); got != "(send 1 + 2)" {
		t.Fatalf("parenthesized: got %s", got)
	}
}

func TestProcessPrintStringSummarizesProcess(t *testing.T) {
	obj := &ObjectServer{
		id:        "process-test",
		procState: &processState{command: "sleep 1", started: true},
	}
	got := obj.callProcessMethod("printString", nil)
	if got.Kind != "string" {
		t.Fatalf("printString kind: got %s want string", got.Kind)
	}
	if !strings.Contains(got.Str, "running") {
		t.Fatalf("printString should include status, got %q", got.Str)
	}
	if !strings.Contains(got.Str, "pid") {
		t.Fatalf("printString should include pid label, got %q", got.Str)
	}
	if !strings.Contains(got.Str, "sleep 1") {
		t.Fatalf("printString should include command, got %q", got.Str)
	}
}

package main

import "testing"

// newTestObject builds a bare in-process ObjectServer for unit-testing the
// local (non-RPC) method dispatchers.  No coordinator, no HTTP server, no
// spawned children — so it only exercises methods that operate on the
// object's own state.  Methods that spawn a result collection or send to a
// block (collect:, select:, reverse, copy, …) need a live cluster and are
// covered by integration_test.go instead.
func newTestObject(class string) *ObjectServer {
	return &ObjectServer{
		classDef:       &ClassDef{Name: class, Methods: map[string]*MethodDef{}},
		instanceVars:   map[string]Value{},
		dictionaryData: map[string]Value{},
	}
}

func (obj *ObjectServer) arr(vals ...Value) {
	for _, v := range vals {
		obj.callArrayMethod("add:", []Value{v})
	}
}

func TestArrayLocalMethods(t *testing.T) {
	a := newTestObject(BuiltinClassArray)
	a.arr(NumVal(3), NumVal(1), NumVal(2))

	if got := a.callArrayMethod("size", nil); got.Num != 3 {
		t.Fatalf("size = %v, want 3", got.Num)
	}
	if got := a.callArrayMethod("at:", []Value{NumVal(0)}); got.Num != 3 {
		t.Fatalf("at: 0 = %v, want 3", got.Num)
	}
	if got := a.callArrayMethod("first", nil); got.Num != 3 {
		t.Fatalf("first = %v, want 3", got.Num)
	}
	if got := a.callArrayMethod("last", nil); got.Num != 2 {
		t.Fatalf("last = %v, want 2", got.Num)
	}
	if !a.callArrayMethod("includes:", []Value{NumVal(2)}).Bool {
		t.Fatal("includes: 2 should be true")
	}
	if a.callArrayMethod("includes:", []Value{NumVal(9)}).Bool {
		t.Fatal("includes: 9 should be false")
	}
	if got := a.callArrayMethod("indexOf:", []Value{NumVal(2)}); got.Num != 2 {
		t.Fatalf("indexOf: 2 = %v, want 2", got.Num)
	}
	if got := a.callArrayMethod("indexOf:", []Value{NumVal(9)}); got.Num != -1 {
		t.Fatalf("indexOf: 9 = %v, want -1", got.Num)
	}
	if a.callArrayMethod("isEmpty", nil).Bool {
		t.Fatal("isEmpty should be false")
	}
	if !a.callArrayMethod("notEmpty", nil).Bool {
		t.Fatal("notEmpty should be true")
	}

	// sort mutates in place → 1, 2, 3
	a.callArrayMethod("sort", nil)
	for i, want := range []float64{1, 2, 3} {
		if got := a.callArrayMethod("at:", []Value{NumVal(float64(i))}); got.Num != want {
			t.Fatalf("after sort at: %d = %v, want %v", i, got.Num, want)
		}
	}

	// removeAt: 0 removes the 1, returns it
	if got := a.callArrayMethod("removeAt:", []Value{NumVal(0)}); got.Num != 1 {
		t.Fatalf("removeAt: 0 = %v, want 1", got.Num)
	}
	if got := a.callArrayMethod("size", nil); got.Num != 2 {
		t.Fatalf("size after removeAt: = %v, want 2", got.Num)
	}

	// remove: 3 by value
	a.callArrayMethod("remove:", []Value{NumVal(3)})
	if a.callArrayMethod("includes:", []Value{NumVal(3)}).Bool {
		t.Fatal("3 should be gone after remove:")
	}
	if got := a.callArrayMethod("at:put:", []Value{NumVal(5), NumVal(99)}); got.Num != 99 {
		t.Fatalf("at:put: returned %v, want 99", got.Num)
	}
	if got := a.callArrayMethod("at:", []Value{NumVal(5)}); got.Num != 99 {
		t.Fatalf("at: 5 = %v, want 99 (auto-grown)", got.Num)
	}

	empty := newTestObject(BuiltinClassArray)
	if !empty.callArrayMethod("isEmpty", nil).Bool {
		t.Fatal("fresh array should be empty")
	}

	// min / max / join:
	nums := newTestObject(BuiltinClassArray)
	nums.arr(NumVal(3), NumVal(1), NumVal(4), NumVal(1), NumVal(5))
	if got := nums.callArrayMethod("min", nil); got.Num != 1 {
		t.Fatalf("min = %v, want 1", got.Num)
	}
	if got := nums.callArrayMethod("max", nil); got.Num != 5 {
		t.Fatalf("max = %v, want 5", got.Num)
	}

	words := newTestObject(BuiltinClassArray)
	words.arr(StrVal("a"), StrVal("b"), StrVal("c"))
	if got := words.callArrayMethod("join:", []Value{StrVal("-")}); got.Str != "a-b-c" {
		t.Fatalf("join: '-' = %q, want \"a-b-c\"", got.Str)
	}
	if got := words.callArrayMethod("join:", nil); got.Str != "abc" {
		t.Fatalf("join: nil sep = %q, want \"abc\"", got.Str)
	}

	// min / max on empty array → Nil
	if got := empty.callArrayMethod("min", nil); got.Kind != "" && got.Kind != "nil" {
		t.Fatalf("min on empty should be nil, got %v", got)
	}
}

func TestDictionaryLocalMethods(t *testing.T) {
	d := newTestObject(BuiltinClassDictionary)
	d.callDictionaryMethod("at:put:", []Value{StrVal("a"), NumVal(1)})
	d.callDictionaryMethod("at:put:", []Value{StrVal("b"), NumVal(2)})

	if got := d.callDictionaryMethod("size", nil); got.Num != 2 {
		t.Fatalf("size = %v, want 2", got.Num)
	}
	if got := d.callDictionaryMethod("at:", []Value{StrVal("b")}); got.Num != 2 {
		t.Fatalf("at: b = %v, want 2", got.Num)
	}
	if !d.callDictionaryMethod("includesKey:", []Value{StrVal("a")}).Bool {
		t.Fatal("includesKey: a should be true")
	}
	if d.callDictionaryMethod("includesKey:", []Value{StrVal("z")}).Bool {
		t.Fatal("includesKey: z should be false")
	}
	if d.callDictionaryMethod("isEmpty", nil).Bool {
		t.Fatal("isEmpty should be false")
	}
	if !d.callDictionaryMethod("notEmpty", nil).Bool {
		t.Fatal("notEmpty should be true")
	}

	d.callDictionaryMethod("removeKey:", []Value{StrVal("a")})
	if d.callDictionaryMethod("includesKey:", []Value{StrVal("a")}).Bool {
		t.Fatal("a should be gone after removeKey:")
	}
	if got := d.callDictionaryMethod("size", nil); got.Num != 1 {
		t.Fatalf("size after removeKey: = %v, want 1", got.Num)
	}

	empty := newTestObject(BuiltinClassDictionary)
	if !empty.callDictionaryMethod("isEmpty", nil).Bool {
		t.Fatal("fresh dictionary should be empty")
	}
}

func TestStringMethods(t *testing.T) {
	o := newTestObject("String")
	cases := []struct {
		method string
		recv   string
		args   []Value
		want   Value
	}{
		{"size", "hello", nil, NumVal(5)},
		{"at:", "hello", []Value{NumVal(1)}, StrVal("e")},
		{"asUppercase", "abc", nil, StrVal("ABC")},
		{"asLowercase", "ABC", nil, StrVal("abc")},
		{"reversed", "hello", nil, StrVal("olleh")},
		{"capitalize", "hello world", nil, StrVal("Hello world")},
		{"capitalize", "", nil, StrVal("")},
		{"startsWith:", "hello", []Value{StrVal("he")}, True},
		{"endsWith:", "hello", []Value{StrVal("lo")}, True},
		{"includes:", "hello", []Value{StrVal("ell")}, True},
		{"indexOf:", "hello", []Value{StrVal("l")}, NumVal(2)},
		{"indexOf:", "hello", []Value{StrVal("z")}, NumVal(-1)},
		{"isEmpty", "", nil, True},
		{"notEmpty", "x", nil, True},
		{"trimSeparators", "  hi  ", nil, StrVal("hi")},
		{",", "foo", []Value{StrVal("bar")}, StrVal("foobar")},
		{"replaceAll:with:", "a.b.c", []Value{StrVal("."), StrVal("-")}, StrVal("a-b-c")},
		{"asNumber", "42", nil, NumVal(42)},
		// repeat:
		{"repeat:", "ab", []Value{NumVal(3)}, StrVal("ababab")},
		{"repeat:", "x", []Value{NumVal(0)}, StrVal("")},
		// count:
		{"count:", "hello world", []Value{StrVal("l")}, NumVal(3)},
		{"count:", "abcabc", []Value{StrVal("bc")}, NumVal(2)},
		// padLeft:with:
		{"padLeft:with:", "42", []Value{NumVal(5), StrVal("0")}, StrVal("00042")},
		{"padLeft:with:", "hello", []Value{NumVal(3), StrVal(" ")}, StrVal("hello")}, // already wide enough
		// padRight:with:
		{"padRight:with:", "hi", []Value{NumVal(5), StrVal(".")}, StrVal("hi...")},
		{"padRight:with:", "hello", []Value{NumVal(3), StrVal(".")}, StrVal("hello")}, // already wide enough
		{"asCharacter", "hi", nil, CharVal('h')},                                     // first rune
		{"asCharacter", "", nil, Nil},                                                // empty → nil
	}
	for _, tc := range cases {
		got := callStringMethod(o, tc.recv, tc.method, tc.args)
		if !valuesEqual(got, tc.want) {
			t.Errorf("%q %s %v = %v, want %v", tc.recv, tc.method, tc.args, got, tc.want)
		}
	}
}

func TestNumberMethods(t *testing.T) {
	o := newTestObject("Number")
	cases := []struct {
		method string
		recv   float64
		args   []Value
		want   Value
	}{
		{"+", 2, []Value{NumVal(3)}, NumVal(5)},
		{"-", 7, []Value{NumVal(4)}, NumVal(3)},
		{"*", 6, []Value{NumVal(7)}, NumVal(42)},
		{"/", 9, []Value{NumVal(2)}, NumVal(4.5)},
		{"abs", -7, nil, NumVal(7)},
		{"max:", 3, []Value{NumVal(9)}, NumVal(9)},
		{"min:", 3, []Value{NumVal(9)}, NumVal(3)},
		{"sqrt", 144, nil, NumVal(12)},
		{"factorial", 5, nil, NumVal(120)},
		{"<", 3, []Value{NumVal(4)}, True},
		{">", 3, []Value{NumVal(4)}, False},
		{"even", 4, nil, True},
		{"odd", 4, nil, False},
		{"between:and:", 5, []Value{NumVal(1), NumVal(10)}, True},
		// sign
		{"sign", 7, nil, NumVal(1)},
		{"sign", -3, nil, NumVal(-1)},
		{"sign", 0, nil, NumVal(0)},
		// bitwise
		{"bitAnd:", 12, []Value{NumVal(10)}, NumVal(8)},  // 1100 & 1010 = 1000
		{"bitOr:", 12, []Value{NumVal(10)}, NumVal(14)},  // 1100 | 1010 = 1110
		{"bitXor:", 12, []Value{NumVal(10)}, NumVal(6)},  // 1100 ^ 1010 = 0110
		{"bitShift:", 1, []Value{NumVal(3)}, NumVal(8)},  // 1 << 3 = 8
		{"bitShift:", 8, []Value{NumVal(-2)}, NumVal(2)}, // 8 >> 2 = 2
		{"asCharacter", 65, nil, CharVal('A')},
		{"asCharacter", 128512, nil, CharVal('😀')}, // non-BMP
	}
	for _, tc := range cases {
		got := callNumberMethod(o, tc.recv, tc.method, tc.args)
		if !valuesEqual(got, tc.want) {
			t.Errorf("%v %s %v = %v, want %v", tc.recv, tc.method, tc.args, got, tc.want)
		}
	}

	// random returns a float in [0, 1)
	r := callNumberMethod(o, 0, "random", nil)
	if r.Kind != "number" || r.Num < 0 || r.Num >= 1 {
		t.Errorf("random = %v, want number in [0,1)", r)
	}
}

func TestBoolMethods(t *testing.T) {
	o := newTestObject("Boolean")
	if got := callBoolMethod(o, true, "not", nil); got.Bool {
		t.Fatal("true not should be false")
	}
	if got := callBoolMethod(o, true, "and:", []Value{False}); got.Bool {
		t.Fatal("true and: false should be false")
	}
	if got := callBoolMethod(o, false, "or:", []Value{True}); !got.Bool {
		t.Fatal("false or: true should be true")
	}
}

func TestCharacterMethods(t *testing.T) {
	o := newTestObject("Character")
	cases := []struct {
		method string
		recv   rune
		args   []Value
		want   Value
	}{
		{"asInteger", 'a', nil, NumVal(97)},
		{"value", 'a', nil, NumVal(97)},
		{"asString", 'a', nil, StrVal("a")},
		{"printString", 'a', nil, StrVal("$a")},
		{"printString", ' ', nil, StrVal("$ ")},
		{"asUppercase", 'a', nil, CharVal('A')},
		{"asLowercase", 'A', nil, CharVal('a')},
		{"isLetter", 'a', nil, True},
		{"isLetter", '5', nil, False},
		{"isDigit", '5', nil, True},
		{"isUppercase", 'A', nil, True},
		{"isLowercase", 'A', nil, False},
		{"isSpace", ' ', nil, True},
		{"=", 'a', []Value{CharVal('a')}, True},
		{"=", 'a', []Value{CharVal('b')}, False},
		{"~=", 'a', []Value{CharVal('b')}, True},
		{"<", 'a', []Value{CharVal('b')}, True},
		{">", 'a', []Value{CharVal('b')}, False},
		{"class", 'a', nil, StrVal("Character")},
		// Full-Unicode: a non-BMP code point (U+1F600) survives round-trips.
		{"asInteger", '😀', nil, NumVal(128512)},
		{"asUppercase", '😀', nil, CharVal('😀')}, // no case mapping; unchanged
	}
	for _, tc := range cases {
		got := callCharacterMethod(o, tc.recv, tc.method, tc.args)
		if !valuesEqual(got, tc.want) {
			t.Errorf("$%c %s %v = %v, want %v", tc.recv, tc.method, tc.args, got, tc.want)
		}
	}
}

// valuesEqual compares two Values by kind and payload for the kinds the
// collection/primitive methods return.
func valuesEqual(a, b Value) bool {
	if a.Kind != b.Kind {
		// treat "" and "nil" as the same nil kind
		aNil := a.Kind == "" || a.Kind == "nil"
		bNil := b.Kind == "" || b.Kind == "nil"
		if aNil && bNil {
			return true
		}
		return false
	}
	switch a.Kind {
	case "number":
		return a.Num == b.Num
	case "string":
		return a.Str == b.Str
	case "bool":
		return a.Bool == b.Bool
	case "objid":
		return a.ObjID == b.ObjID
	case "character":
		return a.Char == b.Char
	}
	return true
}

package main

import (
	"fmt"
	"path/filepath"
	"testing"
)

func useInheritanceClassPath(t *testing.T, paths ...string) {
	t.Helper()
	previous := classPath
	classPath = append([]string{}, paths...)
	t.Cleanup(func() { classPath = previous })
}

func requireInheritanceString(t *testing.T, got Value, want string, operation string) {
	t.Helper()
	if got.Kind != "string" || got.Str != want {
		t.Fatalf("%s = kind %q value %q, want string %q", operation, got.Kind, got.Str, want)
	}
}

func TestThreeLevelInheritanceFixture(t *testing.T) {
	useInheritanceClassPath(t, ".")
	classDef, loadErr := LoadClass("Tiger")
	if loadErr != nil {
		t.Fatalf("LoadClass(Tiger): %v", loadErr)
	}
	if classDef.Name != "Tiger" || classDef.Superclass == nil || classDef.Superclass.Name != "Cat" || classDef.Superclass.Superclass == nil || classDef.Superclass.Superclass.Name != "Animal" {
		t.Fatalf("Tiger class chain = %#v, want Tiger -> Cat -> Animal", classDef)
	}

	vars := initialInstanceVars(classDef)
	for _, name := range []string{"name", "sound", "tricks", "stripes"} {
		if value, exists := vars[name]; !exists || value.Kind != "nil" {
			t.Fatalf("initial field %q = %#v exists=%v, want declared nil field", name, value, exists)
		}
	}
	if len(vars) != 4 {
		t.Fatalf("initial fields = %v, want exactly name, sound, tricks, stripes", vars)
	}

	object := &ObjectServer{id: "tiger-test", classDef: classDef, instanceVars: vars}
	object.callMethod("initialize", nil)
	requireInheritanceString(t, object.callMethod("name", nil), "tiger", "grandparent name method")
	requireInheritanceString(t, object.callMethod("speak", nil), "tiger says roar", "grandparent speak method")
	object.callMethod("learnTrick", nil)
	object.callMethod("learnTrick", nil)
	if got := object.callMethod("trickCount", nil); got.Kind != "number" || got.Num != 2 {
		t.Fatalf("inherited parent trickCount = %#v, want number 2", got)
	}
	if got := object.callMethod("stripeCount", nil); got.Kind != "number" || got.Num != 21 {
		t.Fatalf("Tiger stripeCount = %#v, want number 21", got)
	}
	requireInheritanceString(t, object.callMethod("describe", nil), "I am a tiger that knows 2 tricks with 21 stripes", "two-hop super describe")

	method, definingClass, found := lookupMethod(classDef, "speak")
	if !found || method == nil || definingClass == nil || definingClass.Name != "Animal" {
		t.Fatalf("Tiger speak lookup = method=%v class=%v found=%v, want Animal method", method, definingClass, found)
	}
	if !classAllowsSetVar(classDef, "name") || !classAllowsSetVar(classDef, "tricks") || !classAllowsSetVar(classDef, "stripes") || classAllowsSetVar(classDef, "missing") {
		t.Fatalf("Tiger writable fields did not match the complete inheritance chain")
	}
}

func TestThreeLevelInheritanceMissingTransitiveSuperclass(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Leaf.st"), "class Leaf extends Middle []")
	mustWrite(t, filepath.Join(dir, "Middle.st"), "class Middle extends Missing []")
	useInheritanceClassPath(t, dir)

	_, loadErr := LoadClass("Leaf")
	expected := fmt.Sprintf("loading superclass of Leaf: loading superclass of Middle: cannot find Missing.smt, Missing.st, or Missing/ in classpath %s", dir)
	if loadErr == nil || loadErr.Error() != expected {
		t.Fatalf("LoadClass(Leaf) error = %v, want %q", loadErr, expected)
	}
}

func TestThreeLevelInheritanceCycle(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "First.st"), "class First extends Second []")
	mustWrite(t, filepath.Join(dir, "Second.st"), "class Second extends Third []")
	mustWrite(t, filepath.Join(dir, "Third.st"), "class Third extends First []")
	useInheritanceClassPath(t, dir)

	_, loadErr := LoadClass("First")
	want := "loading superclass of First: loading superclass of Second: loading superclass of Third: cyclic superclass chain: First -> Second -> Third -> First"
	if loadErr == nil || loadErr.Error() != want {
		t.Fatalf("LoadClass(First) error = %v, want chain %q", loadErr, want)
	}
}

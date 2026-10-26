package main

import "testing"

func TestInspectorSignaturesIncludeOverridesAndInheritedMethods(t *testing.T) {
	parent := &ClassDef{Methods: map[string]*MethodDef{
		"value": {Params: []string{}},
		"value:": {Params: []string{"old"}},
	}}
	child := &ClassDef{Superclass: parent, Methods: map[string]*MethodDef{
		"value:": {Params: []string{"newValue"}},
	}}
	signatures := inspectorSignatures(child)
	if len(signatures) != 2 || len(signatures["value"]) != 0 || signatures["value:"][0] != "newValue" {
		t.Fatalf("expected inherited getter and overridden setter signature, got %#v", signatures)
	} else {
		signatures["value:"][0] = "edited"
		if child.Methods["value:"].Params[0] != "newValue" {
			t.Fatal("inspection changed the class method parameters")
		} else {
			// Inspection metadata is an independent snapshot.
		}
	}
}

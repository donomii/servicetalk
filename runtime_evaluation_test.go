package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvaluationPrograms(t *testing.T) {
	files, err := filepath.Glob("examples/evaluation/*_test.smt")
	if err != nil { t.Fatal(err) } else {}
	if len(files) != 5 { t.Fatalf("expected five evaluation programs, got %v", files) } else {}
	output, err := scriptTestCommand(t, ".", append([]string{"test"}, files...)...)
	if err != nil || !strings.Contains(output, "5 passed, 0 failed") { t.Fatalf("evaluation programs: %v\n%s", err, output) } else {}
}

func TestUnknownMessagesPropagateAndHandlersRemainUsable(t *testing.T) {
	dir := t.TempDir()
	classes := map[string]string{
		"Unhandled.st": "class Unhandled [ fromSuper [ ^ super absentSelector ] ]",
		"HandlesUnknown.st": "class HandlesUnknown [ doesNotUnderstand: selector [ ^ 'handled: ' , selector ] ]",
		"InheritsHandler.st": "class InheritsHandler extends HandlesUnknown [ fromSuper [ ^ super absentSelector ] ]",
	}
	for name, source := range classes {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil { t.Fatal(err) } else {}
	}
	broker := startNATSTestServerOnPort(t, -1)
	t.Cleanup(broker.Shutdown)
	base := startMCPTestCoordinator(t, broker.ClientURL(), dir)
	for _, receiver := range []string{"'hello'", "42", "true", "nil", "$x", "{}", "Dictionary new", "[42]", "Unhandled new", "HttpClient new", "HttpServer new", "RobotsMatcher new", "SitemapParser new", "Process new", "self", "coordinator"} {
		t.Run(receiver, func(t *testing.T) {
			result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: receiver + " absentSelector. 'execution continued' signal"})
			if !result.IsError || !strings.Contains(result.Content[0].Text, "absentSelector") || strings.Contains(result.Content[0].Text, "execution continued") { t.Fatalf("unsupported send was not an immediate error: %+v", result) } else {}
		})
	}
	for _, code := range []string{"Unhandled new fromSuper", "missingEvaluationVariable + 2"} {
		result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: code})
		if !result.IsError || !strings.Contains(result.Content[0].Text, "does not understand") { t.Fatalf("%s: %+v", code, result) } else {}
	}
	for _, code := range []string{"HandlesUnknown new absentSelector", "InheritsHandler new absentSelector", "InheritsHandler new fromSuper", "[Unhandled new absentSelector] onError: [:error | 'handled: absentSelector']"} {
		if got := sourceTestEval(t, base, code); got != "handled: absentSelector" { t.Fatalf("custom or explicit handler changed: %s -> %s", code, got) } else {}
	}
	if got := sourceTestEval(t, base, "6 * 7"); got != "42" { t.Fatalf("workspace did not recover: %s", got) } else {}
	missing := mcpTestCall(t, base, "eval", MCPToolArguments{Code: "MissingEvaluationClass new. 'execution continued' signal"})
	if !missing.IsError || !strings.Contains(missing.Content[0].Text, "MissingEvaluationClass") || strings.Contains(missing.Content[0].Text, "execution continued") { t.Fatalf("missing class was not an immediate error: %+v", missing) } else {}
}

func TestComparatorReadsAndFailuresKeepArrayUsable(t *testing.T) {
	broker := startNATSTestServerOnPort(t, -1)
	t.Cleanup(broker.Shutdown)
	base := startMCPTestCoordinator(t, broker.ClientURL(), ".")
	for _, test := range []struct{ code, want string }{
		{"a := {3. 1. 2}. a sort: [:x :y | a size. x < y]. a join: ','", "1,2,3"},
		{"a := {3. 1. 2}. [a sort: [:x :y | 'comparison failed' signal]] onError: [:error | nil]. a add: 4. a join: ','", "3,1,2,4"},
		{"a := {3. 1. 2}. error := [a sort: [:x :y | a add: 9. x < y]] onError: [:message | message]. (error includes: 'changed') and: [a includes: 9]", "true"},
		{"workspace := ObjectWorkspace new. workspace target: a. workspace evaluate: 'size'. workspace result = a size", "true"},
		{"workspace evaluate: 'missingScratch'. workspace result", "nil"},
	} {
		if got := sourceTestEval(t, base, test.code); got != test.want { t.Fatalf("%s -> %s, want %s", test.code, got, test.want) } else {}
	}
}

func TestScriptRejectsMissingMessagesAndClasses(t *testing.T) {
	dir := t.TempDir()
	for name, source := range map[string]string{"a_test.smt": "'text' absentSelector", "b_test.smt": "MissingEvaluationClass new", "c_test.smt": "6 * 7"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(source), 0600); err != nil { t.Fatal(err) } else {}
	}
	output, err := scriptTestCommand(t, dir, "test")
	if err == nil || !strings.Contains(output, "1 passed, 2 failed") || !strings.Contains(output, "absentSelector") || !strings.Contains(output, "MissingEvaluationClass") { t.Fatalf("script failures were missed: %v\n%s", err, output) } else {}
}

func TestNumericDivisionFailuresAndModuloSyntax(t *testing.T) {
	broker := startNATSTestServerOnPort(t, -1)
	t.Cleanup(broker.Shutdown)
	base := startMCPTestCoordinator(t, broker.ClientURL(), ".")
	for _, code := range []string{"1 / 0", "1 // 0", `1 \\ 0`, "0 reciprocal", "1 / 'oops'"} {
		result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: code + ". 'execution continued' signal"})
		if !result.IsError || !strings.Contains(result.Content[0].Text, "nonzero") || strings.Contains(result.Content[0].Text, "execution continued") { t.Fatalf("invalid numeric operation did not signal: %s -> %+v", code, result) } else {}
	}
	for _, code := range []string{`17 \\ 5`, `17 perform: #\\ with: 5`} {
		if got := sourceTestEval(t, base, code); got != "2" { t.Fatalf("modulo syntax %s -> %s", code, got) } else {}
	}
	if got := sourceTestEval(t, base, "[1 / 0] onError: [:error | 42]"); got != "42" { t.Fatalf("division error was not catchable: %s", got) } else {}
	for _, code := range []string{"''", "'text' copyFrom: 0 to: 0"} {
		result := mcpTestCall(t, base, "eval", MCPToolArguments{Code: code})
		if result.IsError || result.Content[0].Text != "" { t.Fatalf("empty string became nil: %s -> %+v", code, result) } else {}
	}
	if got := sourceTestEval(t, base, "nil"); got != "nil" { t.Fatalf("nil output changed: %s", got) } else {}
}

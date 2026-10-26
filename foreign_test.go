package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// foreignSourcesMention should see author code but skip generated glue.
func TestForeignSourcesMention(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "impl.c"), []byte("Value f(){ st_send(x); }"), 0644); err != nil {
		t.Fatal(err)
	}
	// Generated glue mentioning st_send must NOT count.
	if err := os.WriteFile(filepath.Join(dir, "_st_glue.c"), []byte("st_send everywhere"), 0644); err != nil {
		t.Fatal(err)
	}
	if !foreignSourcesMention(dir, []string{".c", ".h"}, "st_send") {
		t.Fatal("expected impl.c st_send to be found")
	}

	clean := t.TempDir()
	if err := os.WriteFile(filepath.Join(clean, "_st_glue.c"), []byte("st_send only in glue"), 0644); err != nil {
		t.Fatal(err)
	}
	if foreignSourcesMention(clean, []string{".c", ".h"}, "st_send") {
		t.Fatal("generated glue should be skipped")
	}
}

// writeForeignGitignore writes the right entries and never clobbers an
// existing file.
func TestWriteForeignGitignore(t *testing.T) {
	dir := t.TempDir()
	info := &ForeignClassInfo{Dir: dir, BinaryPath: filepath.Join(dir, "Foo"), Lang: "c"}
	writeForeignGitignore(info)
	got, err := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if err != nil {
		t.Fatalf("gitignore not written: %v", err)
	}
	for _, want := range []string{"_st.h", "_st_runtime.c", "_st_glue.c", "Foo"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("gitignore missing %q; got:\n%s", want, got)
		}
	}

	// A pre-existing .gitignore is left untouched.
	if err := os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("custom\n"), 0644); err != nil {
		t.Fatal(err)
	}
	writeForeignGitignore(info)
	got, _ = os.ReadFile(filepath.Join(dir, ".gitignore"))
	if string(got) != "custom\n" {
		t.Fatalf("existing .gitignore was overwritten: %s", got)
	}

	rust := &ForeignClassInfo{Dir: t.TempDir(), BinaryPath: filepath.Join(t.TempDir(), "Bar"), Lang: "rust"}
	rust.Dir = filepath.Dir(rust.BinaryPath)
	writeForeignGitignore(rust)
	got, _ = os.ReadFile(filepath.Join(rust.Dir, ".gitignore"))
	for _, want := range []string{"src/_st_main.rs", "/target", "Bar"} {
		if !strings.Contains(string(got), want) {
			t.Fatalf("rust gitignore missing %q; got:\n%s", want, got)
		}
	}
}

func TestForeignCArrayHelpersAreGenerated(t *testing.T) {
	header := string(stHeaderC)
	for _, want := range []string{
		"int   st_arraySize(Value arr);",
		"Value st_arrayAt(Value arr, int index);",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("C header missing %q", want)
		}
	}

	runtime := string(stRuntimeC)
	for _, want := range []string{
		`Value result = st_send(arr, "size", 0, NULL);`,
		`return st_send(arr, "at:", 1, &arg);`,
	} {
		if !strings.Contains(runtime, want) {
			t.Fatalf("C runtime missing %q", want)
		}
	}
}

func TestForeignBinaryNameUsesTargetOS(t *testing.T) {
	t.Setenv("SMT_FOREIGN_TARGET_GOOS", "windows")
	if got := foreignBinaryName("Thing"); got != "Thing.exe" {
		t.Fatalf("foreignBinaryName windows = %q, want Thing.exe", got)
	}
	t.Setenv("SMT_FOREIGN_TARGET_GOOS", "linux")
	if got := foreignBinaryName("Thing"); got != "Thing" {
		t.Fatalf("foreignBinaryName linux = %q, want Thing", got)
	}
}

func TestForeignExecEngineManifest(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "PyThing")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	src := `
(foreign-class PyThing
  (engine python3 worker.py "{class}")
  (emethod value)
  (emethod pulse:))
`
	if err := os.WriteFile(filepath.Join(dir, "PyThing.smt"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	cd, err := LoadForeignClass(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cd.Foreign.Lang != "exec" {
		t.Fatalf("Lang = %q, want exec", cd.Foreign.Lang)
	}
	wantEngine := []string{"python3", "worker.py", "{class}"}
	if strings.Join(cd.Foreign.Engine, " ") != strings.Join(wantEngine, " ") {
		t.Fatalf("Engine = %v, want %v", cd.Foreign.Engine, wantEngine)
	}
	for _, method := range []string{"value", "pulse:"} {
		if _, ok := cd.Methods[method]; !ok {
			t.Fatalf("missing method %s", method)
		}
	}
	if err := validateForeignExec(cd.Foreign); err != nil {
		t.Fatalf("validate exec: %v", err)
	}
}

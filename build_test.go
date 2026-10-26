package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func TestCollectAppClasses(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "Alpha.smt"), "(class Alpha)")
	mustWrite(t, filepath.Join(dir, "Beta.st"), "class Beta []")
	mustWrite(t, filepath.Join(dir, "readme.txt"), "ignore me")
	// A foreign class is a dir holding <name>/<name>.smt.
	mustWrite(t, filepath.Join(dir, "Gamma", "Gamma.smt"), "(foreign-class Gamma)")

	files, foreign, err := collectAppClasses(dir)
	if err != nil {
		t.Fatalf("collectAppClasses: %v", err)
	}
	if len(files) != 2 || files["Alpha.smt"] == "" || files["Beta.st"] == "" {
		t.Fatalf("class files = %v, want Alpha.smt + Beta.st", files)
	}
	if len(foreign) != 1 || foreign[0] != "Gamma" {
		t.Fatalf("foreign = %v, want [Gamma]", foreign)
	}
}

func TestValidateAppClasses(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "Alpha.st")
	mustWrite(t, valid, "class Alpha []")
	if err := validateAppClasses(map[string]string{"Alpha.st": valid}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		file    string
		source  string
		wantErr string
	}{
		{"syntax", "Broken.st", "class Broken [", "parse"},
		{"filename mismatch", "Expected.smt", "(class Actual)", "expected Expected"},
		{"multiple classes", "First.st", "class First [] class Second []", "declares 2 classes"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(dir, test.file)
			mustWrite(t, path, test.source)
			err := validateAppClasses(map[string]string{test.file: path})
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("validateAppClasses error = %v, want substring %q", err, test.wantErr)
			}
		})
	}
}

// TestEmbeddedClassLoading drives the loader hook that `smt build` relies on:
// when embeddedClasses is set, LoadClass resolves from it (taking priority
// over disk) without any file on the classpath.
func TestEmbeddedClassLoading(t *testing.T) {
	prev := embeddedClasses
	embeddedClasses = fstest.MapFS{
		"Widget.smt": &fstest.MapFile{Data: []byte("(class Widget (method ping () 42))")},
	}
	t.Cleanup(func() { embeddedClasses = prev })

	cd, err := LoadClass("Widget")
	if err != nil {
		t.Fatalf("LoadClass(Widget) from embed: %v", err)
	}
	if cd.Name != "Widget" {
		t.Fatalf("name = %q, want Widget", cd.Name)
	}
	if _, ok := cd.Methods["ping"]; !ok {
		t.Fatalf("method ping missing; methods = %v", cd.Methods)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

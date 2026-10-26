package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestClassBundleFromClassPathStagesSources(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "Main.smt"), []byte("(class Main)"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Helper.st"), []byte("(class Helper)"), 0644); err != nil {
		t.Fatal(err)
	}
	foreignDir := filepath.Join(dir, "Thing")
	if err := os.MkdirAll(filepath.Join(foreignDir, "src"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignDir, "Thing.smt"), []byte("(foreign-class Thing)"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignDir, "thing.c"), []byte("int x;"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignDir, "_st_glue.c"), []byte("generated"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(foreignDir, "target"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(foreignDir, "target", "ignored"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}
	looseDir := filepath.Join(dir, "Loose")
	if err := os.MkdirAll(looseDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(looseDir, "loose.c"), []byte("ignored"), 0644); err != nil {
		t.Fatal(err)
	}

	oldClassPath := classPath
	classPath = []string{dir, dir}
	defer func() { classPath = oldClassPath }()

	bundle, err := classBundleFromClassPath()
	if err != nil {
		t.Fatal(err)
	}
	if bundle == nil || bundle.Digest == "" {
		t.Fatalf("bundle = %+v, want digest", bundle)
	}
	got := map[string]bool{}
	for _, file := range bundle.Files {
		got[file.Path] = true
	}
	for _, want := range []string{"Helper.st", "Main.smt", "Thing/Thing.smt", "Thing/thing.c"} {
		if !got[want] {
			t.Fatalf("bundle missing %s; got %v", want, got)
		}
	}
	if got["Thing/_st_glue.c"] || got["Thing/target/ignored"] || got["Loose/loose.c"] {
		t.Fatalf("bundle included generated files: %v", got)
	}
	if len(got) != len(bundle.Files) {
		t.Fatalf("bundle included duplicate file paths: %+v", bundle.Files)
	}

	staged, err := stageSourceBundle(bundle)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(staged, "Thing", "thing.c")); err != nil {
		t.Fatalf("staged foreign source missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staged, "Helper.st")); err != nil {
		t.Fatalf("staged class source missing: %v", err)
	}
}

func TestSafeBundlePathRejectsTraversal(t *testing.T) {
	if _, err := safeBundlePath("../outside.smt"); err == nil {
		t.Fatal("safeBundlePath accepted traversal")
	}
	if _, err := safeBundlePath("/outside.smt"); err == nil {
		t.Fatal("safeBundlePath accepted absolute path")
	}
	if _, err := safeBundlePath("..\\outside.smt"); err == nil {
		t.Fatal("safeBundlePath accepted backslash traversal")
	}
	if _, err := safeBundlePath("C:/outside.smt"); err == nil {
		t.Fatal("safeBundlePath accepted drive path")
	}
}

func TestStageSourceBundleRejectsDigestMismatch(t *testing.T) {
	files := []SourceBundleFile{{Path: "Main.smt", Data: []byte("(class Main)")}}
	bundle := &SourceBundle{Digest: digestSourceBundle(files), Files: files}
	bundle.Files[0].Data = []byte("(class Changed)")
	if _, err := stageSourceBundle(bundle); err == nil {
		t.Fatal("stageSourceBundle accepted a mismatched digest")
	}
}

func TestSourceBundleRejectsOversizedBundle(t *testing.T) {
	t.Setenv(sourceBundleMaxBytesVar, "4")
	files := []SourceBundleFile{{Path: "Main.smt", Data: []byte("12345")}}
	if err := validateSourceBundleSize(files); err == nil {
		t.Fatal("validateSourceBundleSize accepted an oversized bundle")
	}
}

func TestSourceBundleRejectsTooManyFiles(t *testing.T) {
	t.Setenv(sourceBundleMaxFilesVar, "1")
	files := []SourceBundleFile{
		{Path: "Main.smt", Data: []byte("1")},
		{Path: "Other.smt", Data: []byte("2")},
	}
	if err := validateSourceBundleSize(files); err == nil {
		t.Fatal("validateSourceBundleSize accepted too many files")
	}
}

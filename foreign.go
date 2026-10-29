package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// A foreign class is a directory containing:
//   - ClassName.smt     — (foreign-class ...) manifest listing cmethods
//   - one or more .c    — user implementations
// At spawn time we generate _st.h, _st_runtime.c, _st_glue.c into the same
// directory, shell out to $CC (default "cc"), and then launch the resulting
// binary as a subprocess.  Messages are driven over stdin/stdout using a
// small length-prefixed binary protocol (see op* / kind* constants below).
// All HTTP/registry/heartbeat work stays in Go; the C side only implements
// the method bodies and talks back via st_send.

// ---- Protocol constants (must match _st_runtime.c) ----

const (
	opCall      = 0x01 // parent → child: start a method call
	opReturn    = 0x02 // child → parent: final result for current call
	opSend      = 0x03 // child → parent: please deliver a message
	opSendReply = 0x04 // parent → child: reply to a send
	opSignal    = 0x05 // child → parent: raise a signal
)

const (
	kindNil    = 0x01
	kindBool   = 0x02
	kindNumber = 0x03
	kindString = 0x04
	kindObjID  = 0x05
)

// ---- ClassDef extension ----

type ForeignClassInfo struct {
	Dir        string
	BinaryPath string
	Lang       string // "c" (default), "rust", or "exec"
	Engine     []string
	Methods    []ForeignMethod
}

type ForeignMethod struct {
	Selector string // ServiceTalk selector (e.g. "increment", "at:put:")
	CFunc    string // C function name OR Rust path (e.g. "ccounter_increment" / "increment")
}

// ---- Manifest loading ----

// PrebuildAllForeign scans every directory on the classpath for
// foreign-class manifests (<name>/<name>.smt with a (foreign-class …)
// form) and builds each one for the current OS+arch.  Idempotent:
// classes already up-to-date are skipped without invoking $CC.
//
// Intended invocation points:
//
//   - From the bundle's Dockerfile, as a `RUN /app/run prebuild` step
//     during image build.  Combined with `docker buildx --platform`,
//     this produces per-arch images with their foreign binaries
//     already compiled — no first-spawn delay, no runtime $CC need.
//   - From an init container or one-shot Job in Kubernetes before the
//     coordinator/workers come up.
//   - On bare metal, run once on each host that will spawn the class.
//
// The function returns the first build error encountered.  Names are
// deduplicated across classpath dirs so a class shadowed by an
// earlier classpath entry is built once.
func PrebuildAllForeign() error {
	seen := map[string]bool{}
	var built, skipped, scanned int
	for _, base := range classPath {
		entries, err := os.ReadDir(base)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if seen[name] {
				continue
			}
			dir := filepath.Join(base, name)
			if _, err := os.Stat(filepath.Join(dir, name+".smt")); err != nil {
				continue
			}
			cd, err := LoadForeignClass(dir)
			if err != nil {
				continue // not actually a foreign class
			}
			if cd.Foreign == nil {
				continue
			}
			seen[name] = true
			scanned++
			var wasUpToDate bool
			if cd.Foreign.Lang == "exec" {
				wasUpToDate = true
			} else if cd.Foreign.Lang == "rust" {
				wasUpToDate = upToDateRust(cd.Foreign)
			} else {
				wasUpToDate = upToDate(cd.Foreign)
			}
			if err := BuildForeignClass(cd.Foreign); err != nil {
				return fmt.Errorf("build %s: %w", name, err)
			}
			if wasUpToDate {
				skipped++
				fmt.Printf("up-to-date  %s\n", cd.Foreign.BinaryPath)
			} else {
				built++
				fmt.Printf("built       %s\n", cd.Foreign.BinaryPath)
			}
		}
	}
	fmt.Printf("prebuild: scanned %d, built %d, already-current %d\n", scanned, built, skipped)
	return nil
}

// LoadForeignClass reads <dir>/<basename(dir)>.smt and returns a ClassDef
// whose .Foreign field carries the dispatch table needed to build and
// run the class.  The classpath search happens in LoadClass; this
// function takes the resolved directory path directly and rewrites it
// to absolute so cc's working dir survives later CWD changes.
func LoadForeignClass(dir string) (*ClassDef, error) {
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	className := filepath.Base(dir)
	manifestPath := filepath.Join(dir, className+".smt")
	src, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("cannot read manifest %s: %w", manifestPath, err)
	}
	nodes, err := Parse(string(src))
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if node.Kind != NodeList || len(node.Children) < 2 {
			continue
		}
		head := node.Children[0]
		if head.Kind == NodeSymbol && head.Str == "foreign-class" {
			return parseForeignClassNode(node, dir)
		}
	}
	return nil, fmt.Errorf("no (foreign-class ...) form in %s", manifestPath)
}

func parseForeignClassNode(node *Node, dir string) (*ClassDef, error) {
	if node.Children[1].Kind != NodeSymbol {
		return nil, fmt.Errorf("foreign-class name must be a symbol")
	}
	name := node.Children[1].Str
	cd := &ClassDef{
		Name:    name,
		Methods: make(map[string]*MethodDef),
		Foreign: &ForeignClassInfo{
			Dir:        dir,
			BinaryPath: filepath.Join(dir, foreignBinaryName(name)),
		},
	}
	for _, child := range node.Children[2:] {
		if child.Kind != NodeList || len(child.Children) == 0 {
			continue
		}
		head := child.Children[0]
		if head.Kind != NodeSymbol {
			continue
		}
		switch head.Str {
		case "engine":
			if len(child.Children) < 2 {
				return nil, fmt.Errorf("engine expects (engine command [args...]) at line %d", child.Line)
			}
			if cd.Foreign.Lang != "" && cd.Foreign.Lang != "exec" {
				return nil, fmt.Errorf("foreign-class %s: engine mixed with %s at line %d", name, cd.Foreign.Lang, child.Line)
			}
			cd.Foreign.Lang = "exec"
			for _, arg := range child.Children[1:] {
				s, ok := foreignManifestAtom(arg)
				if !ok {
					return nil, fmt.Errorf("engine args must be symbols or strings at line %d", arg.Line)
				}
				cd.Foreign.Engine = append(cd.Foreign.Engine, s)
			}
		case "cmethod":
			if len(child.Children) < 3 || child.Children[1].Kind != NodeSymbol || child.Children[2].Kind != NodeSymbol {
				return nil, fmt.Errorf("cmethod expects (cmethod selector c-func) at line %d", child.Line)
			}
			if cd.Foreign.Lang != "" && cd.Foreign.Lang != "c" {
				return nil, fmt.Errorf("foreign-class %s: cmethod mixed with %s at line %d", name, cd.Foreign.Lang, child.Line)
			}
			cd.Foreign.Lang = "c"
			m := ForeignMethod{Selector: child.Children[1].Str, CFunc: child.Children[2].Str}
			cd.Foreign.Methods = append(cd.Foreign.Methods, m)
			cd.Methods[m.Selector] = &MethodDef{Name: m.Selector}
		case "rmethod":
			if len(child.Children) < 3 || child.Children[1].Kind != NodeSymbol || child.Children[2].Kind != NodeSymbol {
				return nil, fmt.Errorf("rmethod expects (rmethod selector rust-path) at line %d", child.Line)
			}
			if cd.Foreign.Lang != "" && cd.Foreign.Lang != "rust" {
				return nil, fmt.Errorf("foreign-class %s: rmethod mixed with %s at line %d", name, cd.Foreign.Lang, child.Line)
			}
			cd.Foreign.Lang = "rust"
			m := ForeignMethod{Selector: child.Children[1].Str, CFunc: child.Children[2].Str}
			cd.Foreign.Methods = append(cd.Foreign.Methods, m)
			cd.Methods[m.Selector] = &MethodDef{Name: m.Selector}
		case "emethod":
			if len(child.Children) < 2 || child.Children[1].Kind != NodeSymbol {
				return nil, fmt.Errorf("emethod expects (emethod selector) at line %d", child.Line)
			}
			if cd.Foreign.Lang != "" && cd.Foreign.Lang != "exec" {
				return nil, fmt.Errorf("foreign-class %s: emethod mixed with %s at line %d", name, cd.Foreign.Lang, child.Line)
			}
			cd.Foreign.Lang = "exec"
			m := ForeignMethod{Selector: child.Children[1].Str, CFunc: child.Children[1].Str}
			cd.Foreign.Methods = append(cd.Foreign.Methods, m)
			cd.Methods[m.Selector] = &MethodDef{Name: m.Selector}
		default:
			return nil, fmt.Errorf("foreign-class: unknown form %q at line %d", head.Str, head.Line)
		}
	}
	if cd.Foreign.Lang == "" {
		cd.Foreign.Lang = "c" // empty manifest defaults to C
	}
	if cd.Foreign.Lang == "exec" && len(cd.Foreign.Engine) == 0 {
		return nil, fmt.Errorf("foreign-class %s: exec engine requires (engine command [args...])", name)
	}
	return cd, nil
}

func foreignManifestAtom(n *Node) (string, bool) {
	if n.Kind == NodeSymbol || n.Kind == NodeString {
		return n.Str, true
	}
	return "", false
}

func foreignTargetOS() string {
	if v := os.Getenv("SMT_FOREIGN_TARGET_GOOS"); v != "" {
		return v
	}
	if v := os.Getenv("GOOS"); v != "" {
		return v
	}
	return runtime.GOOS
}

func foreignTargetArch() string {
	if v := os.Getenv("SMT_FOREIGN_TARGET_GOARCH"); v != "" {
		return v
	}
	if v := os.Getenv("GOARCH"); v != "" {
		return v
	}
	return runtime.GOARCH
}

func foreignBinaryName(className string) string {
	if foreignTargetOS() == "windows" {
		return className + ".exe"
	}
	return className
}

// ---- Build step ----

// BuildForeignClass writes the header, runtime, and generated glue into the
// class directory, then invokes $CC to produce the class binary.  Skips the
// rebuild if the binary is newer than all .c/.h/.smt files in the directory.
func BuildForeignClass(info *ForeignClassInfo) error {
	warnForeignMissingPulse(info)
	if info.Lang == "exec" {
		return validateForeignExec(info)
	}
	if info.Lang == "rust" {
		return buildForeignClassRust(info)
	}
	return buildForeignClassC(info)
}

// warnForeignMissingPulse prints an advisory when a foreign class calls
// back into the cluster (st_send in C, send(…) in Rust) but declares no
// `pulse:` method.  The runtime's default field-walker can't see object
// references held in native state, so without a hand-written pulse: those
// refs aren't kept alive and the pointee can be GC'd out from under the
// class.  Heuristic — it can't tell a transient call from a held ref —
// so it's a warning, never an error.  See README "Pulse-based GC".
func warnForeignMissingPulse(info *ForeignClassInfo) {
	for _, m := range info.Methods {
		if m.Selector == "pulse:" {
			return // author already owns liveness
		}
	}
	needle, exts, kw := "st_send", []string{".c", ".h"}, "cmethod"
	if info.Lang == "rust" {
		needle, exts, kw = "send(", []string{".rs"}, "rmethod"
	} else if info.Lang == "exec" {
		return
	}
	if !foreignSourcesMention(info.Dir, exts, needle) {
		return
	}
	name := filepath.Base(info.BinaryPath)
	fmt.Fprintf(os.Stderr,
		"warning: foreign class %s calls %s but declares no (%s pulse: …). "+
			"If it holds object references in native state, pulse-GC can't see "+
			"them and may collect the pointee; add a pulse: method that re-sends "+
			"pulse: to each held ref (see README \"Pulse-based GC\").\n",
		name, strings.TrimSuffix(needle, "("), kw)
}

// foreignSourcesMention reports whether any non-generated source file
// under dir (matching one of exts) contains needle.  Generated glue
// (_st*.c/.h/.rs) and the cargo target/ tree are skipped so the scan
// only sees code the author actually wrote.
func foreignSourcesMention(dir string, exts []string, needle string) bool {
	found := false
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() {
			return nil
		}
		base := filepath.Base(p)
		if strings.HasPrefix(base, "_st") || strings.Contains(p, string(os.PathSeparator)+"target"+string(os.PathSeparator)) {
			return nil
		}
		matchesExt := false
		for _, e := range exts {
			if strings.HasSuffix(base, e) {
				matchesExt = true
				break
			}
		}
		if !matchesExt {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte(needle)) {
			found = true
		}
		return nil
	})
	return found
}

// foreignGitignore lists the build artifacts a foreign-class directory
// generates, so they don't get committed.  Lang-specific: C drops three
// generated .c/.h files next to the sources; Rust drops one glue file
// under src/ plus the cargo target/ tree.  Both produce the class binary
// (named after the class) in the directory root.
func foreignGitignore(info *ForeignClassInfo) []byte {
	binName := filepath.Base(info.BinaryPath)
	if info.Lang == "exec" {
		return []byte("# generated by servicetalk\n")
	}
	if info.Lang == "rust" {
		return []byte("# generated by servicetalk\nsrc/_st_main.rs\n/target\n" + binName + "\n")
	}
	return []byte("# generated by servicetalk\n_st.h\n_st_runtime.c\n_st_glue.c\n" + binName + "\n")
}

// writeForeignGitignore writes a .gitignore into the class directory the
// first time the class is built.  Never overwrites — an author who has
// customised theirs keeps it.
func writeForeignGitignore(info *ForeignClassInfo) {
	path := filepath.Join(info.Dir, ".gitignore")
	if _, err := os.Stat(path); err == nil {
		return // already present — leave it alone
	}
	_ = os.WriteFile(path, foreignGitignore(info), 0644)
}

func validateForeignExec(info *ForeignClassInfo) error {
	if len(info.Engine) == 0 {
		return fmt.Errorf("exec foreign class %s has no engine command", filepath.Base(info.Dir))
	}
	argv := expandForeignEngine(info)
	if argv[0] == "" {
		return fmt.Errorf("exec foreign class %s has an empty engine command", filepath.Base(info.Dir))
	}
	return nil
}

func expandForeignEngine(info *ForeignClassInfo) []string {
	className := strings.TrimSuffix(filepath.Base(info.BinaryPath), ".exe")
	out := make([]string, 0, len(info.Engine))
	for _, arg := range info.Engine {
		arg = strings.ReplaceAll(arg, "{dir}", info.Dir)
		arg = strings.ReplaceAll(arg, "{class}", className)
		arg = strings.ReplaceAll(arg, "{binary}", info.BinaryPath)
		out = append(out, arg)
	}
	return out
}

func buildForeignClassC(info *ForeignClassInfo) error {
	writeForeignGitignore(info)
	if err := writeIfChanged(filepath.Join(info.Dir, "_st.h"), []byte(stHeaderC)); err != nil {
		return fmt.Errorf("write _st.h: %w", err)
	}
	if err := writeIfChanged(filepath.Join(info.Dir, "_st_runtime.c"), []byte(stRuntimeC)); err != nil {
		return fmt.Errorf("write _st_runtime.c: %w", err)
	}
	if err := writeIfChanged(filepath.Join(info.Dir, "_st_glue.c"), []byte(generateGlueC(info))); err != nil {
		return fmt.Errorf("write _st_glue.c: %w", err)
	}

	if upToDate(info) {
		return nil
	}

	entries, err := os.ReadDir(info.Dir)
	if err != nil {
		return err
	}
	sources := []string{"_st_runtime.c", "_st_glue.c"}
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, "_") || !strings.HasSuffix(name, ".c") {
			continue
		}
		sources = append(sources, name)
	}

	cc := os.Getenv("CC")
	if v := os.Getenv("SMT_FOREIGN_CC"); v != "" {
		cc = v
	}
	if cc == "" {
		cc = "cc"
	}
	binName := filepath.Base(info.BinaryPath)
	cmdArgs := append([]string{"-std=c99", "-O2", "-o", binName}, sources...)
	if flags := strings.Fields(os.Getenv("SMT_FOREIGN_CFLAGS")); len(flags) > 0 {
		cmdArgs = append(flags, cmdArgs...)
	}
	if flags := strings.Fields(os.Getenv("SMT_FOREIGN_LDFLAGS")); len(flags) > 0 {
		cmdArgs = append(cmdArgs, flags...)
	}
	cmd := exec.Command(cc, cmdArgs...)
	cmd.Dir = info.Dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	logInfo("[foreign] building %s: %s %s", info.BinaryPath, cc, strings.Join(cmdArgs, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s failed: %w", cc, err)
	}
	return nil
}

// buildForeignClassRust compiles a Rust foreign class:
//  1. Writes `src/_st_main.rs` with an auto-generated dispatch table
//     that calls into the user's lib (`use <pkg>::*;`) and runs
//     `servicetalk::main_loop`.  The file is overwritten on every
//     build; user code lives in `src/lib.rs` (and submodules).
//  2. Runs `cargo build --release`.
//  3. Copies the resulting binary from `target/release/<binName>` to
//     `<Dir>/<className>` so the spawn path (which exec's
//     info.BinaryPath) works identically to C.
//
// User's `Cargo.toml` must declare both a `[lib]` (auto-detected from
// `src/lib.rs`) and a `[[bin]]` named after the class with
// `path = "src/_st_main.rs"`, plus a `servicetalk` path-dep on the
// runtime crate.  See examples/RCounter/Cargo.toml for a worked
// minimum.
func buildForeignClassRust(info *ForeignClassInfo) error {
	writeForeignGitignore(info)
	srcDir := filepath.Join(info.Dir, "src")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		return fmt.Errorf("create src/: %w", err)
	}
	pkgName, err := readCargoPackageName(filepath.Join(info.Dir, "Cargo.toml"))
	if err != nil {
		return fmt.Errorf("read Cargo.toml: %w", err)
	}
	glue := generateGlueRust(info, pkgName)
	if err := writeIfChanged(filepath.Join(srcDir, "_st_main.rs"), []byte(glue)); err != nil {
		return fmt.Errorf("write _st_main.rs: %w", err)
	}

	if upToDateRust(info) {
		return nil
	}

	binName := filepath.Base(info.BinaryPath)
	cargo := os.Getenv("CARGO")
	if v := os.Getenv("SMT_FOREIGN_CARGO"); v != "" {
		cargo = v
	}
	if cargo == "" {
		cargo = "cargo"
	}
	cmdArgs := []string{"build", "--release", "--bin", binName}
	if target := os.Getenv("SMT_FOREIGN_RUST_TARGET"); target != "" {
		cmdArgs = append(cmdArgs, "--target", target)
	}
	cmd := exec.Command(cargo, cmdArgs...)
	cmd.Dir = info.Dir
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	logInfo("[foreign] building %s: %s %s", info.BinaryPath, cargo, strings.Join(cmdArgs, " "))
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cargo build failed: %w", err)
	}

	// Copy target/release/<binName> to <Dir>/<binName>.  Symlinking
	// would also work but copying lets bundle.sh treat the artifact
	// the same way it treats the C output.
	targetParts := []string{info.Dir, "target"}
	if target := os.Getenv("SMT_FOREIGN_RUST_TARGET"); target != "" {
		targetParts = append(targetParts, target)
	}
	targetParts = append(targetParts, "release", binName)
	srcBin := filepath.Join(targetParts...)
	if err := copyFile(srcBin, info.BinaryPath, 0755); err != nil {
		return fmt.Errorf("copy %s -> %s: %w", srcBin, info.BinaryPath, err)
	}
	return nil
}

// upToDateRust is the Rust analogue of upToDate — checks whether the
// output binary is newer than every `.rs` file under `src/` and the
// Cargo.toml.  Skips an expensive `cargo build` when nothing changed.
func upToDateRust(info *ForeignClassInfo) bool {
	binInfo, err := os.Stat(info.BinaryPath)
	if err != nil {
		return false
	}
	binMod := binInfo.ModTime()
	cargoToml, err := os.Stat(filepath.Join(info.Dir, "Cargo.toml"))
	if err != nil {
		return false
	}
	if cargoToml.ModTime().After(binMod) {
		return false
	}
	var newer bool
	_ = filepath.Walk(filepath.Join(info.Dir, "src"), func(p string, fi os.FileInfo, err error) error {
		if err != nil || fi.IsDir() || !strings.HasSuffix(p, ".rs") {
			return nil
		}
		if fi.ModTime().After(binMod) {
			newer = true
		}
		return nil
	})
	return !newer
}

// readCargoPackageName extracts `package.name` from a Cargo.toml by
// minimal hand-parse — avoids pulling in a toml dep for one field.
// Tolerates whitespace and CRLF; doesn't handle multi-line strings or
// inline-table forms (Cargo's own examples use single-line).
func readCargoPackageName(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	inPkg := false
	for _, raw := range strings.Split(string(b), "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "[") {
			inPkg = strings.HasPrefix(line, "[package]")
			continue
		}
		if !inPkg {
			continue
		}
		if !strings.HasPrefix(line, "name") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		val := strings.TrimSpace(line[eq+1:])
		val = strings.Trim(val, `"'`)
		if val == "" {
			continue
		}
		return val, nil
	}
	return "", fmt.Errorf("package.name not found in %s", path)
}

// copyFile copies src to dst with the given mode.  Used to lift the
// cargo target binary to the class dir so the spawn path is uniform
// across C and Rust foreign classes.
func copyFile(src, dst string, mode os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}

// writeIfChanged writes content to path only when it differs from what's
// already there.  Keeps mtimes stable across repeat spawns so upToDate()
// can skip the cc invocation when nothing actually changed.
func writeIfChanged(path string, content []byte) error {
	existing, err := os.ReadFile(path)
	if err == nil && bytes.Equal(existing, content) {
		return nil
	}
	return os.WriteFile(path, content, 0644)
}

func upToDate(info *ForeignClassInfo) bool {
	binInfo, err := os.Stat(info.BinaryPath)
	if err != nil {
		return false
	}
	entries, err := os.ReadDir(info.Dir)
	if err != nil {
		return false
	}
	binMod := binInfo.ModTime()
	for _, e := range entries {
		name := e.Name()
		if !(strings.HasSuffix(name, ".c") || strings.HasSuffix(name, ".h") || strings.HasSuffix(name, ".smt")) {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		if fi.ModTime().After(binMod) {
			return false
		}
	}
	return true
}

// generateGlueRust produces `src/_st_main.rs` — the auto-generated
// binary entry point for a Rust foreign class.  Imports the user's
// lib with `use <pkg>::*`, builds a `dispatch(selector, args)` match
// from the manifest's `(rmethod selector rust-path)` entries, and
// calls `servicetalk::main_loop`.
//
// Rust paths in the manifest are emitted verbatim — selector
// `"increment"` mapped to rust-path `increment` becomes
// `increment(args)` (resolved via the `use <pkg>::*;` import).  A
// fully-qualified path like `crate::helpers::increment` is also
// fine.
func generateGlueRust(info *ForeignClassInfo, pkgName string) string {
	var b strings.Builder
	b.WriteString("// auto-generated by servicetalk — do not edit\n")
	b.WriteString("use servicetalk::{Value, main_loop};\n")
	if pkgName != "" {
		fmt.Fprintf(&b, "use %s::*;\n", strings.ReplaceAll(pkgName, "-", "_"))
	}
	b.WriteString("\nfn dispatch(selector: &str, args: &[Value]) -> Value {\n")
	b.WriteString("    match selector {\n")
	for _, m := range info.Methods {
		fmt.Fprintf(&b, "        %q => %s(args),\n", m.Selector, m.CFunc)
	}
	b.WriteString("        _ => Value::Nil,\n")
	b.WriteString("    }\n}\n\n")
	b.WriteString("fn main() {\n    main_loop(dispatch);\n}\n")
	return b.String()
}

func generateGlueC(info *ForeignClassInfo) string {
	var b strings.Builder
	b.WriteString("/* auto-generated by servicetalk — do not edit */\n")
	b.WriteString("#include <string.h>\n")
	b.WriteString("#include \"_st.h\"\n\n")
	for _, m := range info.Methods {
		fmt.Fprintf(&b, "extern Value %s(int nargs, Value* args);\n", m.CFunc)
	}
	b.WriteString("\nstatic Value __dispatch(const char* selector, int nargs, Value* args) {\n")
	for _, m := range info.Methods {
		fmt.Fprintf(&b, "    if (strcmp(selector, %q) == 0) return %s(nargs, args);\n", m.Selector, m.CFunc)
	}
	b.WriteString("    return st_nil();\n")
	b.WriteString("}\n\n")
	b.WriteString("int main(void) {\n    st_main_loop(__dispatch);\n    return 0;\n}\n")
	return b.String()
}

// ---- Bridge: Go parent <-> C subprocess ----

type ForeignBridge struct {
	mu     sync.Mutex
	forceProcess *os.Process
	forceStopped atomic.Bool
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	obj    *ObjectServer
}

// NewForeignBridge builds (if needed) and launches the user binary, ready
// to accept method calls.  Caller must invoke Close() on shutdown.
func NewForeignBridge(obj *ObjectServer, info *ForeignClassInfo) (*ForeignBridge, error) {
	if err := BuildForeignClass(info); err != nil {
		return nil, err
	}
	cmd, err := foreignCommand(info)
	if err != nil {
		return nil, err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _ = cmd.Wait() }()
	return &ForeignBridge{
		forceProcess: cmd.Process,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdout),
		obj:    obj,
	}, nil
}

func foreignCommand(info *ForeignClassInfo) (*exec.Cmd, error) {
	if info.Lang == "exec" {
		argv := expandForeignEngine(info)
		if len(argv) == 0 || argv[0] == "" {
			return nil, fmt.Errorf("exec foreign class %s has no engine command", filepath.Base(info.Dir))
		}
		cmd := exec.Command(argv[0], argv[1:]...)
		cmd.Dir = info.Dir
		return cmd, nil
	}
	absBin, err := filepath.Abs(info.BinaryPath)
	if err != nil {
		return nil, err
	}
	return exec.Command(absBin), nil
}

// Close asks the user subprocess to exit by closing stdin.  Safe to call multiple times.
func (b *ForeignBridge) Close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.cmd == nil || b.cmd.Process == nil {
		return
	}
	b.stdin.Close()
	b.cmd = nil
}

// dispatch sends a method call to the child and drives the send/reply
// conversation until the child returns or signals.  Serialised via mu so
// concurrent incoming messages queue cleanly.
func (b *ForeignBridge) dispatch(method string, args []Value) Value {
	defer func(){ if b.forceStopped.Load() { panic(signalError{msg:"foreign method "+method+" on "+b.obj.id+" interrupted by forced retirement"}) } else {} }()
	b.mu.Lock()
	defer b.mu.Unlock()

	if err := writeCall(b.stdin, method, args); err != nil {
		logInfo("[foreign] write call failed: %v", err)
		return Nil
	}
	for {
		op, err := b.stdout.ReadByte()
		if err != nil {
			logInfo("[foreign] child closed stdout: %v", err)
			return Nil
		}
		switch op {
		case opReturn:
			v, err := readValue(b.stdout)
			if err != nil {
				return Nil
			}
			return v
		case opSend:
			target, err := readValue(b.stdout)
			if err != nil {
				return Nil
			}
			selector, err := readString(b.stdout)
			if err != nil {
				return Nil
			}
			nargs, err := readUint32(b.stdout)
			if err != nil {
				return Nil
			}
			sendArgs := make([]Value, nargs)
			for i := range sendArgs {
				v, err := readValue(b.stdout)
				if err != nil {
					return Nil
				}
				sendArgs[i] = v
			}
			result := b.obj.sendMessage(target, selector, sendArgs)
			if err := writeSendReply(b.stdin, result); err != nil {
				return Nil
			}
		case opSignal:
			msg, err := readString(b.stdout)
			if err != nil {
				return Nil
			}
			panic(signalError{msg: msg})
		default:
			logInfo("[foreign] unknown op byte: 0x%x", op)
			return Nil
		}
	}
}

// ---- Binary encode / decode ----

func writeCall(w io.Writer, method string, args []Value) error {
	if _, err := w.Write([]byte{opCall}); err != nil {
		return err
	}
	if err := writeLenString(w, method); err != nil {
		return err
	}
	if err := writeUint32(w, uint32(len(args))); err != nil {
		return err
	}
	for _, a := range args {
		if err := writeValue(w, a); err != nil {
			return err
		}
	}
	return nil
}

func writeSendReply(w io.Writer, v Value) error {
	if _, err := w.Write([]byte{opSendReply}); err != nil {
		return err
	}
	return writeValue(w, v)
}

func writeLenString(w io.Writer, s string) error {
	if err := writeUint32(w, uint32(len(s))); err != nil {
		return err
	}
	_, err := w.Write([]byte(s))
	return err
}

func writeUint32(w io.Writer, n uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], n)
	_, err := w.Write(buf[:])
	return err
}

func writeValue(w io.Writer, v Value) error {
	switch v.Kind {
	case "", "nil":
		_, err := w.Write([]byte{kindNil})
		return err
	case "bool":
		b := byte(0)
		if v.Bool {
			b = 1
		}
		_, err := w.Write([]byte{kindBool, b})
		return err
	case "number":
		if _, err := w.Write([]byte{kindNumber}); err != nil {
			return err
		}
		var buf [8]byte
		binary.LittleEndian.PutUint64(buf[:], math.Float64bits(v.Num))
		_, err := w.Write(buf[:])
		return err
	case "string":
		if _, err := w.Write([]byte{kindString}); err != nil {
			return err
		}
		return writeLenString(w, v.Str)
	case "objid":
		if _, err := w.Write([]byte{kindObjID}); err != nil {
			return err
		}
		return writeLenString(w, v.ObjID)
	}
	_, err := w.Write([]byte{kindNil})
	return err
}

func readValue(r *bufio.Reader) (Value, error) {
	kind, err := r.ReadByte()
	if err != nil {
		return Nil, err
	}
	switch kind {
	case kindNil:
		return Nil, nil
	case kindBool:
		b, err := r.ReadByte()
		if err != nil {
			return Nil, err
		}
		return BoolVal(b != 0), nil
	case kindNumber:
		var buf [8]byte
		if _, err := io.ReadFull(r, buf[:]); err != nil {
			return Nil, err
		}
		return NumVal(math.Float64frombits(binary.LittleEndian.Uint64(buf[:]))), nil
	case kindString:
		s, err := readString(r)
		if err != nil {
			return Nil, err
		}
		return StrVal(s), nil
	case kindObjID:
		s, err := readString(r)
		if err != nil {
			return Nil, err
		}
		return ObjVal(s), nil
	}
	return Nil, fmt.Errorf("unknown value kind: 0x%x", kind)
}

func readString(r *bufio.Reader) (string, error) {
	n, err := readUint32(r)
	if err != nil {
		return "", err
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(r, buf); err != nil {
		return "", err
	}
	return string(buf), nil
}

func readUint32(r *bufio.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

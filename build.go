package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// runRunCmd is the built-in app launcher: `smt run ./myapp [flags]` puts the
// app directory on the classpath, defaults --main to "Main", and starts the
// coordinator serving — so a directory of class files is a one-command
// runnable unit without remembering `coordinator --path … --main …`.
func runRunCmd(args []string) {
	// A leading non-flag argument is the app directory (Go's flag package
	// stops at the first positional, so peel it off before parsing).
	appDir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		appDir = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("run", flag.ExitOnError)
	port := fs.Int("port", 8000, "coordinator listen port")
	mainClass := fs.String("main", "Main", "class to spawn on startup (empty to skip)")
	eval := fs.String("eval", "", "evaluate this expression against the app, print, and exit")
	var paths pathFlag
	fs.Var(&paths, "path", "additional class directory (repeatable; also via SMT_PATH)")
	fs.BoolVar(&verbose, "verbose", true, "print lifecycle messages to stderr")
	fs.Parse(args)
	setVerbose(verbose)

	// The app directory leads the classpath so its classes win.
	SetClassPath(append([]string{appDir}, paths...))

	mode := ModeServe
	if *eval != "" {
		*mainClass = ""
		mode = ModeEval
	}
	runCoordinator(*port, *mainClass, mode, *eval)
}

// runBuildCmd compiles a self-contained single binary with the app's class
// files baked in via go:embed: `smt build ./myapp -o myapp`.  It needs the Go
// toolchain and the servicetalk source (located automatically from this
// binary's build path, or via --src), and rebuilds per app.  Pure-ServiceTalk
// apps only — foreign (C/Rust) classes need on-disk compilation; bundle those
// with bundle.sh instead.
func runBuildCmd(args []string) {
	appDir := "."
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		appDir = args[0]
		args = args[1:]
	}

	fs := flag.NewFlagSet("build", flag.ExitOnError)
	out := fs.String("o", "", "output binary path (default: ./<appdir-name>)")
	mainClass := fs.String("main", "Main", "class the built binary auto-spawns when run with no subcommand")
	srcDir := fs.String("src", "", "servicetalk source directory (default: auto-detect)")
	fs.Parse(args)

	appAbs, err := filepath.Abs(appDir)
	if err != nil {
		fatalf("build: %v", err)
	}
	classFiles, foreignDirs, err := collectAppClasses(appAbs)
	if err != nil {
		fatalf("build: %v", err)
	}
	if len(classFiles) == 0 {
		fatalf("build: no .smt or .st class files found in %s", appAbs)
	}
	if err := validateAppClasses(classFiles); err != nil {
		fatalf("build: %v", err)
	}
	for _, d := range foreignDirs {
		fmt.Fprintf(os.Stderr, "build: warning: skipping foreign class %q — single-binary builds are pure-ServiceTalk only; use bundle.sh for foreign classes\n", d)
	}
	if *mainClass != "" {
		if _, ok := classFiles[*mainClass+".smt"]; !ok {
			if _, ok := classFiles[*mainClass+".st"]; !ok {
				fatalf("build: --main %s has no %s.smt or %s.st in %s (pass --main '' to skip)", *mainClass, *mainClass, *mainClass, appAbs)
			}
		}
	}

	src, err := locateSource(*srcDir)
	if err != nil {
		fatalf("build: %v", err)
	}

	outPath := *out
	if outPath == "" {
		outPath = filepath.Base(appAbs)
		if outPath == "." || outPath == string(filepath.Separator) {
			outPath = "app"
		}
	}
	outAbs, err := filepath.Abs(outPath)
	if err != nil {
		fatalf("build: %v", err)
	}

	tmp, err := os.MkdirTemp("", "smt-build-")
	if err != nil {
		fatalf("build: %v", err)
	}
	defer os.RemoveAll(tmp)

	if err := stageSource(src, tmp); err != nil {
		fatalf("build: staging source: %v", err)
	}
	if err := stageAppFS(tmp, appAbs, classFiles); err != nil {
		fatalf("build: staging app: %v", err)
	}
	if err := writeEmbedFile(tmp); err != nil {
		fatalf("build: %v", err)
	}

	ldflags := "-s -w"
	if *mainClass != "" {
		ldflags += " -X main.builtMain=" + *mainClass
	}
	cmd := exec.Command("go", "build", "-trimpath", "-ldflags", ldflags, "-o", outAbs, ".")
	cmd.Dir = tmp
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fatalf("build: go build failed: %v", err)
	}

	fmt.Printf("built %s (%d class file(s) embedded", outAbs, len(classFiles))
	if *mainClass != "" {
		fmt.Printf(", main=%s", *mainClass)
	}
	fmt.Println(")")
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

// collectAppClasses returns the app's class source files (basename → abs path)
// and the names of foreign-class directories (skipped, reported by caller).
func collectAppClasses(appDir string) (map[string]string, []string, error) {
	entries, err := os.ReadDir(appDir)
	if err != nil {
		return nil, nil, err
	}
	files := map[string]string{}
	var foreign []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			// A directory holding <name>/<name>.smt is a foreign class.
			if _, err := os.Stat(filepath.Join(appDir, name, name+".smt")); err == nil {
				foreign = append(foreign, name)
			}
			continue
		}
		if strings.HasSuffix(name, ".smt") || strings.HasSuffix(name, ".st") {
			files[name] = filepath.Join(appDir, name)
		}
	}
	return files, foreign, nil
}

func validateAppClasses(classFiles map[string]string) error {
	for name, path := range classFiles {
		source, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		var nodes []*Node
		if strings.HasSuffix(name, ".st") {
			nodes, err = ParseSmalltalk(string(source))
		} else {
			nodes, err = Parse(string(source))
		}
		if err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}
		var classNodes []*Node
		for _, node := range nodes {
			if node.Kind == NodeList && len(node.Children) >= 2 && node.Children[0].Kind == NodeSymbol && node.Children[0].Str == "class" {
				classNodes = append(classNodes, node)
			}
		}
		if len(classNodes) != 1 {
			return fmt.Errorf("%s declares %d classes; each class file must declare exactly one class", path, len(classNodes))
		}
		classDef, err := parseClassNode(classNodes[0])
		if err != nil {
			return fmt.Errorf("parse class in %s: %w", path, err)
		}
		expectedName := strings.TrimSuffix(name, filepath.Ext(name))
		if classDef.Name != expectedName {
			return fmt.Errorf("%s declares class %s; expected %s to match the filename", path, classDef.Name, expectedName)
		}
	}
	return nil
}

// locateSource finds the servicetalk Go source directory.  Preference:
// explicit --src, then this binary's compile-time source path (works on the
// machine that built smt), then the current working directory.
func locateSource(override string) (string, error) {
	candidates := []string{}
	if override != "" {
		candidates = append(candidates, override)
	}
	if _, file, _, ok := runtime.Caller(0); ok {
		candidates = append(candidates, filepath.Dir(file))
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates, cwd)
	}
	for _, c := range candidates {
		if isServicetalkSource(c) {
			return c, nil
		}
	}
	return "", fmt.Errorf("cannot find servicetalk source (looked in %v); run from the source checkout or pass --src DIR", candidates)
}

// isServicetalkSource is a cheap sanity check: the dir must hold go.mod and the
// package's own main.go + coordinator.go.
func isServicetalkSource(dir string) bool {
	for _, f := range []string{"go.mod", "main.go", "coordinator.go"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err != nil {
			return false
		}
	}
	return true
}

// stageSource copies the package's build inputs into dst: every non-test .go
// file, go.mod, go.sum, and a vendor/ dir if present.  Test files are skipped
// (they pull in test-only deps and aren't needed for the binary).
func stageSource(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			if name == "vendor" {
				if err := copyTree(filepath.Join(src, name), filepath.Join(dst, name)); err != nil {
					return err
				}
			}
			continue
		}
		copyIt := name == "go.mod" || name == "go.sum" ||
			(strings.HasSuffix(name, ".go") && !strings.HasSuffix(name, "_test.go"))
		if copyIt {
			if err := copyFileInto(filepath.Join(src, name), filepath.Join(dst, name)); err != nil {
				return err
			}
		}
	}
	return nil
}

// stageAppFS writes the app's class files into dst/appfs/ for go:embed.
func stageAppFS(dst, appDir string, classFiles map[string]string) error {
	appfs := filepath.Join(dst, "appfs")
	if err := os.MkdirAll(appfs, 0755); err != nil {
		return err
	}
	for name, path := range classFiles {
		if err := copyFileInto(path, filepath.Join(appfs, name)); err != nil {
			return err
		}
	}
	return nil
}

// writeEmbedFile drops a generated source file that embeds appfs/ and wires it
// into embeddedClasses at init.  Lives only in the throwaway build copy, so the
// committed tree carries no embed directive or appfs placeholder.
func writeEmbedFile(dst string) error {
	const gen = `package main

import (
	"embed"
	"io/fs"
)

//go:embed appfs
var embeddedAppDir embed.FS

func init() {
	if sub, err := fs.Sub(embeddedAppDir, "appfs"); err == nil {
		embeddedClasses = sub
	}
}
`
	return os.WriteFile(filepath.Join(dst, "zz_embed_app.go"), []byte(gen), 0644)
}

// copyFileInto copies src to dst, creating dst's parent directory first.
// (foreign.go's copyFile assumes the parent already exists.)
func copyFileInto(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	return copyFile(src, dst, 0644)
}

func copyTree(src, dst string) error {
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		return copyFileInto(path, target)
	})
}

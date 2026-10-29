package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type MethodDef struct {
	Name   string
	Params []string
	Body   []*Node
}

type ClassDef struct {
	Name    string
	Vars    []string
	Methods map[string]*MethodDef
	// SuperclassName is the parent class name as written in the source
	// (`(superclass Parent)` / `class Child extends Parent`).  Empty for
	// root classes.  Resolved to Superclass by LoadClass.
	SuperclassName string
	// Superclass is the resolved parent ClassDef, or nil for a root class
	// (or when the def was produced by parse-only paths that don't resolve
	// the chain).  Method lookup and var inheritance walk this pointer.
	Superclass *ClassDef
	// Foreign, when non-nil, marks this class as directory-backed.
	// Method bodies are implemented in another language (currently C) and
	// dispatched to via a subprocess bridge (see foreign.go).
	Foreign *ForeignClassInfo
}

// lookupMethod walks the superclass chain starting at cd, returning the
// first matching method and the class that defined it.  ok is false if no
// class in the chain defines the selector.
func lookupMethod(cd *ClassDef, name string) (*MethodDef, *ClassDef, bool) {
	for c := cd; c != nil; c = c.Superclass {
		if m, ok := c.Methods[name]; ok {
			return m, c, true
		}
	}
	return nil, nil, false
}

func initialInstanceVars(cd *ClassDef) map[string]Value {
	vars := make(map[string]Value)
	for c := cd; c != nil; c = c.Superclass {
		for _, name := range c.Vars {
			if _, exists := vars[name]; !exists {
				vars[name] = Nil
			}
		}
	}
	return vars
}

// classPath is the ordered list of directories LoadClass searches.
// Initialised lazily by SetClassPath; the zero state is "." for tests
// and other callers that bypass main entry points.
var classPath = []string{"."}

// binaryRelativeFallbacks returns the directories the runtime should
// search as a last resort, derived from os.Executable().  These cover
// three packaging layouts:
//
//	bundle/                           → flat: bin sits next to classes
//	  bin/smt-...
//	  Main.smt
//
//	bundle/bin/smt + bundle/Main.smt  → bundle.sh layout (classes are
//	                                    one level above the binary)
//
//	App.app/                          → macOS .app bundle
//	  Contents/MacOS/smt              (binary is here)
//	  Contents/Resources/Main.smt    (classes are here)
//
// Symlinks to the binary are resolved so an `smt` shim on $PATH still
// finds its bundled classes via the real binary location.  Returned
// entries are de-duplicated and skip the binary's own directory if
// it equals CWD (avoids redundant lookups when running from the
// build dir).
func binaryRelativeFallbacks() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	exeDir := filepath.Dir(exe)
	parent := filepath.Dir(exeDir)
	return []string{exeDir, parent, filepath.Join(parent, "Resources")}
}

// userClassPath is the subset of classPath that came from the user
// (SMT_PATH + --path).  Tracked separately so ClassPathArgs() can
// propagate only those to spawned children — children compute their
// own binary-relative defaults from os.Executable().
var userClassPath []string

// SetClassPath rebuilds classPath from three sources, in priority order:
//
//  1. SMT_PATH (PATH-style; filepath.ListSeparator) and --path flags —
//     the user's explicit dirs, in the order they were supplied.
//     Highest priority so a user pointing the runtime at their app
//     can't be shadowed by a demo class shipped alongside the binary.
//  2. CWD (".") — convenience for users running the smt binary from
//     inside their app directory.
//  3. Binary-relative fallbacks (exe dir, parent, parent/Resources) —
//     auto-discovered for bundle / .app layouts where the binary
//     lives next to or inside the same tree as the class files.
//
// Empty entries are ignored.  Call once at startup.
func SetClassPath(extra []string) {
	user := []string{}
	if env := os.Getenv("SMT_PATH"); env != "" {
		for _, d := range filepath.SplitList(env) {
			if d != "" {
				user = append(user, d)
			}
		}
	}
	for _, d := range extra {
		if d != "" {
			user = append(user, d)
		}
	}
	userClassPath = user

	dirs := append([]string{}, user...)
	dirs = append(dirs, ".")
	for _, d := range binaryRelativeFallbacks() {
		if d != "" {
			dirs = append(dirs, d)
		}
	}
	classPath = dirs
}

// ClassPathArgs returns the --path flags that reproduce the
// user-supplied classpath in a child process.  Binary-relative entries
// are not propagated — the child computes its own from
// os.Executable().  Used by spawn paths that exec a smt subcommand.
func ClassPathArgs() []string {
	var out []string
	for _, d := range userClassPath {
		out = append(out, "--path", d)
	}
	return out
}

// LoadClass searches classPath for a class definition and resolves its
// superclass chain.  See loadClassRaw for the per-class file lookup.
func LoadClass(className string) (*ClassDef, error) {
	return loadClassChain(className, nil)
}

// loadClassChain loads className and recursively resolves its superclass,
// guarding against cycles via the `loading` stack (the chain of names
// currently being resolved).
func loadClassChain(className string, loading []string) (*ClassDef, error) {
	for _, n := range loading {
		if n == className {
			cycle := append(append([]string{}, loading...), className)
			return nil, fmt.Errorf("cyclic superclass chain: %s", strings.Join(cycle, " -> "))
		}
	}
	cd, err := loadClassRaw(className)
	if err != nil {
		return nil, err
	}
	if cd.SuperclassName != "" {
		parent, err := loadClassChain(cd.SuperclassName, append(loading, className))
		if err != nil {
			return nil, fmt.Errorf("loading superclass of %s: %w", className, err)
		}
		cd.Superclass = parent
	}
	return cd, nil
}

// loadClassRaw searches classPath for a single class definition (no
// superclass resolution).  In each directory it tries ClassName.smt
// (S-expression), then ClassName.st (Smalltalk), then ClassName/
// (foreign / C-backed class).  The first match wins.
func loadClassRaw(className string) (*ClassDef, error) {
	if class, found, err := generationClassSource(className); found { return class, err } else {}
	// Baked-in classes (`smt build` single-binary) take priority, so a built
	// app is self-contained and isn't shadowed by stray files in the CWD.
	if src, ok := embeddedClassSource(className + ".smt"); ok {
		cd, perr := ParseClassSource(string(src))
		if perr != nil {
			return nil, fmt.Errorf("embedded %s.smt: %w", className, perr)
		}
		return cd, nil
	}
	if src, ok := embeddedClassSource(className + ".st"); ok {
		cd, perr := ParseSmalltalkClassSource(string(src))
		if perr != nil {
			return nil, fmt.Errorf("embedded %s.st: %w", className, perr)
		}
		return cd, nil
	}
	for _, dir := range classPath {
		smtPath := filepath.Join(dir, className+".smt")
		if src, err := os.ReadFile(smtPath); err == nil {
			cd, perr := ParseClassSource(string(src))
			if perr != nil {
				return nil, fmt.Errorf("%s: %w", smtPath, perr)
			}
			return cd, nil
		}
		stPath := filepath.Join(dir, className+".st")
		if src, err := os.ReadFile(stPath); err == nil {
			cd, perr := ParseSmalltalkClassSource(string(src))
			if perr != nil {
				return nil, fmt.Errorf("%s: %w", stPath, perr)
			}
			return cd, nil
		}
		dirPath := filepath.Join(dir, className)
		if info, err := os.Stat(dirPath); err == nil && info.IsDir() {
			cd, perr := LoadForeignClass(dirPath)
			if perr != nil {
				return nil, fmt.Errorf("%s/: %w", dirPath, perr)
			}
			return cd, nil
		}
	}
	return nil, fmt.Errorf("cannot find %s.smt, %s.st, or %s/ in classpath %s", className, className, className, strings.Join(classPath, string(filepath.ListSeparator)))
}

// ParseClassSource parses a class definition from source text.
func ParseClassSource(src string) (*ClassDef, error) {
	nodes, err := Parse(src)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		if node.Kind == NodeList && len(node.Children) >= 2 {
			if node.Children[0].Kind == NodeSymbol && node.Children[0].Str == "class" {
				return parseClassNode(node)
			}
		}
	}
	return nil, fmt.Errorf("no (class ...) form found")
}

func parseClassNode(node *Node) (*ClassDef, error) {
	if len(node.Children) < 2 {
		return nil, fmt.Errorf("class requires a name")
	}
	if node.Children[1].Kind != NodeSymbol {
		return nil, fmt.Errorf("class name must be a symbol, got %s", node.Children[1].String())
	}
	cd := &ClassDef{
		Name:    node.Children[1].Str,
		Methods: make(map[string]*MethodDef),
	}
	for _, child := range node.Children[2:] {
		if child.Kind != NodeList || len(child.Children) == 0 {
			return nil, fmt.Errorf("expected (vars ...) or (method ...) in class body at line %d", child.Line)
		}
		head := child.Children[0]
		if head.Kind != NodeSymbol {
			return nil, fmt.Errorf("expected symbol at start of class body form at line %d", child.Line)
		}
		switch head.Str {
		case "superclass":
			if len(child.Children) < 2 || child.Children[1].Kind != NodeSymbol {
				return nil, fmt.Errorf("superclass requires a class name at line %d", child.Line)
			}
			cd.SuperclassName = child.Children[1].Str
		case "vars":
			for _, v := range child.Children[1:] {
				if v.Kind != NodeSymbol {
					return nil, fmt.Errorf("var name must be a symbol at line %d", v.Line)
				}
				cd.Vars = append(cd.Vars, v.Str)
			}
		case "method":
			m, err := parseMethodDef(child)
			if err != nil {
				return nil, err
			}
			cd.Methods[m.Name] = m
		default:
			return nil, fmt.Errorf("unknown class body form %q at line %d", head.Str, head.Line)
		}
	}
	return cd, nil
}

// (method name (param ...) body-expr ...)
func parseMethodDef(node *Node) (*MethodDef, error) {
	if len(node.Children) < 3 {
		return nil, fmt.Errorf("method requires name, params list, and body at line %d", node.Line)
	}
	if node.Children[1].Kind != NodeSymbol {
		return nil, fmt.Errorf("method name must be a symbol at line %d", node.Line)
	}
	name := node.Children[1].Str
	paramsNode := node.Children[2]
	if paramsNode.Kind != NodeList {
		return nil, fmt.Errorf("method params must be a list at line %d", paramsNode.Line)
	}
	var params []string
	for _, p := range paramsNode.Children {
		if p.Kind != NodeSymbol {
			return nil, fmt.Errorf("param name must be a symbol at line %d", p.Line)
		}
		params = append(params, p.Str)
	}
	body := node.Children[3:]
	return &MethodDef{Name: name, Params: params, Body: body}, nil
}

package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// helpText returns a starter card for the REPL: what classes are
// available in the current directory, the universal methods every
// object responds to, and the key sends to the `coordinator` global.
//
// Used by:
//   - the terminal REPL when the user types `help`, `:help`, or `?`,
//   - the coordinator object's `help` / `printHelp` methods, which is
//     how this surface is reachable from inside .smt code.
func helpText() string {
	var b strings.Builder

	b.WriteString("smt — quick reference\n")
	b.WriteString("\n")
	b.WriteString("Classes available in the current directory:\n")
	for _, line := range listClasses() {
		fmt.Fprintf(&b, "  %s\n", line)
	}
	b.WriteString("\n")
	b.WriteString("Every object responds to:\n")
	b.WriteString("  class            → class-name string\n")
	b.WriteString("  yourself         → self\n")
	b.WriteString("  isNil / notNil   → bool\n")
	b.WriteString("  inspect          → prints instance vars to transcript\n")
	b.WriteString("  respondsTo: sym  → bool\n")
	b.WriteString("\n")
	b.WriteString("The coordinator (symbol `coordinator`) responds to:\n")
	b.WriteString("  allObjects                 → Array of every live objid\n")
	b.WriteString("  objectsOfClass: \"Counter\"  → Array of matching objids\n")
	b.WriteString("  classNameOf: anId          → string\n")
	b.WriteString("  size                       → live-object count\n")
	b.WriteString("  printRegistry              → dump all objects to transcript\n")
	b.WriteString("  help                       → this text\n")
	b.WriteString("\n")
	b.WriteString("Examples:\n")
	b.WriteString("  c := Counter new. c increment. c value\n")
	b.WriteString("  coordinator allObjects\n")
	b.WriteString("  coordinator objectsOfClass: 'Counter'\n")
	b.WriteString("\n")
	b.WriteString("Other modes (run from an OS shell, not from this REPL):\n")
	b.WriteString("  smt shell                  → command shell (ls, cd, run, jobs, …)\n")
	b.WriteString("  smt --eval EXPR            → one-shot eval, print, exit\n")
	b.WriteString("  smt shutdown --port N      → graceful coordinator exit\n")
	b.WriteString("  MCP: http://localhost:PORT/mcp (Streamable HTTP)\n")
	b.WriteString("       eval, objects, inspect, call, metrics\n")
	b.WriteString("\n")
	b.WriteString("REPL: Smalltalk syntax.  Comments are \"…\".  S-expr lives in\n")
	b.WriteString("  .smt class files only.  See README.md for the full language.\n")

	return strings.TrimRight(b.String(), "\n")
}

// listClasses scans the current directory for class definitions and
// returns one descriptive line per class, sorted by name.  Used in the
// help card so a user can see what's loadable without reading the
// filesystem themselves.
func listClasses() []string {
	entries, err := os.ReadDir(".")
	if err != nil {
		return []string{"  (cannot read current directory)"}
	}

	type classFound struct{ name, kind string }
	var found []classFound
	seen := map[string]bool{}

	add := func(name, kind string) {
		if seen[name] {
			return
		}
		seen[name] = true
		found = append(found, classFound{name, kind})
	}

	// Built-ins are always available.
	add(UserClassArray, "built-in")
	add(UserClassDictionary, "built-in")
	add(UserClassHttpClient, "built-in")
	add(UserClassHttpServer, "built-in")
	add(UserClassRobotsMatcher, "built-in")
	add(UserClassSitemapParser, "built-in")
	add(UserClassProcess, "built-in")

	for _, e := range entries {
		name := e.Name()
		if e.IsDir() {
			// Foreign-class directory: must contain Name.smt manifest.
			if _, err := os.Stat(name + "/" + name + ".smt"); err == nil {
				add(name, "foreign (C)")
			}
			continue
		}
		if strings.HasSuffix(name, ".smt") {
			add(strings.TrimSuffix(name, ".smt"), ".smt")
		} else if strings.HasSuffix(name, ".st") {
			add(strings.TrimSuffix(name, ".st"), ".st")
		}
	}

	sort.Slice(found, func(i, j int) bool { return found[i].name < found[j].name })

	out := make([]string, 0, len(found))
	for _, f := range found {
		out = append(out, fmt.Sprintf("%-16s %s", f.name, f.kind))
	}
	return out
}
